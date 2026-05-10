package streamd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/chathandler"
	"github.com/xaionaro-go/streamctl/pkg/dedup"
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
			key := eventSource{
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
	key eventSource,
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

	// injectedEventIDTTL aliases pkg/dedup.TTL so the streamd gate and the
	// subprocess-runner gate (pkg/chathandler.Runner.seenIDs) share one
	// authoritative source for retention. Must be longer than any plausible
	// overlap during Level 2 (cross-source collapse) transitions.
	injectedEventIDTTL = dedup.TTL
)

type ChatMessageStorage interface {
	AddMessage(context.Context, api.ChatMessage) error
	UpsertMessage(context.Context, api.ChatMessage) error
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

// processChatMessageUpdate is the publish path for the translated variant of
// an already-published message. It upserts the storage entry so the archive
// holds the final form, then re-publishes through the EventBus so live
// subscribers can replace their displayed copy. shoutoutIfNeeded is NOT
// called: the shoutout fired on the raw publish; firing again on the update
// would double up.
func (d *StreamD) processChatMessageUpdate(
	ctx context.Context,
	msg api.ChatMessage,
) error {
	logger.Tracef(ctx, "processChatMessageUpdate")
	defer logger.Tracef(ctx, "/processChatMessageUpdate")

	if err := d.ChatMessagesStorage.UpsertMessage(ctx, msg); err != nil {
		logger.Errorf(ctx, "unable to upsert the translated message: %v", err)
	}
	publishEvent(ctx, d.EventBus, msg)
	return nil
}

func (d *StreamD) InjectChatMessage(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	listenerType streamcontrol.ChatListenerType,
	ev streamcontrol.Event,
) (_err error) {
	logger.Tracef(ctx, "InjectChatMessage")
	defer func() { logger.Tracef(ctx, "/InjectChatMessage: %v", _err) }()

	// Step 1: ID-level dedup. Multiple PACE listener subprocesses for the
	// same (platform, listener-type) can briefly emit the same logical
	// chat message in different ID formats. computeDedupKey collapses
	// those onto a single key; the cleanup goroutine evicts old entries
	// past injectedEventIDTTL.
	key := computeDedupKey(ctx, platID, ev)
	src := eventSource{Platform: platID, ListenerType: listenerType}

	if _, alreadySeen := d.injectedEvents.LoadOrStore(key, time.Now()); alreadySeen {
		logger.Debugf(ctx, "duplicate event %q from %s/%s (key=%s), skipping",
			ev.ID, platID, listenerType, key)
		return nil
	}

	// Step 2: by content (cross-source collapse). Skipped when the event
	// has no usable Message body — empty-content events collapse via the
	// existing fingerprint-fallback path on Layer 1 already.
	//
	// CONDITIONAL-1 fix: getOrCreateFPEntry can return a pointer that the
	// cleanup goroutine deletes from the outer map between Load and our
	// Lock. After locking, re-check that the entry is still the live one
	// for this fp; if not, retry with a freshly installed entry. One
	// retry is sufficient — two consecutive deletions of the same fp
	// inside one Inject call require a pathological cleanup storm, and
	// even then we fall through to publish without collapse, which is
	// safe (a duplicate publish is preferable to a corrupted index).
	if ev.Message != nil && ev.Message.Content != "" {
		fp := fingerprintEventForCollapse(platID, ev)
		const maxRetries = 2
		for retry := 0; retry < maxRetries; retry++ {
			entry := d.getOrCreateFPEntry(fp)
			entry.mu.Lock()
			if current, ok := d.contentFingerprintIndex.Load(fp); !ok || current != entry {
				// Cleanup deleted (or replaced) this entry between
				// getOrCreateFPEntry and Lock. Re-acquire and retry.
				entry.mu.Unlock()
				continue
			}

			// Find OLDEST group whose source-set does NOT yet contain src.
			// That's the group that this emission "completes" with src.
			var target *sourceGroup
			for _, g := range entry.groups {
				if _, has := g.Sources[src]; !has {
					target = g
					break
				}
			}

			if target != nil {
				// Cross-source link: record src in the group. The
				// linking key is already in injectedEvents from the
				// Step 1 LoadOrStore above (that's how we got past
				// the "alreadySeen" gate); no refresh is needed.
				//
				// REJECT-1 fix: capture every map-derived value into
				// locals BEFORE Unlock so the Debugf below cannot read
				// a Sources map that another concurrent linker is
				// mutating after we release entry.mu.
				target.Sources[src] = struct{}{}
				targetKey := target.DedupKey
				groupSize := len(target.Sources)
				entry.mu.Unlock()
				logger.Debugf(ctx, "cross-source content collapse: %s/%s key=%s onto %s (fp=%s, group_size=%d)",
					platID, listenerType, key, targetKey, fp, groupSize)
				return nil
			}

			// All groups already include src — this is a NEW logical event for src.
			// Append a new group; fall through to publish.
			entry.groups = append(entry.groups, &sourceGroup{
				DedupKey:   key,
				Sources:    map[eventSource]struct{}{src: {}},
				InsertedAt: time.Now(),
			})
			entry.mu.Unlock()
			break
		}
	}

	msg := api.ChatMessage{
		Event:    ev,
		IsLive:   true,
		Platform: platID,
	}

	// Raw-first: publish the untranslated message immediately so chat keeps
	// flowing at network speed even when the translator is slow or down.
	// The translation worker re-publishes the translated variant later via
	// processChatMessageUpdate.
	if err := d.processChatMessage(ctx, msg); err != nil {
		return fmt.Errorf("process raw chat message: %w", err)
	}

	d.enqueueTranslation(ctx, translationJob{
		id:         key,
		platID:     platID,
		msg:        msg,
		enqueuedAt: time.Now(),
	})
	return nil
}

// enqueueTranslation hands a job to the translation worker. When the worker
// is not running (translation disabled) the call is a no-op; when the
// channel is full the call increments translationWorkerQueueDrops and
// returns without blocking. Backpressure must NEVER block ingestion.
//
// Single-disposition accounting (sums hold at every instant):
//   - totalOffered++ unconditionally at the top (every Inject reaches here).
//   - nil-channel branch (translation disabled): bumps
//     offered_with_translation_disabled; no acceptedJob is constructed.
//   - successful channel send: totalEnqueued++; an *acceptedJob is constructed
//     and the finalizer is armed AFTER the send so the queue-full path can
//     drop the half-built struct without firing DispositionLeaked.
//   - queue-full branch: bumps queue_full_at_enqueue; no acceptedJob is
//     constructed (the job never became "enqueued").
//
// totalOffered == totalEnqueued + queueFullAtEnqueue + offeredWithTranslationDisabled
// at every instant (the three branches are mutually exclusive).
//
// The translationQueueIndex is appended under translationQueueLocker BEFORE
// the channel send so a concurrent TranslatorQueueList sees the job no
// later than the worker can see it via the channel. The drop branch does
// not touch the index — the job never entered the channel.
func (d *StreamD) enqueueTranslation(
	ctx context.Context,
	job translationJob,
) {
	if d.translationDispositions != nil {
		d.translationDispositions.totalOffered.Add(1)
	}
	if d.translationJobs == nil {
		if d.translationDispositions != nil {
			d.translationDispositions.offeredWithTranslationDisabled.Add(1)
		}
		return
	}
	accepted := newAcceptedJob(
		job.id, job.platID, job.msg, job.enqueuedAt,
		d.translationDispositions,
	)
	d.translationQueueLocker.Lock()
	d.translationQueueIndex = append(d.translationQueueIndex, accepted)
	select {
	case d.translationJobs <- accepted:
		if d.translationDispositions != nil {
			d.translationDispositions.totalEnqueued.Add(1)
		}
		d.translationQueueLocker.Unlock()
		// Arm the leak finalizer AFTER the locker unlock so the critical
		// section stays minimal. The trade-off is acceptable: `accepted`
		// remains stack-rooted in this local through the next statement,
		// so GC cannot collect it before SetFinalizer runs. The sub-
		// microsecond window between unlock and SetFinalizer cannot lose
		// a finalizer arming.
		//
		// (The queue-full default branch must NOT arm the finalizer; the
		// half-built struct goes out of scope unreferenced and is freed
		// without recording DispositionLeaked.)
		accepted.armLeakFinalizer()
	default:
		// Channel full: roll back the index append and count the drop.
		d.translationQueueIndex = d.translationQueueIndex[:len(d.translationQueueIndex)-1]
		d.translationQueueLocker.Unlock()
		d.translationWorkerQueueDrops.Add(1)
		if d.translationDispositions != nil {
			d.translationDispositions.queueFullAtEnqueue.Add(1)
		}
		logger.Debugf(ctx, "translation queue full; skipping translation for event %q", job.msg.ID)
	}
}

// ReportChatHandlerActivity is the dedicated heartbeat path for external
// chat handlers. It updates only the per-handler liveness timestamp and
// does no other work, so a slow translation in InjectChatMessage cannot
// stall the watchdog signal.
func (d *StreamD) ReportChatHandlerActivity(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	listenerType streamcontrol.ChatListenerType,
) {
	logger.Tracef(ctx, "ReportChatHandlerActivity")
	defer logger.Tracef(ctx, "/ReportChatHandlerActivity")

	d.recordExternalChatHandlerActivity(ctx, eventSource{
		Platform:     platID,
		ListenerType: listenerType,
	})
}

// ReportTranslatorActivity is the heartbeat path for the translator
// subprocess. Like ReportChatHandlerActivity, it does only the timestamp
// update so a slow Translate call cannot stall the watchdog signal.
func (d *StreamD) ReportTranslatorActivity(
	ctx context.Context,
) {
	logger.Tracef(ctx, "ReportTranslatorActivity")
	defer logger.Tracef(ctx, "/ReportTranslatorActivity")

	d.recordTranslatorActivity(ctx)
}

// injectedEventsCleanupInterval controls how often the dedup-cache
// cleanup goroutine sweeps the map. Five passes per TTL keeps each
// expired entry around for at most ~20% longer than the configured TTL,
// which is fine for the dedup gate's purpose.
const injectedEventsCleanupInterval = injectedEventIDTTL / 5

// initInjectedEventsCleanup spawns the long-lived goroutine that evicts
// entries past injectedEventIDTTL from the dedup cache. The goroutine
// exits on ctx cancellation.
func (d *StreamD) initInjectedEventsCleanup(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "initInjectedEventsCleanup")
	defer func() { logger.Tracef(ctx, "/initInjectedEventsCleanup: %v", _err) }()

	observability.Go(ctx, func(ctx context.Context) {
		d.runInjectedEventsCleanup(ctx)
	})
	return nil
}

