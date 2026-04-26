package streamd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	child_process_manager "github.com/AgustinSRG/go-child-process-manager"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/chathandler"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
)

// chatPlatforms lists platforms that support chat listeners (all except OBS).
var chatPlatforms = []streamcontrol.PlatformName{twitch.ID, kick.ID, youtube.ID}

// reconcileChatListeners compares running chat handler processes against the
// current config and starts/stops handlers to match. Called after config
// updates so that CLI enable/disable commands take effect at runtime.
func (d *StreamD) reconcileChatListeners(
	ctx context.Context,
) {
	logger.Debugf(ctx, "reconcileChatListeners")
	defer logger.Debugf(ctx, "/reconcileChatListeners")

	for _, platName := range chatPlatforms {
		platCfg := d.Config.Backends[platName]
		enabledTypes := resolveEnabledChatListenerTypes(platCfg)

		enabledSet := make(map[streamcontrol.ChatListenerType]struct{}, len(enabledTypes))
		for _, lt := range enabledTypes {
			enabledSet[lt] = struct{}{}
		}

		// Stop handlers for types no longer enabled.
		d.stopDisabledChatHandlers(ctx, platName, enabledSet)

		// Start handlers for newly enabled types.
		for _, lt := range enabledTypes {
			key := chatHandlerKey{
				Platform:     platName,
				ListenerType: lt,
			}
			if d.isHandlerRunning(key) {
				continue
			}
			if err := d.StartExternalChatHandler(ctx, platName, lt, d.GRPCListenAddr); err != nil {
				logger.Errorf(ctx, "reconcile: start chat handler for '%s'/%s: %v", platName, lt, err)
			}
		}
	}
}

// stopDisabledChatHandlers cancels and removes handlers for a platform whose
// listener type is not in enabledSet.
func (d *StreamD) stopDisabledChatHandlers(
	ctx context.Context,
	platName streamcontrol.PlatformName,
	enabledSet map[streamcontrol.ChatListenerType]struct{},
) {
	d.externalChatHandlerLocker.Lock()
	defer d.externalChatHandlerLocker.Unlock()

	for key, handler := range d.externalChatHandlers {
		if key.Platform != platName {
			continue
		}
		if _, ok := enabledSet[key.ListenerType]; ok {
			continue
		}
		handler.cancelFunc()
		delete(d.externalChatHandlers, key)
		logger.Debugf(ctx, "reconcile: stopped chat handler for '%s'/%s", key.Platform, key.ListenerType)
	}
}

// isHandlerRunning returns true if a handler exists for the given key.
func (d *StreamD) isHandlerRunning(
	key chatHandlerKey,
) bool {
	d.externalChatHandlerLocker.Lock()
	defer d.externalChatHandlerLocker.Unlock()

	_, ok := d.externalChatHandlers[key]
	return ok
}

// resolveEnabledChatListenerTypes is the single source of truth for which
// chat listener types should run for a platform. Returns nil when the
// platform is disabled, unconfigured, or has no enabled types.
func resolveEnabledChatListenerTypes(
	platCfg *streamcontrol.AbstractPlatformConfig,
) []streamcontrol.ChatListenerType {
	switch {
	case platCfg == nil:
		return nil
	case platCfg.Enable != nil && !*platCfg.Enable:
		return nil
	case platCfg.EnabledChatListenerTypes != nil:
		return platCfg.EnabledChatListenerTypes
	default:
		return []streamcontrol.ChatListenerType{streamcontrol.ChatListenerPrimary}
	}
}

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
	d.shoutoutIfNeeded(ctx, msg)
	return nil
}

