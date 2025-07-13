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

type chatUIAsList struct {
	CanvasObject  fyne.CanvasObject
	Panel         *Panel
	List          *widget.List
	EnableButtons bool
	ReverseOrder  bool
	OnAdd         func(context.Context, api.ChatMessage)
	OnRemove      func(context.Context, api.ChatMessage)

	ItemLocker          xsync.Mutex
	ItemsByCanvasObject map[fyne.CanvasObject]*chatListItem
	ItemsByMessageID    map[streamcontrol.ChatMessageID]*chatListItem
	TotalListHeight     uint

	// TODO: do not store ctx in a struct:
	ctx context.Context
}

func (panel *Panel) newChatUIAsList(
	ctx context.Context,
	enableButtons bool,
	reverseOrder bool,
	compactify bool,
) (_ret *chatUIAsList, _err error) {
	logger.Debugf(ctx, "newChatUIAsList")
	defer func() { logger.Debugf(ctx, "/newChatUIAsList: %v %v", _ret, _err) }()

	ui := &chatUIAsList{
		Panel:               panel,
		EnableButtons:       enableButtons,
		ReverseOrder:        reverseOrder,
		ItemsByCanvasObject: map[fyne.CanvasObject]*chatListItem{},
		ItemsByMessageID:    map[streamcontrol.ChatMessageID]*chatListItem{},
		ctx:                 ctx,
	}
	if err := ui.init(ctx, compactify); err != nil {
		return nil, err
	}
	return ui, nil
}

func (ui *chatUIAsList) init(
	ctx context.Context,
	compactify bool,
) (_err error) {
	logger.Debugf(ctx, "init")
	defer func() { logger.Debugf(ctx, "/init: %v", _err) }()

	ui.List = widget.NewList(ui.listLength, ui.listCreateItem, ui.listUpdateItem)
	if compactify {
		container.NewThemeOverride(ui.List,
			&themeWrapperNoPaddingsAndScrollbar{Theme: theme.Current()},
		)
		if v := ui.List.Theme().Size(theme.SizeNamePadding); v != 0 {
			logger.Errorf(ctx, "an internal error: padding is not zero: %d", v)
		}
	}

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
		ui.List,
	)
	ui.Panel.addChatUI(ctx, ui)
	return nil
}

func (ui *chatUIAsList) GetOnAdd() func(
	ctx context.Context,
	msg api.ChatMessage,
) {
	return ui.OnAdd
}
func (ui *chatUIAsList) Rebuild(
	ctx context.Context,
) {
	ui.List.Refresh()
}
func (ui *chatUIAsList) Append(
	_ context.Context,
	itemIdx int,
) {
	ui.List.RefreshItem(itemIdx)
}

func (ui *chatUIAsList) Remove(
	ctx context.Context,
	msg api.ChatMessage,
) {
	if onRemove := ui.OnRemove; onRemove != nil {
		defer onRemove(ctx, msg)
	}
	ui.ItemLocker.Do(ctx, func() {
		item := ui.ItemsByMessageID[msg.MessageID]
		ui.TotalListHeight -= item.Height
		delete(ui.ItemsByMessageID, msg.MessageID)
		delete(ui.ItemsByCanvasObject, item.Container)
	})
	observability.Go(ctx, func(context.Context) { ui.CanvasObject.Refresh() }) // TODO: remove the observability.Go
}

func (ui *chatUIAsList) GetTotalHeight(
	ctx context.Context,
) float32 {
	return float32(ui.TotalListHeight) + ui.List.Theme().Size(theme.SizeNamePadding)*float32(ui.List.Length())
}

func (ui *chatUIAsList) ScrollToBottom(
	ctx context.Context,
) {
	fyneTryLoop(ctx, func() { ui.List.ScrollToBottom() })
}

func (ui *chatUIAsList) sendMessage(
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

func (ui *chatUIAsList) listLength() int {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &ui.Panel.MessagesHistoryLocker, func() int {
		return len(ui.Panel.MessagesHistory)
	})
}

