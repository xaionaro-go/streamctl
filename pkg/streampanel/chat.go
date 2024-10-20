package streampanel

import (
	"context"
	"fmt"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
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
	ui.CanvasObject = ui.List
	return nil
}

func (ui *chatUI) messageReceiverLoop(
	ctx context.Context,
	msgCh <-chan api.ChatMessage,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-msgCh:
			ui.onReceiveMessage(msg)
		}
	}
}

func (ui *chatUI) onReceiveMessage(
	msg api.ChatMessage,
) {
	ui.MessagesHistoryLocker.Lock()
	defer ui.MessagesHistoryLocker.Unlock()
	ui.MessagesHistory = append(ui.MessagesHistory, msg)
}

func (ui *chatUI) listLength() int {
	ui.MessagesHistoryLocker.Lock()
	defer ui.MessagesHistoryLocker.Unlock()
	return len(ui.MessagesHistory)
}

func (ui *chatUI) listCreateItem() fyne.CanvasObject {
	container := container.NewHBox()
	return container
}

func (ui *chatUI) listUpdateItem(
	itemID int,
	obj fyne.CanvasObject,
) {
	ui.MessagesHistoryLocker.Lock()
	defer ui.MessagesHistoryLocker.Unlock()
	container := obj.(*fyne.Container)
	msg := ui.MessagesHistory[itemID]

	label := widget.NewLabel(
		fmt.Sprintf(
			"%s: %s: %s: %s",
			msg.CreatedAt.Format("15:04"),
			msg.Platform,
			msg.UserID,
			msg.Message,
		),
	)
	container.RemoveAll()
	container.Add(widget.NewButtonWithIcon("", theme.ContentRemoveIcon(), func() {
		ui.onRemoveClicked(itemID)
	}))
	container.Add(label)

	// TODO: think of how to get rid of this racy hack:
	observability.Go(context.TODO(), func() { ui.List.SetItemHeight(itemID, label.MinSize().Height) })
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
	ui.Panel.chatMessageRemove(ui.ctx, msg.Platform, msg.MessageID)
	ui.CanvasObject.Refresh()
}

func (p *Panel) chatMessageRemove(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	msgID streamcontrol.ChatMessageID,
) {

	err := p.StreamD.RemoveChatMessage(ctx, platID, msgID)
	if err != nil {
		p.DisplayError(err)
	}
}
