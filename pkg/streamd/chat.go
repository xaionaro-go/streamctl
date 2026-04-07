package streamd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	child_process_manager "github.com/AgustinSRG/go-child-process-manager"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
)

const (
	debugSendArchiveMessagesAsLive = false

	// injectedEventIDTTL is how long event IDs are retained in the dedup
	// cache. Must be longer than any plausible overlap during Level 2 transitions.
	injectedEventIDTTL = 5 * time.Minute
)

type ChatMessageStorage interface {
	AddMessage(context.Context, api.ChatMessage) error
	RemoveMessage(context.Context, streamcontrol.EventID) error
	Load(ctx context.Context) error
	Store(ctx context.Context) error
	GetMessagesSince(context.Context, time.Time, uint) ([]api.ChatMessage, error)
}

func (d *StreamD) startListeningForChatMessages(
	ctx context.Context,
	platName streamcontrol.PlatformName,
) error {
	logger.Debugf(ctx, "startListeningForChatMessages(ctx, '%s')", platName)

	if platCfg := d.Config.Backends[platName]; platCfg != nil && platCfg.DisableChatListener {
		logger.Debugf(ctx, "chat listener is disabled for '%s'", platName)
		return nil
	}

	ctrl, err := d.streamController(ctx, platName)
	if err != nil {
		return fmt.Errorf("unable to get the just initialized '%s': %w", platName, err)
	}
	ch, err := ctrl.GetChatMessagesChan(ctx)
	if err != nil {
		return fmt.Errorf("unable to get the channel for chat messages of '%s': %w", platName, err)
	}

	listenerCtx, cancel := context.WithCancel(ctx)
	d.chatListenerLocker.Lock()
	if oldCancel := d.chatListenerCancels[platName]; oldCancel != nil {
		oldCancel()
	}
	d.chatListenerCancels[platName] = cancel
	d.chatListenerLocker.Unlock()

	observability.Go(ctx, func(_ context.Context) {
		defer logger.Debugf(listenerCtx, "/startListeningForChatMessages(ctx, '%s')", platName)
		for {
			select {
			case <-listenerCtx.Done():
				logger.Debugf(ctx, "chat listener for '%s' stopped: %v", platName, listenerCtx.Err())
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				func() {
					msg := api.ChatMessage{
						Event:    ev,
						IsLive:   true,
						Platform: platName,
					}
					if err := d.processChatMessage(ctx, msg); err != nil {
						logger.Errorf(ctx, "unable to process the chat message %#+v: %v", msg, err)
					}
				}()
			}
		}
	})
	return nil
}

func (d *StreamD) processChatMessage(
	ctx context.Context,
	msg api.ChatMessage,
) error {
	logger.Tracef(ctx, "processChatMessage")
	defer logger.Tracef(ctx, "/processChatMessage")

	if err := d.ChatMessagesStorage.AddMessage(ctx, msg); err != nil {
		logger.Errorf(ctx, "unable to add the message to the chat messages storage: %v", err)
	}

	publishEvent(ctx, d.EventBus, msg)
	d.greetIfNeeded(ctx, msg)
	d.shoutoutIfNeeded(ctx, msg)
	return nil
}

func (d *StreamD) InjectChatMessage(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	ev streamcontrol.Event,
) (_err error) {
	logger.Debugf(ctx, "InjectChatMessage")
	defer func() { logger.Debugf(ctx, "/InjectChatMessage: %v", _err) }()

	// Record that the external handler is alive (for health monitoring).
	d.RecordExternalChatHandlerActivity(platID)

	// Keepalive messages are only for health monitoring — don't store or display.
	// Filter by EventID prefix (not message content) to avoid dropping real user messages.
	if strings.HasPrefix(string(ev.ID), "keepalive-") {
		logger.Tracef(ctx, "keepalive received from %s, skipping processing", platID)
		return nil
	}

	// Dedup guard: during Level 2 transitions both built-in and external
	// handlers may briefly overlap. Skip events already processed recently.
	if _, alreadySeen := d.injectedEventIDs.LoadOrStore(ev.ID, time.Now()); alreadySeen {
		logger.Debugf(ctx, "duplicate event %s from %s, skipping", ev.ID, platID)
		return nil
	}
	d.cleanupInjectedEventIDs()

	msg := api.ChatMessage{
		Event:    ev,
		IsLive:   true,
		Platform: platID,
	}
	return d.processChatMessage(ctx, msg)
}

