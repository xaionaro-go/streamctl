package streampanel

import (
	"context"
	"fmt"
	"slices"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/audio"
	"github.com/xaionaro-go/xsync"
)

var (
	ChatLogSize = 35
)

type chatUIInterface interface {
	GetOnAdd() func(
		ctx context.Context,
		msg api.ChatMessage,
	)
	Remove(
		ctx context.Context,
		msg api.ChatMessage,
	)
	Rebuild(
		ctx context.Context,
	)
	Append(
		ctx context.Context,
		itemIdx int,
	)
	// Update is the in-place refresh path used when a previously-published
	// message is re-published (e.g. the translation update from
	// streamd/processChatMessageUpdate). The caller has already replaced
	// MessagesHistory[idx] with msg; the implementation re-binds its UI
	// segments to the new content. Implementations MUST NOT re-fire add-side
	// notifications (sound, system notification, OnAdd callback) — those
	// fired on the original Append.
	Update(
		ctx context.Context,
		idx int,
		msg api.ChatMessage,
	)
	GetTotalHeight(
		ctx context.Context,
	) float32
	ScrollToBottom(
		ctx context.Context,
	)
}

func (p *Panel) addChatUI(ctx context.Context, ui chatUIInterface) {
	p.chatUIsLocker.Do(ctx, func() {
		p.chatUIs = append(p.chatUIs, ui)
		logger.Debugf(ctx, "len(p.chatUI) == %d", len(p.chatUIs))
	})
	observability.Go(ctx, func(ctx context.Context) {
		<-ctx.Done()
		p.chatUIsLocker.Do(ctx, func() {
			p.chatUIs = slices.DeleteFunc(p.chatUIs, func(cmp chatUIInterface) bool {
				return cmp == ui
			})
		})
	})
}

func (p *Panel) getChatUIs(ctx context.Context) []chatUIInterface {
	return xsync.DoR1(ctx, &p.chatUIsLocker, func() []chatUIInterface {
		return slices.Clone(p.chatUIs)
	})
}

func (p *Panel) initChatMessagesHandler(ctx context.Context) error {
	msgCh, _, err := autoResubscribe(ctx, func(ctx context.Context) (<-chan api.ChatMessage, error) {
		return p.StreamD.SubscribeToChatMessages(ctx, time.Now().Add(-60*24*time.Hour), uint64(ChatLogSize))
	})
	if err != nil {
		return fmt.Errorf("unable to subscribe to chat messages: %w", err)
	}

	observability.GoSafeRestartable(ctx, func(ctx context.Context) {
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
	logger.Tracef(ctx, "onReceiveMessage(ctx, %s)", spew.Sdump(msg))
	defer func() { logger.Tracef(ctx, "/onReceiveMessage(ctx, %s)", spew.Sdump(msg)) }()

	// Update path: an earlier publish carried the same (ID, Platform). Replace
	// the stored entry and ask each chatUI to refresh its row in place. We
	// MUST NOT re-fire the add-side notifications (sound, system notification,
	// OnAdd) — those fired on the original Append.
	if updateIdx := p.findExistingMessageIdx(ctx, msg); updateIdx >= 0 {
		p.applyChatMessageUpdate(ctx, updateIdx, msg)
		return
	}

	var prevLen int
	var fullRefresh bool
	for _, chatUI := range p.getChatUIs(ctx) {
		if onAdd := chatUI.GetOnAdd(); onAdd != nil {
			defer onAdd(ctx, msg)
		}
		defer func() {
			if fullRefresh {
				chatUI.Rebuild(ctx)
			} else {
				chatUI.Append(ctx, prevLen)
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
		observability.GoSafe(ctx, func(ctx context.Context) {
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
		observability.GoSafe(ctx, func(ctx context.Context) {
			logger.Debugf(ctx, "SendNotification")
			defer logger.Debugf(ctx, "/SendNotification")
			p.app.SendNotification(&fyne.Notification{
				Title:   string(msg.Platform) + " chat message",
				Content: msg.User.Name + ": " + msg.Message.Content,
			})
		})
		observability.GoSafe(ctx, func(ctx context.Context) {
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
			audio := audio.NewAudio(ctx)
			defer audio.Playbacker.Close()
			logger.Infof(ctx, "audio backend is %T", audio.Playbacker.PlayerPCM)
			err := audio.PlayChatMessage(ctx)
			if err != nil {
				logger.Errorf(ctx, "unable to playback the chat message sound: %v", err)
			}
		})
	})
}

// findExistingMessageIdx returns the index in MessagesHistory whose
// (ID, Platform) pair matches msg, or -1 when no match exists. The (ID,
// Platform) tuple is the identity, not ID alone — different platforms can
// emit the same bare ID and must remain distinct.
func (p *Panel) findExistingMessageIdx(
	ctx context.Context,
	msg api.ChatMessage,
) int {
	return xsync.DoR1(ctx, &p.MessagesHistoryLocker, func() int {
		for i := range p.MessagesHistory {
			if p.MessagesHistory[i].ID == msg.ID &&
				p.MessagesHistory[i].Platform == msg.Platform {
				return i
			}
		}
		return -1
	})
}

// applyChatMessageUpdate replaces MessagesHistory[idx] with msg under the
// history lock and dispatches Update to every registered chatUI. The
// notification side-effects (sound, system notification, OnAdd callback)
// are intentionally skipped — those fired when the original message was
// appended; firing them again on an in-place edit would double them.
func (p *Panel) applyChatMessageUpdate(
	ctx context.Context,
	idx int,
	msg api.ChatMessage,
) {
	p.MessagesHistoryLocker.Do(ctx, func() {
		if idx < 0 || idx >= len(p.MessagesHistory) {
			return
		}
		p.MessagesHistory[idx] = msg
	})
	for _, chatUI := range p.getChatUIs(ctx) {
		chatUI.Update(ctx, idx, msg)
	}
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

	logger.Tracef(ctx, "GetBackendInfo(ctx, '%s')", platID)
	info, err := p.StreamD.GetBackendInfo(ctx, platID, false)
	if err != nil {
		return nil, fmt.Errorf("GetBackendInfo returned error: %w", err)
	}

	p.capabilitiesCache[platID] = info.Capabilities
	return info.Capabilities, nil
}
func (p *Panel) chatUserBan(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	userID streamcontrol.UserID,
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
		chatUI.Remove(ctx, *msg)
	}
	err := p.chatMessageRemove(ctx, msg.Platform, msg.ID)
	if err != nil {
		p.DisplayError(err)
	}
}

func (p *Panel) chatMessageRemove(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	msgID streamcontrol.EventID,
) error {
	return p.StreamD.RemoveChatMessage(ctx, platID, msgID)
}
