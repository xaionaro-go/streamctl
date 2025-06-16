package streampanel

import (
	"context"
	"fmt"
	"slices"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/davecgh/go-spew/spew"
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

const (
	ChatLogSize = 100
)

type chatUI struct {
	CanvasObject  fyne.CanvasObject
	Panel         *Panel
	List          *widget.List
	EnableButtons bool
	ReverseOrder  bool
	OnAdd         func(context.Context, api.ChatMessage)
	OnRemove      func(context.Context, api.ChatMessage)

	ItemLocker          xsync.Mutex
	ItemsByCanvasObject map[fyne.CanvasObject]*chatItem
	ItemsByMessageID    map[streamcontrol.ChatMessageID]*chatItem
	TotalListHeight     uint

	// TODO: do not store ctx in a struct:
	ctx context.Context
}

func (panel *Panel) newChatUI(
	ctx context.Context,
	enableButtons bool,
	reverseOrder bool,
	compactify bool,
) (_ret *chatUI, _err error) {
	logger.Debugf(ctx, "newChatUI")
	defer func() { logger.Debugf(ctx, "/newChatUI: %v %v", _ret, _err) }()

	ui := &chatUI{
		Panel:               panel,
		EnableButtons:       enableButtons,
		ReverseOrder:        reverseOrder,
		ItemsByCanvasObject: map[fyne.CanvasObject]*chatItem{},
		ItemsByMessageID:    map[streamcontrol.ChatMessageID]*chatItem{},
		ctx:                 ctx,
	}
	if err := ui.init(ctx, compactify); err != nil {
		return nil, err
	}
	return ui, nil
}

func (ui *chatUI) init(
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

func (p *Panel) addChatUI(ctx context.Context, ui *chatUI) {
	p.chatUIsLocker.Do(ctx, func() {
		p.chatUIs = append(p.chatUIs, ui)
		logger.Debugf(ctx, "len(p.chatUI) == %d", len(p.chatUIs))
	})
	observability.Go(ctx, func() {
		<-ctx.Done()
		p.chatUIsLocker.Do(ctx, func() {
			p.chatUIs = slices.DeleteFunc(p.chatUIs, func(cmp *chatUI) bool {
				return cmp == ui
			})
		})
	})
}

func (p *Panel) getChatUIs(ctx context.Context) []*chatUI {
	return xsync.DoR1(ctx, &p.chatUIsLocker, func() []*chatUI {
		return slices.Clone(p.chatUIs)
	})
}

func (ui *chatUI) sendMessage(
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

func (p *Panel) initChatMessagesHandler(ctx context.Context) error {
	msgCh, err := p.StreamD.SubscribeToChatMessages(ctx, time.Now().Add(-7*24*time.Hour))
	if err != nil {
		return fmt.Errorf("unable to subscribe to chat messages: %w", err)
	}

	observability.GoSafe(ctx, func() {
		p.messageReceiverLoop(ctx, msgCh)
	})
	return nil
}

func (p *Panel) messageReceiverLoop(
	ctx context.Context,
	msgCh <-chan api.ChatMessage,
) {
	logger.Tracef(ctx, "messageReceiverLoop")
	defer func() { logger.Tracef(ctx, "/messageReceiverLoop") }()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgCh:
			if !ok {
				p.DisplayError(fmt.Errorf("message channel got closed; you won't get chat notifications anymore"))
				return
			}
			p.onReceiveMessage(ctx, msg)
		}
	}
}

func (p *Panel) onReceiveMessage(
	ctx context.Context,
	msg api.ChatMessage,
) {
	logger.Debugf(ctx, "onReceiveMessage(ctx, %s)", spew.Sdump(msg))
	defer func() { logger.Tracef(ctx, "/onReceiveMessage(ctx, %s)", spew.Sdump(msg)) }()
	var prevLen int
	var fullRefresh bool
	for _, chatUI := range p.getChatUIs(ctx) {
		if onAdd := chatUI.OnAdd; onAdd != nil {
			defer onAdd(ctx, msg)
		}
		defer func() {
			if fullRefresh {
				chatUI.List.Refresh()
			} else {
				chatUI.List.RefreshItem(prevLen)
			}
		}()
	}
	p.MessagesHistoryLocker.Do(ctx, func() {
		prevLen = len(p.MessagesHistory)
		p.MessagesHistory = append(p.MessagesHistory, msg)
		if len(p.MessagesHistory) > ChatLogSize*2 {
			p.MessagesHistory = p.MessagesHistory[len(p.MessagesHistory)-ChatLogSize:]
			fullRefresh = true
		}
		if time.Since(msg.CreatedAt) > time.Hour {
			return
		}
		notificationsEnabled := xsync.DoR1(ctx, &p.configLocker, func() bool {
			return p.Config.Chat.NotificationsEnabled()
		})
		if !notificationsEnabled {
			return
		}
		observability.GoSafe(ctx, func() {
			commandTemplate := xsync.DoR1(ctx, &p.configLocker, func() string {
				return p.Config.Chat.CommandOnReceiveMessage
			})
			if commandTemplate == "" {
				return
			}
			logger.Debugf(ctx, "CommandOnReceiveMessage: <%s>", commandTemplate)
			defer logger.Debugf(ctx, "/CommandOnReceiveMessage")

			p.execCommand(ctx, commandTemplate, msg)
		})
		observability.GoSafe(ctx, func() {
			logger.Debugf(ctx, "SendNotification")
			defer logger.Debugf(ctx, "/SendNotification")
			p.app.SendNotification(&fyne.Notification{
				Title:   string(msg.Platform) + " chat message",
				Content: msg.Username + ": " + msg.Message,
			})
		})
		observability.GoSafe(ctx, func() {
			soundEnabled := xsync.DoR1(ctx, &p.configLocker, func() bool {
				return p.Config.Chat.ReceiveMessageSoundAlarmEnabled()
			})
			if !soundEnabled {
				return
			}
			concurrentCount := atomic.AddInt32(&p.currentlyPlayingChatMessageSoundCount, 1)
			defer atomic.AddInt32(&p.currentlyPlayingChatMessageSoundCount, -1)
			logger.Debugf(ctx, "PlayChatMessage (count: %d)", concurrentCount)
			if concurrentCount != 1 {
				logger.Debugf(ctx, "/PlayChatMessage: skipped (count == %d)", concurrentCount)
				return
			}
			defer logger.Debugf(ctx, "/PlayChatMessage: (attempted to) played")
			err := p.Audio.PlayChatMessage(ctx)
			if err != nil {
				logger.Errorf(ctx, "unable to playback the chat message sound: %v", err)
			}
		})
	})
}

func (ui *chatUI) listLength() int {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &ui.Panel.MessagesHistoryLocker, func() int {
		return len(ui.Panel.MessagesHistory)
	})
}