type chatListItem struct {
	BanUserButton       *widget.Button
	RemoveMessageButton *widget.Button
	TimestampSegment    *widget.TextSegment
	UsernameSegment     *widget.TextSegment
	MessageSegment      *widget.TextSegment
	Text                *widget.RichText
	Height              uint

	*fyne.Container
}

func (ui *chatUIAsList) listCreateItem() fyne.CanvasObject {
	ctx := context.TODO()
	logger.Debugf(ctx, "listCreateItem")
	defer func() { logger.Tracef(ctx, "/listCreateItem") }()

	item := &chatListItem{
		Container: &fyne.Container{},
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

	var leftPanel fyne.CanvasObject
	if ui.EnableButtons {
		item.BanUserButton = widget.NewButtonWithIcon("", theme.ErrorIcon(), func() {})
		item.RemoveMessageButton = widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {})
		leftPanel = container.NewHBox(
			item.BanUserButton,
			item.RemoveMessageButton,
		)
	}
	item.Text = widget.NewRichText(
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
	)
	item.Text.Wrapping = fyne.TextWrapWord
	item.Container = container.NewBorder(
		nil,
		nil,
		leftPanel,
		nil,
		item.Text,
	)
	ui.ItemLocker.Do(ctx, func() {
		ui.ItemsByCanvasObject[item.Container] = item
	})
	return item.Container
}

func (ui *chatUIAsList) listUpdateItem(
	rowID int,
	obj fyne.CanvasObject,
) {
	ctx := context.TODO()
	logger.Debugf(ctx, "listUpdateItem(%d, obj)", rowID)
	defer func() { logger.Tracef(ctx, "/listUpdateItem(%d, obj)", rowID) }()

	var entryID int
	var requiredHeight float32
	msg := xsync.DoR1(ctx, &ui.Panel.MessagesHistoryLocker, func() *api.ChatMessage {
		if ui.ReverseOrder {
			entryID = len(ui.Panel.MessagesHistory) - 1 - rowID
		} else {
			entryID = rowID
		}
		if entryID < 0 || entryID >= len(ui.Panel.MessagesHistory) {
			logger.Errorf(ctx, "invalid entry ID: %d", entryID)
			return nil
		}
		return &ui.Panel.MessagesHistory[entryID]
	})
	if msg == nil {
		return
	}

	platCaps, err := ui.Panel.getPlatformCapabilities(ctx, msg.Platform)
	if err != nil {
		ui.Panel.ReportError(fmt.Errorf("unable to get capabilities of platform '%s': %w", msg.Platform, err))
		platCaps = map[streamcontrol.Capability]struct{}{}
	}

	item := xsync.DoR1(ctx, &ui.ItemLocker, func() *chatListItem {
		return ui.ItemsByCanvasObject[obj]
	})
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
					ui.Panel.onRemoveChatMessageClicked(entryID)
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
	newText := msg.CreatedAt.Format("05")
	if msg.EventType != streamcontrol.EventTypeChatMessage {
		newText += fmt.Sprintf(" %s", msg.EventType.String())
	}
	item.TimestampSegment.Text = newText
	item.TimestampSegment.Style.ColorName = colorForPlatform(msg.Platform)
	item.UsernameSegment.Text = msg.Username
	item.UsernameSegment.Style.ColorName = colorForUsername(msg.Username)
	item.MessageSegment.Text = msg.Message
	item.Text.Refresh()
	logger.Tracef(ctx, "%d: updated message is: '%s'", rowID, msg.Message)

	requiredHeight = item.Container.MinSize().Height
	logger.Tracef(ctx, "%d: requiredHeight == %f", rowID, requiredHeight)

	ui.ItemLocker.Do(ctx, func() {
		ui.ItemsByMessageID[msg.MessageID] = item
		ui.TotalListHeight += uint(requiredHeight) - item.Height
		ui.ItemsByMessageID[msg.MessageID].Height = uint(requiredHeight)
	})
	ui.List.SetItemHeight(rowID, requiredHeight)
}

func (ui *chatUIAsList) onBanClicked(
	platID streamcontrol.PlatformName,
	userID streamcontrol.ChatUserID,
) {
	ui.Panel.chatUserBan(ui.ctx, platID, userID)
}
