package streampanel

import (
	"context"
	"fmt"
	"sync"
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

type chatUI struct {
	CanvasObject          fyne.CanvasObject
	Panel                 *Panel
	List                  *widget.List
	MessagesHistoryLocker xsync.Mutex
	MessagesHistory       []api.ChatMessage
	EnableButtons         bool
	ReverseOrder          bool
	SuppressNotifications bool
	OnAdd                 func(context.Context, api.ChatMessage)
	OnRemove              func(context.Context, api.ChatMessage)

	CapabilitiesCacheLocker sync.Mutex
	CapabilitiesCache       map[streamcontrol.PlatformName]map[streamcontrol.Capability]struct{}

	CurrentlyPlayingChatMessageSoundCount int32

	ItemLocker          xsync.Mutex
	ItemsByCanvasObject map[fyne.CanvasObject]*chatItem
	ItemsByMessageID    map[streamcontrol.ChatMessageID]*chatItem
	TotalListHeight     uint

	// TODO: do not store ctx in a struct:
	ctx context.Context
}

func newChatUI(
	ctx context.Context,
	enableButtons bool,
	reverseOrder bool,
	suppressNotifications bool,
	panel *Panel,
) (_ret *chatUI, _err error) {
	logger.Debugf(ctx, "newChatUI")
	defer func() { logger.Debugf(ctx, "/newChatUI: %v %v", _ret, _err) }()

	ui := &chatUI{
		Panel:                 panel,
		EnableButtons:         enableButtons,
		ReverseOrder:          reverseOrder,
		SuppressNotifications: suppressNotifications,
		CapabilitiesCache:     make(map[streamcontrol.PlatformName]map[streamcontrol.Capability]struct{}),
		ItemsByCanvasObject:   map[fyne.CanvasObject]*chatItem{},
		ItemsByMessageID:      map[streamcontrol.ChatMessageID]*chatItem{},
		ctx:                   ctx,
	}
	if err := ui.init(ctx); err != nil {
		return nil, err
	}
	return ui, nil
}

func (ui *chatUI) init(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "init")
	defer func() { logger.Debugf(ctx, "/init: %v", _err) }()

	ui.List = widget.NewList(ui.listLength, ui.listCreateItem, ui.listUpdateItem)

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

	msgCh, err := ui.Panel.StreamD.SubscribeToChatMessages(ctx, time.Now().Add(-7*24*time.Hour))
	if err != nil {
		return fmt.Errorf("unable to subscribe to chat messages: %w", err)
	}

	observability.Go(ctx, func() {
		time.Sleep(2 * time.Second) // TODO: delete this ugliness
		ui.messageReceiverLoop(ctx, msgCh)
	})
	return nil
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

func (ui *chatUI) messageReceiverLoop(
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
				logger.Errorf(ctx, "message channel got closed")
				return
			}
			ui.onReceiveMessage(ctx, msg, ui.SuppressNotifications)
		}
	}
}

func (ui *chatUI) onReceiveMessage(
	ctx context.Context,
	msg api.ChatMessage,
	muteNotifications bool,
) {
	logger.Debugf(ctx, "onReceiveMessage(ctx, %s)", spew.Sdump(msg))
	defer func() { logger.Tracef(ctx, "/onReceiveMessage(ctx, %s)", spew.Sdump(msg)) }()
	if onAdd := ui.OnAdd; onAdd != nil {
		defer onAdd(ctx, msg)
	}
	ui.MessagesHistoryLocker.ManualLock(ctx)
	prevLen := len(ui.MessagesHistory)
	defer ui.List.RefreshItem(prevLen)
	defer ui.MessagesHistoryLocker.ManualUnlock(ctx)
	ui.MessagesHistory = append(ui.MessagesHistory, msg)
	if time.Since(msg.CreatedAt) > time.Hour {
		return
	}
	notificationsEnabled := xsync.DoR1(ctx, &ui.Panel.configLocker, func() bool {
		return ui.Panel.Config.Chat.NotificationsEnabled()
	})
	if muteNotifications {
		notificationsEnabled = false
	}
	if !notificationsEnabled {
		return
	}
	observability.GoSafe(ctx, func() {
		commandTemplate := xsync.DoR1(ctx, &ui.Panel.configLocker, func() string {
			return ui.Panel.Config.Chat.CommandOnReceiveMessage
		})
		if commandTemplate == "" {
			return
		}
		logger.Debugf(ctx, "CommandOnReceiveMessage: <%s>", commandTemplate)
		defer logger.Debugf(ctx, "/CommandOnReceiveMessage")

		ui.Panel.execCommand(ctx, commandTemplate, msg)
	})
	observability.GoSafe(ctx, func() {
		logger.Debugf(ctx, "SendNotification")
		defer logger.Debugf(ctx, "/SendNotification")
		ui.Panel.app.SendNotification(&fyne.Notification{
			Title:   string(msg.Platform) + " chat message",
			Content: msg.Username + ": " + msg.Message,
		})
	})
	observability.GoSafe(ctx, func() {
		soundEnabled := xsync.DoR1(ctx, &ui.Panel.configLocker, func() bool {
			return ui.Panel.Config.Chat.ReceiveMessageSoundAlarmEnabled()
		})
		if !soundEnabled {
			return
		}
		concurrentCount := atomic.AddInt32(&ui.CurrentlyPlayingChatMessageSoundCount, 1)
		defer atomic.AddInt32(&ui.CurrentlyPlayingChatMessageSoundCount, -1)
		logger.Debugf(ctx, "PlayChatMessage (count: %d)", concurrentCount)
		if concurrentCount != 1 {
			logger.Debugf(ctx, "/PlayChatMessage: skipped (count == %d)", concurrentCount)
			return
		}
		defer logger.Debugf(ctx, "/PlayChatMessage: (attempted to) played")
		err := ui.Panel.Audio.PlayChatMessage(ctx)
		if err != nil {
			logger.Errorf(ctx, "unable to playback the chat message sound: %v", err)
		}
	})
}