// cleanupInjectedEventIDs removes expired entries from the dedup cache.
func (d *StreamD) cleanupInjectedEventIDs() {
	cutoff := time.Now().Add(-injectedEventIDTTL)
	d.injectedEventIDs.Range(func(id streamcontrol.EventID, insertedAt time.Time) bool {
		if insertedAt.Before(cutoff) {
			d.injectedEventIDs.Delete(id)
		}
		return true
	})
}

// greetIfNeeded emits a synthetic EventTypeGreeting ("said hi") event
// on the first live chat message from each user in this session,
// unless the platform already delivered a real greeting (e.g. YouTube's
// liveChatViewerEngagementMessageRenderer "said hi" button).
func (d *StreamD) greetIfNeeded(
	ctx context.Context,
	msg api.ChatMessage,
) {
	if !msg.IsLive {
		return
	}

	userID := config.ChatUserID{
		Platform: msg.Platform,
		User:     streamcontrol.UserID(strings.ToLower(string(msg.Event.User.ID))),
	}

	// Real platform greeting: record the user as greeted so we don't
	// emit a redundant synthetic greeting when their first regular
	// chat message arrives later.
	switch msg.Event.Type {
	case streamcontrol.EventTypeGreeting:
		d.greetedUsersLocker.Lock()
		d.greetedUsers[userID] = struct{}{}
		d.greetedUsersLocker.Unlock()
		return
	case streamcontrol.EventTypeChatMessage:
		// Fall through to synthetic greeting logic below.
	default:
		return
	}

	d.greetedUsersLocker.Lock()
	_, alreadyGreeted := d.greetedUsers[userID]
	if !alreadyGreeted {
		d.greetedUsers[userID] = struct{}{}
	}
	d.greetedUsersLocker.Unlock()

	if alreadyGreeted {
		return
	}

	logger.Infof(ctx, "first message from %s on %s — emitting greeting", userID.User, msg.Platform)

	greeting := api.ChatMessage{
		Event: streamcontrol.Event{
			ID:        streamcontrol.EventID(fmt.Sprintf("greeting-%s-%s-%d", msg.Platform, userID.User, time.Now().UnixNano())),
			CreatedAt: msg.Event.CreatedAt,
			Type:      streamcontrol.EventTypeGreeting,
			User:      msg.Event.User,
		},
		IsLive:   true,
		Platform: msg.Platform,
	}
	publishEvent(ctx, d.EventBus, greeting)
}

func (d *StreamD) shoutoutIfNeeded(
	ctx context.Context,
	msg api.ChatMessage,
) (_ret bool) {
	logger.Debugf(ctx, "shoutoutIfNeeded(ctx, %#+v)", msg)
	defer logger.Debugf(ctx, "/shoutoutIfNeeded(ctx, %#+v): %v", msg, _ret)
	if !msg.IsLive {
		logger.Tracef(ctx, "is not a live message")
		return false
	}

	d.lastShoutoutAtLocker.Lock()
	defer d.lastShoutoutAtLocker.Unlock()

	userID := config.ChatUserID{
		Platform: msg.Platform,
		User:     streamcontrol.UserID(strings.ToLower(string(msg.User.ID))),
	}
	userIDByName := config.ChatUserID{
		Platform: msg.Platform,
		User:     streamcontrol.UserID(strings.ToLower(string(msg.User.Name))),
	}
	lastShoutoutAt := d.lastShoutoutAt[userID]
	logger.Debugf(ctx, "lastShoutoutAt(%#+v): %v", userID, lastShoutoutAt)
	if v := time.Since(lastShoutoutAt); v < time.Hour {
		logger.Tracef(ctx, "the previous shoutout was too soon: %v < %v", v, time.Hour)
		return false
	}

	cfg, err := d.GetConfig(ctx)
	if err != nil {
		logger.Errorf(ctx, "unable to get the config: %v", err)
		return false
	}

	found := false
	for _, _candidate := range cfg.Shoutout.AutoShoutoutOnMessage {
		if _candidate.Platform != msg.Platform {
			continue
		}
		candidate := config.ChatUserID{
			Platform: _candidate.Platform,
			User:     streamcontrol.UserID(strings.ToLower(string(_candidate.User))),
		}
		if candidate == userID {
			found = true
			break
		}
		if candidate == userIDByName {
			found = true
			break
		}
	}

	if !found {
		logger.Debugf(ctx, "'%s' not in the list for auto-shoutout at '%s'", userID.User, msg.Platform)
		return false
	}

	return d.shoutoutIfCan(ctx, userID.Platform, userID.User)
}