type chatItem struct {
	BanUserButton       *widget.Button
	RemoveMessageButton *widget.Button
	TimestampSegment    *widget.TextSegment
	UsernameSegment     *widget.TextSegment
	MessageSegment      *widget.TextSegment
	Text                *widget.RichText
	Height              uint

	*fyne.Container
}

func (ui *chatUI) listCreateItem() fyne.CanvasObject {
	ctx := context.TODO()
	logger.Debugf(ctx, "listCreateItem")
	defer func() { logger.Tracef(ctx, "listCreateItem") }()

	item := &chatItem{
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

func (p *Panel) getPlatformCapabilities(
	ctx context.Context,
	platID streamcontrol.PlatformName,
) (_ret map[streamcontrol.Capability]struct{}, _err error) {
	logger.Tracef(ctx, "getPlatformCapabilities(ctx, '%s')", platID)
	defer func() { logger.Tracef(ctx, "/getPlatformCapabilities(ctx, '%s'): %#+v, %v", platID, _ret, _err) }()
	p.capabilitiesCacheLocker.Lock()
	defer p.capabilitiesCacheLocker.Unlock()

	if m, ok := p.capabilitiesCache[platID]; ok {
		return m, nil
	}

	if p.StreamD == nil {
		return nil, fmt.Errorf("p.StreamD == nil")
	}

	logger.Debugf(ctx, "GetBackendInfo(ctx, '%s')", platID)
	info, err := p.StreamD.GetBackendInfo(ctx, platID, false)
	if err != nil {
		return nil, fmt.Errorf("GetBackendInfo returned error: %w", err)
	}

	p.capabilitiesCache[platID] = info.Capabilities
	return info.Capabilities, nil
}

func (ui *chatUI) listUpdateItem(
	rowID int,
	obj fyne.CanvasObject,
) {
	ctx := context.TODO()
	logger.Debugf(ctx, "listUpdateItem(%d, obj)", rowID)
	defer func() { logger.Tracef(ctx, "listUpdateItem(%d, obj)", rowID) }()

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

	item := xsync.DoR1(ctx, &ui.ItemLocker, func() *chatItem {
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
	item.TimestampSegment.Text = msg.CreatedAt.Format("15:04:05")
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

func (ui *chatUI) onBanClicked(
	platID streamcontrol.PlatformName,
	userID streamcontrol.ChatUserID,
) {
	ui.Panel.chatUserBan(ui.ctx, platID, userID)
}

func (p *Panel) chatUserBan(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	userID streamcontrol.ChatUserID,
) error {
	// TODO: add controls for the reason and deadline
	return p.StreamD.BanUser(ctx, platID, userID, "", time.Time{})
}

func (p *Panel) onRemoveChatMessageClicked(
	itemID int,
) {
	ctx := context.TODO()
	logger.Debugf(ctx, "onRemoveChatMessageClicked(%s)", itemID)
	defer func() { logger.Debugf(ctx, "/onRemoveChatMessageClicked(%s)", itemID) }()
	var msg *api.ChatMessage
	p.MessagesHistoryLocker.Do(ctx, func() {
		if itemID < 0 || itemID >= len(p.MessagesHistory) {
			return
		}
		msg = &p.MessagesHistory[itemID]
		p.MessagesHistory = append(p.MessagesHistory[:itemID], p.MessagesHistory[itemID+1:]...)
	})
	if msg == nil {
		return
	}
	for _, chatUI := range p.getChatUIs(ctx) {
		if onRemove := chatUI.OnRemove; onRemove != nil {
			defer onRemove(ctx, *msg)
		}
		chatUI.ItemLocker.Do(ctx, func() {
			item := chatUI.ItemsByMessageID[msg.MessageID]
			chatUI.TotalListHeight -= item.Height
			delete(chatUI.ItemsByMessageID, msg.MessageID)
			delete(chatUI.ItemsByCanvasObject, item.Container)
		})
		observability.Go(ctx, func() { chatUI.CanvasObject.Refresh() })
	}
	err := p.chatMessageRemove(ctx, msg.Platform, msg.MessageID)
	if err != nil {
		p.DisplayError(err)
	}
}

func (p *Panel) chatMessageRemove(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	msgID streamcontrol.ChatMessageID,
) error {
	return p.StreamD.RemoveChatMessage(ctx, platID, msgID)
}
