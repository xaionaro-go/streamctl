package streampanel

import (
	"context"
	"fmt"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
)

type chatUI struct {
	CanvasObject          fyne.CanvasObject
	Panel                 *Panel
	List                  *widget.List
	MessagesHistoryLocker sync.Mutex
	MessagesHistory       []api.ChatMessage

	// TODO: do not store ctx in a struct:
	ctx context.Context
}

func newChatUI(
	ctx context.Context,
	panel *Panel,
) (*chatUI, error) {
	ui := &chatUI{
		Panel: panel,
		ctx:   ctx,
	}
	if err := ui.init(ctx); err != nil {
		return nil, err
	}
	return ui, nil
}

func (ui *chatUI) init(
	ctx context.Context,
) error {
	ui.List = widget.NewList(ui.listLength, ui.listCreateItem, ui.listUpdateItem)
	msgCh, err := ui.Panel.StreamD.SubscribeToChatMessages(ctx)
	if err != nil {
		return fmt.Errorf("unable to subscribe to chat messages: %w", err)
	}

	observability.Go(ctx, func() {
		ui.messageReceiverLoop(ctx, msgCh)
	})

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

	ui.CanvasObject = container.NewBorder(
		nil,
		container.NewBorder(
			nil,
			nil,
			nil,
			messageSendButton,
			messageInputEntry,
		),
		nil,
		nil,
		ui.List,
	)
	return nil
}

func (ui *chatUI) sendMessage(
	ctx context.Context,
	message string,
) error {
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
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgCh:
			if !ok {
				logger.Errorf(ctx, "message channel got closed")
				return
			}
			ui.onReceiveMessage(ctx, msg)
		}
	}
}

func (ui *chatUI) onReceiveMessage(
	ctx context.Context,
	msg api.ChatMessage,
) {
	logger.Debugf(ctx, "onReceiveMessage(ctx, %s)", spew.Sdump(msg))
	ui.MessagesHistoryLocker.Lock()
	defer ui.MessagesHistoryLocker.Unlock()
	ui.MessagesHistory = append(ui.MessagesHistory, msg)
	observability.Go(ctx, func() {
		ui.List.Refresh()
	})
	observability.Go(ctx, func() {
		err := ui.Panel.Audio.PlayChatMessage()
		if err != nil {
			logger.Errorf(ctx, "unable to playback the chat message sound: %v", err)
		}
	})
}

func (ui *chatUI) listLength() int {
	ui.MessagesHistoryLocker.Lock()
	defer ui.MessagesHistoryLocker.Unlock()
	return len(ui.MessagesHistory)
}

func (ui *chatUI) listCreateItem() fyne.CanvasObject {
	banUserButton := widget.NewButtonWithIcon("", theme.ErrorIcon(), func() {})
	removeMsgButton := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {})
	label := widget.NewLabel("<...loading...>")
	label.Wrapping = fyne.TextWrapWord
	return container.NewBorder(
		nil,
		nil,
		container.NewHBox(
			banUserButton,
			removeMsgButton,
		),
		nil,
		label,
	)
}

func (ui *chatUI) listUpdateItem(
	rowID int,
	obj fyne.CanvasObject,
) {
	ctx := context.TODO()
	ui.MessagesHistoryLocker.Lock()
	defer ui.MessagesHistoryLocker.Unlock()
	entryID := len(ui.MessagesHistory) - 1 - rowID
	msg := ui.MessagesHistory[entryID]

	containerPtr := obj.(*fyne.Container)
	objs := containerPtr.Objects
	label := objs[0].(*widget.Label)
	subContainer := objs[1].(*fyne.Container)
	banUserButton := subContainer.Objects[0].(*widget.Button)
	banUserButton.OnTapped = func() {
		w := dialog.NewConfirm(
			"Banning an user",
			fmt.Sprintf("Are you sure you want to ban user '%s' on '%s'", msg.UserID, msg.Platform),
			func(b bool) {
				if b {
					ui.onBanClicked(msg.Platform, msg.UserID)
				}
			},
			ui.Panel.mainWindow,
		)
		w.Show()
	}
	removeMsgButton := subContainer.Objects[1].(*widget.Button)
	removeMsgButton.OnTapped = func() {
		w := dialog.NewConfirm(
			"Removing a message",
			fmt.Sprintf("Are you sure you want to remove the message from '%s' on '%s'", msg.UserID, msg.Platform),
			func(b bool) {
				if b {
					// TODO: think of consistency with onBanClicked
					ui.onRemoveClicked(entryID)
				}
			},
			ui.Panel.mainWindow,
		)
		w.Show()
	}
	label.SetText(fmt.Sprintf(
		"%s: %s: %s: %s",
		msg.CreatedAt.Format("15:04"),
		msg.Platform,
		msg.Username,
		msg.Message,
	))

	requiredHeight := containerPtr.MinSize().Height
	logger.Tracef(ctx, "%d: requiredHeight == %f", rowID, requiredHeight)

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
	return p.StreamD.BanUser(
		ctx,
		platID,
		userID,
		"",
		time.Time{},
	)
}

func (ui *chatUI) onRemoveClicked(
	itemID int,
) {
	ui.MessagesHistoryLocker.Lock()
	defer ui.MessagesHistoryLocker.Unlock()
	if itemID < 0 || itemID >= len(ui.MessagesHistory) {
		return
	}
	msg := ui.MessagesHistory[itemID]
	ui.MessagesHistory = append(ui.MessagesHistory[:itemID], ui.MessagesHistory[itemID+1:]...)
	err := ui.Panel.chatMessageRemove(ui.ctx, msg.Platform, msg.MessageID)
	ui.CanvasObject.Refresh()
	if err != nil {
		ui.Panel.DisplayError(err)
	}
}

func (p *Panel) chatMessageRemove(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	msgID streamcontrol.ChatMessageID,
) error {
	return p.StreamD.RemoveChatMessage(ctx, platID, msgID)
}