// runInjectedEventsCleanup drives the periodic eviction sweep. It is
// expected to be invoked inside an observability.Go goroutine spawned by
// initInjectedEventsCleanup.
func (d *StreamD) runInjectedEventsCleanup(
	ctx context.Context,
) {
	logger.Debugf(ctx, "runInjectedEventsCleanup")
	defer logger.Debugf(ctx, "/runInjectedEventsCleanup")

	t := time.NewTicker(injectedEventsCleanupInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		cutoff := time.Now().Add(-injectedEventIDTTL)
		d.injectedEvents.Range(func(k dedupKey, insertedAt time.Time) bool {
			if insertedAt.Before(cutoff) {
				d.injectedEvents.Delete(k)
			}
			return true
		})
		d.contentFingerprintIndex.Range(func(fp string, entry *fpEntry) bool {
			entry.mu.Lock()
			kept := entry.groups[:0]
			for _, g := range entry.groups {
				if g.InsertedAt.After(cutoff) || g.InsertedAt.Equal(cutoff) {
					kept = append(kept, g)
				}
			}
			entry.groups = kept
			// CONDITIONAL-1 fix: keep the Delete inside the locked
			// section. A concurrent InjectChatMessage that has already
			// resolved this fpEntry pointer will block on entry.mu and
			// then re-validate that the entry is still in the map
			// (see InjectChatMessage Step 2). Deleting outside the lock
			// would let the inject goroutine append to a stale entry
			// and orphan the new group.
			if len(entry.groups) == 0 {
				d.contentFingerprintIndex.Delete(fp)
			}
			entry.mu.Unlock()
			return true
		})
	}
}