func (d *StreamD) shoutoutIfCan(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	userID streamcontrol.UserID,
) (_ret bool) {
	logger.Debugf(ctx, "shoutoutIfCan('%s', '%s')", platID, userID)
	defer logger.Debugf(ctx, "/shoutoutIfCan('%s', '%s')", platID, userID)

	ctrl, err := d.streamController(ctx, platID)
	if err != nil {
		logger.Errorf(ctx, "unable to get a stream controller '%s': %v", platID, err)
		return false
	}

	if !ctrl.IsCapable(ctx, streamcontrol.CapabilityShoutout) {
		logger.Errorf(ctx, "the controller '%s' does not support shoutouts", platID)
		return false
	}

	err = ctrl.Shoutout(ctx, userID)
	if err != nil {
		logger.Errorf(ctx, "unable to shoutout '%s' at '%s': %v", userID, platID, err)
		return false
	}
	userFullID := config.ChatUserID{
		Platform: platID,
		User:     userID,
	}
	d.lastShoutoutAt[userFullID] = time.Now()
	return true
}

func (d *StreamD) RemoveChatMessage(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	msgID streamcontrol.EventID,
) error {
	ctrl, err := d.streamController(ctx, platID)
	if err != nil {
		return fmt.Errorf("unable to get stream controller '%s': %w", platID, err)
	}

	err = ctrl.RemoveChatMessage(ctx, msgID)
	if err != nil {
		return fmt.Errorf("unable to remove message '%s' on '%s': %w", msgID, platID, err)
	}

	if err := d.ChatMessagesStorage.RemoveMessage(ctx, msgID); err != nil {
		logger.Errorf(ctx, "unable to remove the message from the chat messages storage: %v", err)
	}

	return nil
}

func (d *StreamD) BanUser(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	userID streamcontrol.UserID,
	reason string,
	deadline time.Time,
) error {
	ctrl, err := d.streamController(ctx, platID)
	if err != nil {
		return fmt.Errorf("unable to get stream controller '%s': %w", platID, err)
	}

	err = ctrl.BanUser(ctx, streamcontrol.UserID(userID), reason, deadline)
	if err != nil {
		return fmt.Errorf("unable to ban user '%s' on '%s': %w", userID, platID, err)
	}

	return nil
}

func (d *StreamD) SubscribeToChatMessages(
	ctx context.Context,
	since time.Time,
	limit uint64,
) (_ret <-chan api.ChatMessage, _err error) {
	logger.Tracef(ctx, "SubscribeToChatMessages(ctx, %v, %v)", since, limit)
	defer func() { logger.Tracef(ctx, "/SubscribeToChatMessages(ctx, %v, %v): %p %v", since, limit, _ret, _err) }()

	return eventSubToChan(
		ctx, d.EventBus, 1000,
		func(ctx context.Context, outCh chan api.ChatMessage) {
			logger.Tracef(ctx, "backfilling the channel")
			defer func() { logger.Tracef(ctx, "/backfilling the channel") }()
			msgs, err := d.ChatMessagesStorage.GetMessagesSince(ctx, since, uint(limit))
			if err != nil {
				logger.Errorf(ctx, "unable to get the messages from the storage: %v", err)
				return
			}
			for _, msg := range msgs {
				msg.IsLive = false
				if debugSendArchiveMessagesAsLive {
					msg.IsLive = true
				}
				if !func() (_ret bool) {
					defer func() {
						if recover() != nil {
							logger.Debugf(ctx, "the channel is closed")
							_ret = false
						}
					}()
					outCh <- msg
					return true
				}() {
					break
				}
				if debugSendArchiveMessagesAsLive {
					time.Sleep(5 * time.Second)
				}
			}
		},
	)
}