func (d *StreamD) InjectChatMessage(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	ev streamcontrol.Event,
) (_err error) {
	logger.Tracef(ctx, "InjectChatMessage")
	defer func() { logger.Tracef(ctx, "/InjectChatMessage: %v", _err) }()

	// Keepalive messages carry health info for a specific listener type.
	// Format: "keepalive-<listenerType>-<platform>-<timestamp>"
	// Only update the specific handler's health; don't store or display.
	if key, ok := parseKeepaliveEventID(ev.ID, platID); ok {
		d.recordExternalChatHandlerActivity(ctx, key)
		logger.Tracef(ctx, "keepalive received from %s/%s, skipping processing", platID, key.ListenerType)
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

// parseKeepaliveEventID extracts the chatHandlerKey from a keepalive event ID.
// Returns false if the event ID is not a keepalive.
// Keepalive format: "keepalive-<listenerType>-<platform>-<timestamp>"
func parseKeepaliveEventID(
	eventID streamcontrol.EventID,
	platID streamcontrol.PlatformName,
) (chatHandlerKey, bool) {
	id := string(eventID)
	if !strings.HasPrefix(id, "keepalive-") {
		return chatHandlerKey{}, false
	}

	// Strip "keepalive-" prefix, then the next segment is the listener type.
	rest := strings.TrimPrefix(id, "keepalive-")
	dashIdx := strings.Index(rest, "-")
	if dashIdx < 0 {
		return chatHandlerKey{}, false
	}

	ltStr := rest[:dashIdx]
	lt, err := streamcontrol.ChatListenerTypeFromString(ltStr)
	if err != nil {
		return chatHandlerKey{}, false
	}

	return chatHandlerKey{
		Platform:     platID,
		ListenerType: lt,
	}, true
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

// SetBuiltinChatListenerEnabled is deprecated. Chat listeners now run as
// external subprocess handlers managed by startChatListeners /
// StartExternalChatHandler. This method is retained as a no-op so that
// existing gRPC callers do not break.
func (d *StreamD) SetBuiltinChatListenerEnabled(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	enabled bool,
) (_err error) {
	logger.Warnf(ctx, "SetBuiltinChatListenerEnabled is deprecated (platform=%s, enabled=%v); builtin listeners have been removed", platID, enabled)
	return nil
}

// IsBuiltinChatListenerEnabled is deprecated. Chat listeners now run as
// external subprocess handlers. This method always returns false.
func (d *StreamD) IsBuiltinChatListenerEnabled(
	ctx context.Context,
	platID streamcontrol.PlatformName,
) (bool, error) {
	logger.Warnf(ctx, "IsBuiltinChatListenerEnabled is deprecated (platform=%s); builtin listeners have been removed", platID)
	return false, nil
}

const (
	// externalHandlerHealthTimeout is how long streamd waits without
	// receiving an InjectChatMessage before declaring the handler dead.
	externalHandlerHealthTimeout = 30 * time.Second

	// externalHandlerRestartDelay is the delay before restarting a dead handler.
	externalHandlerRestartDelay = 5 * time.Second
)

// StartExternalChatHandler spawns an external chat handler process for the
// given platform and listener type. The process re-uses the current
// executable with chat-listener flags so no separate binary is needed.
func (d *StreamD) StartExternalChatHandler(
	ctx context.Context,
	platName streamcontrol.PlatformName,
	listenerType streamcontrol.ChatListenerType,
	streamdAddr string,
) (_err error) {
	logger.Debugf(ctx, "StartExternalChatHandler(ctx, '%s', '%s', '%s')", platName, listenerType, streamdAddr)
	defer func() {
		logger.Debugf(ctx, "/StartExternalChatHandler(ctx, '%s', '%s', '%s'): %v", platName, listenerType, streamdAddr, _err)
	}()

	if streamdAddr == "" {
		return fmt.Errorf("cannot start chat handler for '%s'/%s: GRPCListenAddr is not set (no gRPC server available)", platName, listenerType)
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve own executable: %w", err)
	}

	key := chatHandlerKey{
		Platform:     platName,
		ListenerType: listenerType,
	}

	handlerCtx, cancel := context.WithCancel(ctx)

	args := []string{
		"--" + chathandler.FlagChatListenerMode,
		"--" + chathandler.FlagChatListenerPlatform, string(platName),
		"--" + chathandler.FlagChatListenerType, listenerType.String(),
		"--" + chathandler.FlagChatListenerStreamdAddr, streamdAddr,
	}

	cmd := exec.CommandContext(handlerCtx, execPath, args...)
	child_process_manager.ConfigureCommand(cmd)

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("start chat handler for '%s'/%s: %w", platName, listenerType, err)
	}
	child_process_manager.AddChildProcess(cmd.Process)

	handler := &externalChatHandler{
		cmd:        cmd,
		cancelFunc: cancel,
	}
	handler.lastMessageTime.Store(time.Now().UnixNano())

	d.registerExternalChatHandler(key, handler)

	// Start health monitor on parent ctx (not handlerCtx). The monitor must
	// survive handler replacement to complete restart. The isCurrentExternalHandler
	// staleness guard prevents stale monitors from restarting replaced handlers.
	observability.Go(ctx, func(ctx context.Context) {
		d.monitorExternalChatHandler(ctx, key, streamdAddr, handler)
	})

	logger.Debugf(ctx, "started external chat handler for '%s'/%s (pid=%d)", platName, listenerType, cmd.Process.Pid)
	return nil
}

// monitorExternalChatHandler watches the external handler process.
// If it dies or stops sending messages, it attempts to restart the handler.
func (d *StreamD) monitorExternalChatHandler(
	ctx context.Context,
	key chatHandlerKey,
	streamdAddr string,
	handler *externalChatHandler,
) {
	logger.Debugf(ctx, "monitorExternalChatHandler('%s'/%s)", key.Platform, key.ListenerType)
	defer logger.Debugf(ctx, "/monitorExternalChatHandler('%s'/%s)", key.Platform, key.ListenerType)

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
			logger.Errorf(ctx,
				"chat handler '%s'/%s died (pid=%d): %v — will restart",
				key.Platform, key.ListenerType, handler.cmd.Process.Pid, err)

			if !sleep(ctx, externalHandlerRestartDelay) {
				return
			}
			if !d.isCurrentExternalHandler(key, handler) {
				logger.Debugf(ctx, "handler for '%s'/%s already replaced, skipping restart", key.Platform, key.ListenerType)
				return
			}
			if restartErr := d.StartExternalChatHandler(ctx, key.Platform, key.ListenerType, streamdAddr); restartErr != nil {
				logger.Errorf(ctx, "failed to restart chat handler for '%s'/%s: %v", key.Platform, key.ListenerType, restartErr)
			}
			return

		case <-ticker.C:
			lastMsg := time.Unix(0, handler.lastMessageTime.Load())
			if time.Since(lastMsg) > externalHandlerHealthTimeout {
				logger.Errorf(ctx,
					"chat handler '%s'/%s unresponsive for %s — restarting",
					key.Platform, key.ListenerType, time.Since(lastMsg).Round(time.Second))

				handler.cancelFunc()
				if !sleep(ctx, externalHandlerRestartDelay) {
					return
				}
				if !d.isCurrentExternalHandler(key, handler) {
					logger.Debugf(ctx, "handler for '%s'/%s already replaced, skipping restart", key.Platform, key.ListenerType)
					return
				}
				if restartErr := d.StartExternalChatHandler(ctx, key.Platform, key.ListenerType, streamdAddr); restartErr != nil {
					logger.Errorf(ctx, "failed to restart chat handler for '%s'/%s: %v", key.Platform, key.ListenerType, restartErr)
				}
				return
			}
		}
	}
}

