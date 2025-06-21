package streampanel

import (
	"context"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/xsync"
)

type chatUIAsText struct {
	CanvasObject       fyne.CanvasObject
	Panel              *Panel
	ScrollingContainer *container.Scroll
	Text               *widget.RichText
	EnableButtons      bool
	ReverseOrder       bool
	OnAdd              func(context.Context, api.ChatMessage)
	OnRemove           func(context.Context, api.ChatMessage)

	ItemLocker       xsync.Mutex
	ItemsByMessageID map[streamcontrol.ChatMessageID]*chatTextItem
	TotalListHeight  uint

	// TODO: do not store ctx in a struct:
	ctx context.Context
}

func (panel *Panel) newChatUIAsText(
	ctx context.Context,
	enableButtons bool,
	reverseOrder bool,
) (_ret *chatUIAsText, _err error) {
	logger.Debugf(ctx, "newChatUIAsText")
	defer func() { logger.Debugf(ctx, "/newChatUIAsText: %v %v", _ret, _err) }()

	ui := &chatUIAsText{
		Panel:            panel,
		EnableButtons:    enableButtons,
		ReverseOrder:     reverseOrder,
		ItemsByMessageID: map[streamcontrol.ChatMessageID]*chatTextItem{},
		ctx:              ctx,
	}
	if err := ui.init(ctx); err != nil {
		return nil, err
	}
	return ui, nil
}

func (ui *chatUIAsText) init(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "init")
	defer func() { logger.Debugf(ctx, "/init: %v", _err) }()

	ui.Text = widget.NewRichText()
	ui.Text.Wrapping = fyne.TextWrapWord
	ui.ScrollingContainer = container.NewScroll(ui.Text)

	messageInputEntry := widget.NewEntry()
	messageInputEntry.OnSubmitted = func(s string) {
		err := ui.sendMessage(ctx, s)
		if err != nil {
			ui.Panel.DisplayError(err)
		}
	}
	messageSendButton := widget.NewButtonWithIcon("", theme.MailSendIcon(), func() {
		messageInputEntry.OnSubmitted(messageInputEntry.Text)
	})

	var buttonPanel fyne.CanvasObject
	if ui.EnableButtons {
		buttonPanel = container.NewBorder(
			nil,
			nil,
			nil,
			messageSendButton,
			messageInputEntry,
		)
	}
	ui.CanvasObject = container.NewBorder(
		nil,
		buttonPanel,
		nil,
		nil,
		ui.Text,
	)
	ui.Panel.addChatUI(ctx, ui)
	return nil
}

func (ui *chatUIAsText) GetOnAdd() func(
	ctx context.Context,
	msg api.ChatMessage,
) {
	return ui.OnAdd
}
func (ui *chatUIAsText) Rebuild(
	ctx context.Context,
) {
	ui.ItemLocker.Do(ctx, func() {
		ui.Text.Segments = ui.Text.Segments[:0]
		for k := range ui.ItemsByMessageID {
			delete(ui.ItemsByMessageID, k)
		}
		ui.Panel.MessagesHistoryLocker.Do(ctx, func() {
			for itemIdx, msg := range ui.Panel.MessagesHistory {
				ui.newItem(ctx, itemIdx, msg)
			}
		})
	})
}
func (ui *chatUIAsText) Append(
	ctx context.Context,
	itemIdx int,
) {
	ui.ItemLocker.Do(ctx, func() {
		msg := xsync.DoR1(ctx, &ui.Panel.MessagesHistoryLocker, func() *api.ChatMessage {
			if itemIdx >= len(ui.Panel.MessagesHistory) {
				return nil
			}
			return &ui.Panel.MessagesHistory[itemIdx]
		})
		if msg == nil {
			logger.Warnf(ctx, "chat item %d not found", itemIdx)
			return
		}
		item, _ := ui.ItemsByMessageID[msg.MessageID]
		if item != nil {
			logger.Warnf(ctx, "item %d is already added", itemIdx)
			return
		}
		ui.newItem(ctx, itemIdx, *msg)
	})
}

func (ui *chatUIAsText) Remove(
	ctx context.Context,
	msg api.ChatMessage,
) {
	if onRemove := ui.OnRemove; onRemove != nil {
		defer onRemove(ctx, msg)
	}
	ui.ItemLocker.Do(ctx, func() {
		delete(ui.ItemsByMessageID, msg.MessageID)
	})
	observability.Go(ctx, func() { ui.CanvasObject.Refresh() }) // TODO: remove the observability.Go
}

func (ui *chatUIAsText) GetTotalHeight(
	ctx context.Context,
) float32 {
	return ui.Text.MinSize().Height
}

func (ui *chatUIAsText) ScrollToBottom(
	ctx context.Context,
) {
	fyneTryLoop(ctx, func() { ui.ScrollingContainer.ScrollToBottom() })
}