func (d *StreamD) SendChatMessage(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	message string,
) (_err error) {
	logger.Debugf(ctx, "SendChatMessage(ctx, '%s', '%s')", platID, message)
	defer func() { logger.Debugf(ctx, "/SendChatMessage(ctx, '%s', '%s'): %v", platID, message, _err) }()
	if message == "" {
		return nil
	}

	ctrl, err := d.streamController(ctx, platID)
	if err != nil {
		return fmt.Errorf("unable to get stream controller for platform '%s': %w", platID, err)
	}

	err = ctrl.SendChatMessage(ctx, message)
	if err != nil {
		return fmt.Errorf("unable to send message '%s' to platform '%s': %w", message, platID, err)
	}

	return nil
}

func (d *StreamD) SetBuiltinChatListenerEnabled(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	enabled bool,
) (_err error) {
	logger.Debugf(ctx, "SetBuiltinChatListenerEnabled(ctx, '%s', %v)", platID, enabled)
	defer func() { logger.Debugf(ctx, "/SetBuiltinChatListenerEnabled: %v", _err) }()

	d.chatListenerLocker.Lock()
	cancel := d.chatListenerCancels[platID]
	d.chatListenerLocker.Unlock()

	switch {
	case !enabled && cancel != nil:
		// Stop the listener — cancels context, which stops YouTube API polling.
		cancel()
		d.chatListenerLocker.Lock()
		delete(d.chatListenerCancels, platID)
		d.chatListenerLocker.Unlock()
		logger.Debugf(ctx, "stopped chat listener for '%s'", platID)
	case enabled && cancel == nil:
		// Restart the listener.
		if err := d.startListeningForChatMessages(ctx, platID); err != nil {
			return fmt.Errorf("restart chat listener for '%s': %w", platID, err)
		}
		logger.Debugf(ctx, "restarted chat listener for '%s'", platID)
	}

	return nil
}

func (d *StreamD) IsBuiltinChatListenerEnabled(
	ctx context.Context,
	platID streamcontrol.PlatformName,
) (bool, error) {
	logger.Debugf(ctx, "IsBuiltinChatListenerEnabled(ctx, '%s')", platID)

	d.chatListenerLocker.Lock()
	defer d.chatListenerLocker.Unlock()
	return d.chatListenerCancels[platID] != nil, nil
}

// externalChatHandler tracks a spawned chat handler process.
type externalChatHandler struct {
	cmd             *exec.Cmd
	cancelFunc      context.CancelFunc
	lastMessageTime atomic.Int64 // UnixNano of last InjectChatMessage from this handler
}

const (
	// externalHandlerHealthTimeout is how long streamd waits without
	// receiving an InjectChatMessage before declaring the handler dead.
	externalHandlerHealthTimeout = 30 * time.Second

	// externalHandlerRestartDelay is the delay before restarting a dead handler.
	externalHandlerRestartDelay = 5 * time.Second
)

// chatHandlerBinaryPath returns the path to the chat handler binary for the given platform.
// Looks for "chat-handler-<platform>" next to the current executable.
func chatHandlerBinaryPath(platName streamcontrol.PlatformName) string {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Sprintf("chat-handler-%s", platName)
	}
	dir := execPath[:strings.LastIndex(execPath, string(os.PathSeparator))+1]
	return dir + fmt.Sprintf("chat-handler-%s", platName)
}