func (ui *chatUI) listLength() int {
	ctx := context.TODO()
	ui.MessagesHistoryLocker.ManualLock(ctx)
	defer ui.MessagesHistoryLocker.ManualUnlock(ctx)
	return len(ui.MessagesHistory)
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
	ctx := context.TODO()
	ui.ItemLocker.Do(ctx, func() {
		ui.ItemsByCanvasObject[item.Container] = item
	})
	return item.Container
}

func (ui *chatUI) getPlatformCapabilities(
	ctx context.Context,
	platID streamcontrol.PlatformName,
) (_ret map[streamcontrol.Capability]struct{}, _err error) {
	logger.Debugf(ctx, "getPlatformCapabilities(ctx, '%s')", platID)
	defer func() { logger.Debugf(ctx, "/getPlatformCapabilities(ctx, '%s'): %#+v, %v", platID, _ret, _err) }()
	ui.CapabilitiesCacheLocker.Lock()
	defer ui.CapabilitiesCacheLocker.Unlock()

	if m, ok := ui.CapabilitiesCache[platID]; ok {
		return m, nil
	}

	if ui.Panel.StreamD == nil {
		return nil, fmt.Errorf("ui.Panel.StreamD == nil")
	}

	info, err := ui.Panel.StreamD.GetBackendInfo(ctx, platID)
	if err != nil {
		return nil, fmt.Errorf("GetBackendInfo returned error: %w", err)
	}

	ui.CapabilitiesCache[platID] = info.Capabilities
	return info.Capabilities, nil
}

func (ui *chatUI) listUpdateItem(
	rowID int,
	obj fyne.CanvasObject,
) {
	ctx := context.TODO()
	ui.MessagesHistoryLocker.ManualLock(ctx)
	defer ui.MessagesHistoryLocker.ManualUnlock(ctx)
	var entryID int
	if ui.ReverseOrder {
		entryID = len(ui.MessagesHistory) - 1 - rowID
	} else {
		entryID = rowID
	}
	if entryID < 0 || entryID >= len(ui.MessagesHistory) {
		logger.Errorf(ctx, "invalid entry ID: %d", entryID)
		return
	}
	msg := ui.MessagesHistory[entryID]

	platCaps, err := ui.getPlatformCapabilities(ctx, msg.Platform)
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
					ui.onRemoveClicked(entryID)
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

	requiredHeight := item.Container.MinSize().Height
	logger.Tracef(ctx, "%d: requiredHeight == %f", rowID, requiredHeight)

	ui.ItemLocker.Do(ctx, func() {
		ui.ItemsByMessageID[msg.MessageID] = item
		ui.TotalListHeight += uint(requiredHeight) - item.Height
		ui.ItemsByMessageID[msg.MessageID].Height = uint(requiredHeight)
	})

	// TODO: think of how to get rid of this racy hack:
	observability.Go(ctx, func() { ui.List.SetItemHeight(rowID, requiredHeight) })
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

func (ui *chatUI) onRemoveClicked(
	itemID int,
) {
	ctx := context.TODO()
	logger.Debugf(ctx, "onRemoveClicked(%s)", itemID)
	defer func() { logger.Debugf(ctx, "/onRemoveClicked(%s)", itemID) }()
	ui.MessagesHistoryLocker.ManualLock(ctx)
	defer ui.MessagesHistoryLocker.ManualUnlock(ctx)
	if itemID < 0 || itemID >= len(ui.MessagesHistory) {
		return
	}
	msg := ui.MessagesHistory[itemID]
	if onRemove := ui.OnRemove; onRemove != nil {
		defer onRemove(ctx, msg)
	}
	ui.MessagesHistory = append(ui.MessagesHistory[:itemID], ui.MessagesHistory[itemID+1:]...)
	ui.ItemLocker.Do(ctx, func() {
		item := ui.ItemsByMessageID[msg.MessageID]
		ui.TotalListHeight -= item.Height
		delete(ui.ItemsByMessageID, msg.MessageID)
		delete(ui.ItemsByCanvasObject, item.Container)
	})
	err := ui.Panel.chatMessageRemove(ui.ctx, msg.Platform, msg.MessageID)
	if err != nil {
		ui.Panel.DisplayError(err)
	}
	observability.Go(ctx, func() { ui.CanvasObject.Refresh() })
}

func (p *Panel) chatMessageRemove(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	msgID streamcontrol.ChatMessageID,
) error {
	return p.StreamD.RemoveChatMessage(ctx, platID, msgID)
}