func (ui *chatUIAsText) sendMessage(
	ctx context.Context,
	message string,
) (_err error) {
	logger.Tracef(ctx, "sendMessage")
	defer func() { logger.Tracef(ctx, "/sendMessage: %v", _err) }()

	var result *multierror.Error
	panel := ui.Panel
	streamD := panel.StreamD

	for _, platID := range []streamcontrol.PlatformName{
		twitch.ID,
		kick.ID,
		youtube.ID,
	} {
		isEnabled, err := streamD.IsBackendEnabled(ctx, platID)
		if err != nil {
			result = multierror.Append(result, fmt.Errorf("unable to check if platform '%s' is enabled: %w", platID, err))
			continue
		}

		if !isEnabled {
			continue
		}

		err = streamD.SendChatMessage(ctx, platID, message)
		if err != nil {
			result = multierror.Append(result, fmt.Errorf("unable to send the message to platform '%s': %w", platID, err))
			continue
		}
	}

	return result.ErrorOrNil()
}

type chatTextItem struct {
	BanUserButton       *widget.Button
	RemoveMessageButton *widget.Button
	TimestampSegment    *widget.TextSegment
	UsernameSegment     *widget.TextSegment
	MessageSegment      *widget.TextSegment
}

func (ui *chatUIAsText) newItem(
	ctx context.Context,
	itemIdx int,
	msg api.ChatMessage,
) {
	logger.Debugf(ctx, "newItem(ctx, %#+v)", msg)
	defer func() { logger.Tracef(ctx, "newItem(ctx, %#+v)", msg) }()

	item := &chatTextItem{
		TimestampSegment: &widget.TextSegment{
			Style: widget.RichTextStyle{Inline: true, TextStyle: fyne.TextStyle{Bold: true}},
		},
		UsernameSegment: &widget.TextSegment{
			Style: widget.RichTextStyle{
				Inline:    true,
				SizeName:  theme.SizeNameSubHeadingText,
				TextStyle: fyne.TextStyle{Bold: true},
			},
		},
		MessageSegment: &widget.TextSegment{
			Style: widget.RichTextStyle{
				Inline:    true,
				SizeName:  theme.SizeNameSubHeadingText,
				TextStyle: fyne.TextStyle{Bold: true},
			},
		},
	}

	platCaps, err := ui.Panel.getPlatformCapabilities(ctx, msg.Platform)
	if err != nil {
		ui.Panel.ReportError(fmt.Errorf("unable to get capabilities of platform '%s': %w", msg.Platform, err))
		platCaps = map[streamcontrol.Capability]struct{}{}
	}

	if ui.EnableButtons {
		banUserButton := item.BanUserButton
		banUserButton.OnTapped = func() {
			w := dialog.NewConfirm(
				"Banning an user",
				fmt.Sprintf("Are you sure you want to ban user '%s' on '%s'", msg.UserID, msg.Platform),
				func(b bool) {
					if !b {
						return
					}
					ui.onBanClicked(msg.Platform, msg.UserID)
				},
				ui.Panel.mainWindow,
			)
			w.Show()
		}
		if _, ok := platCaps[streamcontrol.CapabilityBanUser]; !ok {
			banUserButton.Disable()
		} else {
			banUserButton.Enable()
		}
		removeMsgButton := item.RemoveMessageButton
		removeMsgButton.OnTapped = func() {
			w := dialog.NewConfirm(
				"Removing a message",
				fmt.Sprintf("Are you sure you want to remove the message from '%s' on '%s'", msg.UserID, msg.Platform),
				func(b bool) {
					if !b {
						return
					}
					// TODO: think of consistency with onBanClicked
					ui.Panel.onRemoveChatMessageClicked(itemIdx)
				},
				ui.Panel.mainWindow,
			)
			w.Show()
		}
		if _, ok := platCaps[streamcontrol.CapabilityDeleteChatMessage]; !ok {
			removeMsgButton.Disable()
		} else {
			removeMsgButton.Enable()
		}
	}
	item.TimestampSegment.Text = msg.CreatedAt.Format("15:04:05")
	item.TimestampSegment.Style.ColorName = colorForPlatform(msg.Platform)
	item.UsernameSegment.Text = msg.Username
	item.UsernameSegment.Style.ColorName = colorForUsername(msg.Username)
	item.MessageSegment.Text = msg.Message
	logger.Tracef(ctx, "%d: updated message is: '%s'", itemIdx, msg.Message)

	ui.ItemsByMessageID[msg.MessageID] = item

	newSegments := []widget.RichTextSegment{
		item.TimestampSegment,
		&widget.TextSegment{
			Text:  "   ",
			Style: widget.RichTextStyle{Inline: true},
		},
		item.UsernameSegment,
		&widget.TextSegment{
			Text:  "   ",
			Style: widget.RichTextStyle{Inline: true},
		},
		item.MessageSegment,
		&widget.TextSegment{}, // line break
	}
	if ui.ReverseOrder {
		newSegments = append(newSegments, ui.Text.Segments...)
		ui.Text.Segments = newSegments
	} else {
		ui.Text.Segments = append(ui.Text.Segments, newSegments...)
	}
}

func (ui *chatUIAsText) onBanClicked(
	platID streamcontrol.PlatformName,
	userID streamcontrol.ChatUserID,
) {
	ui.Panel.chatUserBan(ui.ctx, platID, userID)
}