// recordExternalChatHandlerActivity updates the last-message timestamp
// for the specific handler identified by key. Called from InjectChatMessage
// when a keepalive is received, so the health monitor knows the handler is alive.
func (d *StreamD) recordExternalChatHandlerActivity(
	ctx context.Context,
	key chatHandlerKey,
) {
	d.externalChatHandlerLocker.Lock()
	defer d.externalChatHandlerLocker.Unlock()

	handler, ok := d.externalChatHandlers[key]
	if !ok {
		logger.Debugf(ctx, "recordExternalChatHandlerActivity: no handler for %s/%s", key.Platform, key.ListenerType)
		return
	}

	handler.lastMessageTime.Store(time.Now().UnixNano())
}

// isCurrentExternalHandler returns true if the given handler is still the
// active handler for the key. Used as a staleness guard before restart.
func (d *StreamD) isCurrentExternalHandler(
	key chatHandlerKey,
	handler *externalChatHandler,
) bool {
	d.externalChatHandlerLocker.Lock()
	defer d.externalChatHandlerLocker.Unlock()

	return d.externalChatHandlers[key] == handler
}

// registerExternalChatHandler stores the handler in the map, cancelling any
// previous handler for the same key.
func (d *StreamD) registerExternalChatHandler(
	key chatHandlerKey,
	handler *externalChatHandler,
) {
	d.externalChatHandlerLocker.Lock()
	defer d.externalChatHandlerLocker.Unlock()

	if old, exists := d.externalChatHandlers[key]; exists {
		old.cancelFunc()
	}
	d.externalChatHandlers[key] = handler
}

// StopExternalChatHandler stops all external chat handlers for the given
// platform.
func (d *StreamD) StopExternalChatHandler(
	ctx context.Context,
	platName streamcontrol.PlatformName,
) {
	logger.Debugf(ctx, "StopExternalChatHandler(ctx, '%s')", platName)

	d.stopExternalChatHandlersForPlatform(ctx, platName)
}

// stopExternalChatHandlersForPlatform cancels and removes all handlers
// matching the given platform.
func (d *StreamD) stopExternalChatHandlersForPlatform(
	ctx context.Context,
	platName streamcontrol.PlatformName,
) {
	d.externalChatHandlerLocker.Lock()
	defer d.externalChatHandlerLocker.Unlock()

	for key, handler := range d.externalChatHandlers {
		if key.Platform != platName {
			continue
		}
		handler.cancelFunc()
		delete(d.externalChatHandlers, key)
		logger.Debugf(ctx, "stopped external chat handler for '%s'/%s", key.Platform, key.ListenerType)
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