// StartExternalChatHandler spawns an external chat handler process for the
// given platform. It disables the built-in listener, starts the process,
// and monitors its health. If the handler becomes unresponsive, streamd
// re-enables the built-in listener (Level 2 fallback) and restarts the handler.
func (d *StreamD) StartExternalChatHandler(
	ctx context.Context,
	platName streamcontrol.PlatformName,
	streamdAddr string,
	extraArgs ...string,
) (_err error) {
	logger.Debugf(ctx, "StartExternalChatHandler(ctx, '%s', '%s')", platName, streamdAddr)
	defer func() {
		logger.Debugf(ctx, "/StartExternalChatHandler(ctx, '%s', '%s'): %v", platName, streamdAddr, _err)
	}()

	binaryPath := chatHandlerBinaryPath(platName)
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		return fmt.Errorf("chat handler binary not found: %s", binaryPath)
	}

	// Disable built-in listener — the external handler will inject events.
	if err := d.SetBuiltinChatListenerEnabled(ctx, platName, false); err != nil {
		return fmt.Errorf("disable built-in listener for '%s': %w", platName, err)
	}

	handlerCtx, cancel := context.WithCancel(ctx)

	args := append([]string{
		"--streamd-addr", streamdAddr,
	}, extraArgs...)

	cmd := exec.CommandContext(handlerCtx, binaryPath, args...)
	// stdout/stderr inherited from parent process automatically.
	child_process_manager.ConfigureCommand(cmd)

	if err := cmd.Start(); err != nil {
		cancel()
		// Re-enable built-in listener since we failed to start.
		if enableErr := d.SetBuiltinChatListenerEnabled(ctx, platName, true); enableErr != nil {
			logger.Errorf(ctx, "failed to re-enable built-in listener for '%s': %v", platName, enableErr)
		}
		return fmt.Errorf("start chat handler for '%s': %w", platName, err)
	}
	child_process_manager.AddChildProcess(cmd.Process)

	handler := &externalChatHandler{
		cmd:        cmd,
		cancelFunc: cancel,
	}
	handler.lastMessageTime.Store(time.Now().UnixNano())

	d.externalChatHandlerLocker.Lock()
	if old, exists := d.externalChatHandlers[platName]; exists {
		old.cancelFunc()
	}
	d.externalChatHandlers[platName] = handler
	d.externalChatHandlerLocker.Unlock()

	// Start health monitor on parent ctx (not handlerCtx). The monitor must
	// survive handler replacement to complete restart. The isCurrentExternalHandler
	// staleness guard prevents stale monitors from restarting replaced handlers.
	observability.Go(ctx, func(ctx context.Context) {
		d.monitorExternalChatHandler(ctx, platName, streamdAddr, handler, extraArgs)
	})

	logger.Infof(ctx, "started external chat handler for '%s' (pid=%d)", platName, cmd.Process.Pid)
	return nil
}

// monitorExternalChatHandler watches the external handler process.
// If it dies or stops sending messages, it re-enables the built-in listener
// and attempts to restart the handler.
func (d *StreamD) monitorExternalChatHandler(
	ctx context.Context,
	platName streamcontrol.PlatformName,
	streamdAddr string,
	handler *externalChatHandler,
	extraArgs []string,
) {
	logger.Debugf(ctx, "monitorExternalChatHandler('%s')", platName)
	defer logger.Debugf(ctx, "/monitorExternalChatHandler('%s')", platName)

	// Wait for the process to exit in a separate goroutine.
	processDone := make(chan error, 1)
	observability.Go(ctx, func(_ context.Context) {
		processDone <- handler.cmd.Wait()
	})

	ticker := time.NewTicker(externalHandlerHealthTimeout / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case err := <-processDone:
			// Process died — activate Level 2 fallback.
			logger.Errorf(ctx,
				"PROCESS FALLBACK: chat-handler-%s died (pid=%d): %v — re-enabling built-in listener",
				platName, handler.cmd.Process.Pid, err)

			d.injectDiagnosticChatEvent(ctx, platName, fmt.Sprintf(
				"PROCESS FALLBACK: chat-handler-%s died, re-enabling built-in listener", platName))

			if enableErr := d.SetBuiltinChatListenerEnabled(ctx, platName, true); enableErr != nil {
				logger.Errorf(ctx, "failed to re-enable built-in listener for '%s': %v", platName, enableErr)
			}

			// Attempt restart after delay.
			if !sleep(ctx, externalHandlerRestartDelay) {
				return
			}
			// Staleness guard: skip restart if a replacement handler was already started.
			if !d.isCurrentExternalHandler(platName, handler) {
				logger.Debugf(ctx, "handler for '%s' already replaced, skipping restart", platName)
				return
			}
			if restartErr := d.StartExternalChatHandler(ctx, platName, streamdAddr, extraArgs...); restartErr != nil {
				logger.Errorf(ctx, "failed to restart chat handler for '%s': %v", platName, restartErr)
			}
			return

		case <-ticker.C:
			lastMsg := time.Unix(0, handler.lastMessageTime.Load())
			if time.Since(lastMsg) > externalHandlerHealthTimeout {
				logger.Errorf(ctx,
					"PROCESS FALLBACK: chat-handler-%s unresponsive for %s — re-enabling built-in listener",
					platName, time.Since(lastMsg).Round(time.Second))

				d.injectDiagnosticChatEvent(ctx, platName, fmt.Sprintf(
					"PROCESS FALLBACK: chat-handler-%s unresponsive, re-enabling built-in listener", platName))

				if enableErr := d.SetBuiltinChatListenerEnabled(ctx, platName, true); enableErr != nil {
					logger.Errorf(ctx, "failed to re-enable built-in listener for '%s': %v", platName, enableErr)
				}

				// Kill the stuck process and restart.
				handler.cancelFunc()
				if !sleep(ctx, externalHandlerRestartDelay) {
					return
				}
				// Staleness guard: skip restart if a replacement handler was already started.
				if !d.isCurrentExternalHandler(platName, handler) {
					logger.Debugf(ctx, "handler for '%s' already replaced, skipping restart", platName)
					return
				}
				if restartErr := d.StartExternalChatHandler(ctx, platName, streamdAddr, extraArgs...); restartErr != nil {
					logger.Errorf(ctx, "failed to restart chat handler for '%s': %v", platName, restartErr)
				}
				return
			}
		}
	}
}