// getOrCreateFPEntry returns the *fpEntry for the supplied fingerprint,
// creating a fresh empty one (with no groups) on miss. Concurrent callers
// that race on the same missing key all observe the same entry — the
// LoadOrStore loser drops its `fresh` and uses the winner's pointer so
// every code path mutates a single entry under entry.mu.
//
// CAVEAT: the returned pointer can be deleted from the outer map by the
// cleanup goroutine BEFORE the caller acquires entry.mu. Callers MUST
// re-validate, after locking, that the entry they hold is still the live
// one for fp (Load(fp) → same pointer). Otherwise the caller risks
// mutating an orphaned object that no later inject can see.
func (d *StreamD) getOrCreateFPEntry(fp string) *fpEntry {
	if e, ok := d.contentFingerprintIndex.Load(fp); ok {
		return e
	}
	fresh := &fpEntry{}
	actual, _ := d.contentFingerprintIndex.LoadOrStore(fp, fresh)
	return actual
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

	mkID := func(s string) config.ChatUserID {
		return config.ChatUserID{
			Platform: msg.Platform,
			User:     streamcontrol.UserID(strings.ToLower(s)),
		}
	}
	// A configured "auto-shoutout" entry can match the numeric user id, the
	// platform handle/slug, or the display name — different platforms surface
	// different fields, so accept any of them.
	candidates := []config.ChatUserID{
		mkID(string(msg.User.ID)),
		mkID(msg.User.Slug),
		mkID(msg.User.Name),
	}
	userID := candidates[0]
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
		candidate := mkID(string(_candidate.User))
		for _, c := range candidates {
			if c.User == "" {
				continue
			}
			if candidate == c {
				found = true
				break
			}
		}
		if found {
			break
		}
	}

	if !found {
		logger.Debugf(ctx, "user (id=%q slug=%q name=%q) not in the list for auto-shoutout at %q",
			msg.User.ID, msg.User.Slug, msg.User.Name, msg.Platform)
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

	key := eventSource{
		Platform:     platName,
		ListenerType: listenerType,
	}

	handlerCtx, cancel := context.WithCancel(ctx)

	// Reads the live filter value (not the streamd flag default) so that
	// runtime SetLoggerLevel changes are inherited by the next spawn after
	// a restart.
	args := chathandler.BuildChatListenerArgs(
		platName, listenerType, streamdAddr,
		observability.LogLevelFilter.GetLevel(),
		d.Options.LogstashAddr,
	)

	cmd, err := d.Options.SubprocessIO.spawn(handlerCtx, args)
	if err != nil {
		cancel()
		return fmt.Errorf("start chat handler for '%s'/%s: %w", platName, listenerType, err)
	}

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
	key eventSource,
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

	ticker := time.NewTicker(subprocessHealthTimeout / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case err := <-processDone:
			logger.Errorf(ctx,
				"chat handler '%s'/%s died (pid=%d): %v — will restart",
				key.Platform, key.ListenerType, handler.cmd.Process.Pid, err)

			if !sleep(ctx, subprocessRestartDelay) {
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
			if time.Since(lastMsg) > subprocessHealthTimeout {
				logger.Errorf(ctx,
					"chat handler '%s'/%s unresponsive for %s — restarting",
					key.Platform, key.ListenerType, time.Since(lastMsg).Round(time.Second))

				handler.cancelFunc()
				if !sleep(ctx, subprocessRestartDelay) {
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
	key eventSource,
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
	key eventSource,
	handler *externalChatHandler,
) bool {
	d.externalChatHandlerLocker.Lock()
	defer d.externalChatHandlerLocker.Unlock()

	return d.externalChatHandlers[key] == handler
}

// registerExternalChatHandler stores the handler in the map, cancelling any
// previous handler for the same key.
func (d *StreamD) registerExternalChatHandler(
	key eventSource,
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