// RecordExternalChatHandlerActivity updates the last-message timestamp
// for the given platform's external handler. Called from InjectChatMessage
// so the health monitor knows the handler is alive.
func (d *StreamD) RecordExternalChatHandlerActivity(
	platName streamcontrol.PlatformName,
) {
	d.externalChatHandlerLocker.Lock()
	handler := d.externalChatHandlers[platName]
	d.externalChatHandlerLocker.Unlock()
	if handler != nil {
		handler.lastMessageTime.Store(time.Now().UnixNano())
	}
}

// isCurrentExternalHandler returns true if the given handler is still the
// active handler for the platform. Used as a staleness guard before restart.
func (d *StreamD) isCurrentExternalHandler(
	platName streamcontrol.PlatformName,
	handler *externalChatHandler,
) bool {
	d.externalChatHandlerLocker.Lock()
	current := d.externalChatHandlers[platName]
	d.externalChatHandlerLocker.Unlock()
	return current == handler
}

// StopExternalChatHandler stops the external chat handler for the given platform
// and re-enables the built-in listener.
func (d *StreamD) StopExternalChatHandler(
	ctx context.Context,
	platName streamcontrol.PlatformName,
) {
	logger.Debugf(ctx, "StopExternalChatHandler(ctx, '%s')", platName)
	d.externalChatHandlerLocker.Lock()
	handler := d.externalChatHandlers[platName]
	delete(d.externalChatHandlers, platName)
	d.externalChatHandlerLocker.Unlock()

	if handler != nil {
		handler.cancelFunc()
		logger.Infof(ctx, "stopped external chat handler for '%s'", platName)
	}

	if err := d.SetBuiltinChatListenerEnabled(ctx, platName, true); err != nil {
		logger.Errorf(ctx, "failed to re-enable built-in listener for '%s': %v", platName, err)
	}
}

// injectDiagnosticChatEvent injects a diagnostic system event into the chat
// pipeline, making it visible to the operator in the chat UI.
func (d *StreamD) injectDiagnosticChatEvent(
	ctx context.Context,
	platName streamcontrol.PlatformName,
	message string,
) {
	msg := api.ChatMessage{
		Event: streamcontrol.Event{
			ID:        streamcontrol.EventID(fmt.Sprintf("diag-%s-%d", platName, time.Now().UnixNano())),
			CreatedAt: time.Now(),
			Type:      streamcontrol.EventTypeOther,
			User: streamcontrol.User{
				ID:   "system",
				Name: "system",
			},
			Message: &streamcontrol.Message{
				Content: fmt.Sprintf("[DIAGNOSTIC] %s", message),
				Format:  streamcontrol.TextFormatTypePlain,
			},
		},
		IsLive:   true,
		Platform: platName,
	}
	if err := d.processChatMessage(ctx, msg); err != nil {
		logger.Errorf(ctx, "failed to inject diagnostic event: %v", err)
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
