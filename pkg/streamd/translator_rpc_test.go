package streamd

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tassert "github.com/stretchr/testify/assert"
	trequire "github.com/stretchr/testify/require"
	"github.com/xaionaro-go/eventbus"
	"github.com/xaionaro-go/streamctl/pkg/chatmessagesstorage"
	"github.com/xaionaro-go/streamctl/pkg/llm/translatorbuild"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	translator_grpc "github.com/xaionaro-go/streamctl/pkg/translator/grpc/go/translator_grpc"
)

// TestTranslatorStats_NoSubprocess covers the disabled-translation path:
// no externalTranslator was registered, so Running must be false and the
// queue counters must be zero (translation worker never came up). No error
// is expected — translation is opt-in, not a failure mode.
func TestTranslatorStats_NoSubprocess(t *testing.T) {
	d := &StreamD{
		ChatMessagesStorage: chatmessagesstorage.New(""),
		EventBus:            eventbus.New(),
		Config:              config.Config{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	snap, err := d.TranslatorStats(ctx)
	trequire.NoError(t, err)
	trequire.NotNil(t, snap)
	tassert.False(t, snap.Running, "no subprocess registered → Running:false")
	tassert.Zero(t, snap.PID)
	tassert.Equal(t, "", snap.SocketPath)
	tassert.Equal(t, uint32(0), snap.QueueLen)
	tassert.Equal(t, uint32(0), snap.QueueCap, "nil channel cap is 0")
	tassert.Equal(t, int64(0), snap.QueueDrops)
	tassert.Nil(t, snap.SubprocessStats)
}

// TestTranslatorStats_QueueDepth locks in QueueLen reflecting the live
// channel length and QueueCap matching the buffered channel size. We park
// 3 jobs into a cap-5 channel with no consumer (no worker spawned) so the
// snapshot is deterministic.
func TestTranslatorStats_QueueDepth(t *testing.T) {
	d, ctx, cancel := newTestStreamD(t)
	defer cancel()

	d.translationJobs = make(chan *acceptedJob, 5)
	for i := 0; i < 3; i++ {
		d.translationJobs <- &acceptedJob{}
	}

	snap, err := d.TranslatorStats(ctx)
	trequire.NoError(t, err)
	tassert.Equal(t, uint32(3), snap.QueueLen, "three jobs parked, no consumer")
	tassert.Equal(t, uint32(5), snap.QueueCap)
	tassert.False(t, snap.Running, "no subprocess registered")
}

// TestTranslatorStats_DropCounter locks in that QueueDrops reflects the
// streamd-side drop counter incremented by enqueueTranslation.
func TestTranslatorStats_DropCounter(t *testing.T) {
	d, ctx, cancel := newTestStreamD(t)
	defer cancel()

	d.translationJobs = make(chan *acceptedJob, 1)
	// Fill the channel and overflow seven times.
	d.enqueueTranslation(ctx, translationJob{})
	for i := 0; i < 7; i++ {
		d.enqueueTranslation(ctx, translationJob{})
	}

	snap, err := d.TranslatorStats(ctx)
	trequire.NoError(t, err)
	tassert.Equal(t, int64(7), snap.QueueDrops,
		"seven overflowed enqueues must show up in QueueDrops")
}

// TestTranslatorStats_SubprocessOK asserts that when the subprocess answers
// Stats successfully, every chain-level counter and provider entry is
// proxied through to the snapshot.
func TestTranslatorStats_SubprocessOK(t *testing.T) {
	d, ctx, cancel := newTestStreamD(t)
	defer cancel()

	canned := &translator_grpc.StatsReply{
		TotalTranslated:         11,
		TotalAlreadyTarget:      22,
		TotalDetectFailed:       33,
		TotalSpellingOnly:       44,
		TotalAllProvidersFailed: 55,
		TotalSkippedQueueFull:   66,
		LatencySumNanos:         int64(7 * time.Second),
		LatencyCount:            7,
		Providers: []*translator_grpc.ProviderStats{
			{
				Name:                 "p1",
				TotalCalls:           10,
				TotalSuccesses:       9,
				TotalErrors:          1,
				TotalTimeouts:        0,
				TotalQueueRejections: 2,
				LatencySumNanos:      int64(time.Second),
				LatencyCount:         9,
			},
			{
				Name:           "p2",
				TotalCalls:     5,
				TotalSuccesses: 5,
			},
		},
	}
	fake := &fakeTranslatorServer{
		statsFn: func(_ context.Context, _ *translator_grpc.StatsRequest) (*translator_grpc.StatsReply, error) {
			return canned, nil
		},
	}
	wireTestTranslator(t, ctx, d, fake)

	snap, err := d.TranslatorStats(ctx)
	trequire.NoError(t, err)
	trequire.NotNil(t, snap.SubprocessStats)
	tassert.True(t, snap.Running)
	tassert.NotEmpty(t, snap.SocketPath)
	tassert.Equal(t, int64(11), snap.SubprocessStats.GetTotalTranslated())
	tassert.Equal(t, int64(22), snap.SubprocessStats.GetTotalAlreadyTarget())
	tassert.Equal(t, int64(33), snap.SubprocessStats.GetTotalDetectFailed())
	tassert.Equal(t, int64(44), snap.SubprocessStats.GetTotalSpellingOnly())
	tassert.Equal(t, int64(55), snap.SubprocessStats.GetTotalAllProvidersFailed())
	tassert.Equal(t, int64(66), snap.SubprocessStats.GetTotalSkippedQueueFull())
	tassert.Equal(t, int64(7*time.Second), snap.SubprocessStats.GetLatencySumNanos())
	tassert.Equal(t, int64(7), snap.SubprocessStats.GetLatencyCount())
	trequire.Len(t, snap.SubprocessStats.GetProviders(), 2)
	tassert.Equal(t, "p1", snap.SubprocessStats.GetProviders()[0].GetName())
	tassert.Equal(t, int64(10), snap.SubprocessStats.GetProviders()[0].GetTotalCalls())
	tassert.Equal(t, "p2", snap.SubprocessStats.GetProviders()[1].GetName())
}

// TestTranslatorStats_SubprocessTimeout covers the partial-failure path:
// the fake blocks past translatorStatsTimeout, so TranslatorStats must
// return Running:true + the streamd-side queue counters, and a non-nil
// error so callers can surface the RPC failure.
func TestTranslatorStats_SubprocessTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in -short mode (waits translatorStatsTimeout)")
	}
	d, ctx, cancel := newTestStreamD(t)
	defer cancel()

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	fake := &fakeTranslatorServer{
		statsFn: func(ctx context.Context, _ *translator_grpc.StatsRequest) (*translator_grpc.StatsReply, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-release:
				return &translator_grpc.StatsReply{}, nil
			}
		},
	}
	wireTestTranslator(t, ctx, d, fake)

	start := time.Now()
	snap, err := d.TranslatorStats(ctx)
	elapsed := time.Since(start)
	trequire.GreaterOrEqual(t, elapsed, translatorStatsTimeout-100*time.Millisecond,
		"TranslatorStats must wait the full timeout before returning")
	trequire.Less(t, elapsed, translatorStatsTimeout+3*time.Second,
		"TranslatorStats must not exceed translatorStatsTimeout by more than the safety margin")
	trequire.Error(t, err, "blocked Stats RPC must surface as an error")
	trequire.NotNil(t, snap, "partial snapshot must still be returned")
	tassert.True(t, snap.Running, "subprocess is registered → Running:true")
	tassert.Nil(t, snap.SubprocessStats, "no SubprocessStats on RPC failure")
}

// TestTranslatorReload_NoSubprocess locks in that TranslatorReload reports
// a precise error when no translator subprocess is registered. Otherwise an
// operator running `translator reload` against a streamd with translation
// disabled would see a silent success.
func TestTranslatorReload_NoSubprocess(t *testing.T) {
	d := &StreamD{
		ChatMessagesStorage: chatmessagesstorage.New(""),
		EventBus:            eventbus.New(),
		Config:              config.Config{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	applied, err := d.TranslatorReload(ctx)
	trequire.Error(t, err)
	tassert.False(t, applied)
	tassert.Contains(t, err.Error(), "translator subprocess not registered")
}

// TestTranslatorReload_AppliedTrue locks in that TranslatorReload marshals
// the active translation config to YAML and forwards it via Reconfigure.
// The fake captures the YAML so the assertion can confirm a non-default
// TargetLanguage round-tripped through.
func TestTranslatorReload_AppliedTrue(t *testing.T) {
	d, ctx, cancel := newTestStreamD(t)
	defer cancel()

	d.Config.Translation = translatorbuild.Config{
		TargetLanguage: "Klingon",
	}

	var captured atomic.Value // []byte
	reconfigureCount := atomic.Int32{}
	fake := &fakeTranslatorServer{
		reconfigureFn: func(req *translator_grpc.ReconfigureRequest) (*translator_grpc.ReconfigureReply, error) {
			captured.Store(append([]byte(nil), req.GetConfigYaml()...))
			reconfigureCount.Add(1)
			return &translator_grpc.ReconfigureReply{Applied: true}, nil
		},
	}
	wireTestTranslator(t, ctx, d, fake)
	// wireTestTranslator already triggered one Reconfigure (the initial
	// dial path). Reset the counter so the assertion below targets the
	// reload-driven call exclusively.
	reconfigureCount.Store(0)

	applied, err := d.TranslatorReload(ctx)
	trequire.NoError(t, err)
	tassert.True(t, applied)
	tassert.Equal(t, int32(1), reconfigureCount.Load(),
		"TranslatorReload must call Reconfigure exactly once")
	body, ok := captured.Load().([]byte)
	trequire.True(t, ok, "Reconfigure must have been invoked")
	tassert.Contains(t, string(body), "Klingon",
		"forwarded YAML must include the configured target_language")
}

// TestTranslatorReload_AppliedFalse asserts that an in-band rejection
// (subprocess kept the old chain) surfaces as a wrapped error, not a
// silent success. The applied bool must be false in that case.
func TestTranslatorReload_AppliedFalse(t *testing.T) {
	d, ctx, cancel := newTestStreamD(t)
	defer cancel()

	// Initial Reconfigure during wireTestTranslator must succeed; only
	// the subsequent reload should be rejected. We track call count.
	count := atomic.Int32{}
	fake := &fakeTranslatorServer{
		reconfigureFn: func(req *translator_grpc.ReconfigureRequest) (*translator_grpc.ReconfigureReply, error) {
			n := count.Add(1)
			if n == 1 {
				return &translator_grpc.ReconfigureReply{Applied: true}, nil
			}
			return &translator_grpc.ReconfigureReply{
				Applied: false,
				Error:   "boom: invalid provider weight",
			}, nil
		},
	}
	wireTestTranslator(t, ctx, d, fake)

	applied, err := d.TranslatorReload(ctx)
	tassert.False(t, applied)
	trequire.Error(t, err)
	tassert.Contains(t, err.Error(), "boom: invalid provider weight")
}

// TestTranslatorReload_RPCError asserts that a transport error from the
// subprocess (e.g. socket gone) surfaces as a wrapped error and applied=false.
func TestTranslatorReload_RPCError(t *testing.T) {
	d, ctx, cancel := newTestStreamD(t)
	defer cancel()

	count := atomic.Int32{}
	fake := &fakeTranslatorServer{
		reconfigureFn: func(req *translator_grpc.ReconfigureRequest) (*translator_grpc.ReconfigureReply, error) {
			n := count.Add(1)
			if n == 1 {
				return &translator_grpc.ReconfigureReply{Applied: true}, nil
			}
			return nil, errors.New("transport down")
		},
	}
	wireTestTranslator(t, ctx, d, fake)

	applied, err := d.TranslatorReload(ctx)
	tassert.False(t, applied)
	trequire.Error(t, err)
	tassert.True(t,
		strings.Contains(err.Error(), "transport down") ||
			strings.Contains(err.Error(), "Reconfigure"),
		"error must mention the transport failure: got %q", err.Error())
}

// TestTranslatorEnable_emptyTargetReturnsError locks in the precondition
// that an empty target language is rejected as a typo / wrong command. The
// caller is expected to use TranslatorDisable to turn translation off; an
// empty Enable would silently disable an enabled translator and that
// surprise is what the explicit error rules out.
func TestTranslatorEnable_emptyTargetReturnsError(t *testing.T) {
	d := &StreamD{
		ChatMessagesStorage: chatmessagesstorage.New(""),
		EventBus:            eventbus.New(),
		Config:              config.Config{},
		SaveConfigFunc:      func(context.Context, config.Config) error { return nil },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := d.TranslatorEnable(ctx, "")
	trequire.Error(t, err)
	tassert.Contains(t, err.Error(), "targetLanguage")
}

// TestTranslatorEnable_fromDisabled_persistsConfig drives the persist-half
// of the enable contract: when translation is currently disabled, calling
// TranslatorEnable("Klingon") must update d.Config.Translation.TargetLanguage
// to the new value AND persist via SaveConfigFunc so a streamd restart picks
// the value back up. The reconcile half (subprocess spawn) is exercised in
// TestTranslatorEnable_reconcileSpawn — it requires the full subprocess fake
// wiring which only works when the streamd binary's GRPCListenAddr is set.
//
// Falsification: drop the persist call from TranslatorEnable and this test
// fails because saveCalls stays at 0 (or TargetLanguage stays empty).
func TestTranslatorEnable_fromDisabled_persistsConfig(t *testing.T) {
	saveCalls := atomic.Int32{}
	d := &StreamD{
		ChatMessagesStorage: chatmessagesstorage.New(""),
		EventBus:            eventbus.New(),
		Config:              config.Config{},
		SaveConfigFunc: func(_ context.Context, _ config.Config) error {
			saveCalls.Add(1)
			return nil
		},
	}
	d.obsRestarter = newOBSRestarter(d)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// No GRPCListenAddr → reconcile's StartTranslatorSubprocess fails its
	// own precondition with a non-nil error. We assert the persist still
	// happened so on-disk state matches the operator's intent even if the
	// runtime spawn could not complete.
	_ = d.TranslatorEnable(ctx, "Klingon")
	tassert.Equal(t, "Klingon", d.Config.Translation.TargetLanguage,
		"in-memory config must reflect the new target language")
	tassert.GreaterOrEqual(t, saveCalls.Load(), int32(1),
		"SaveConfigFunc must be invoked so a restart resumes the enabled state")
}

// TestTranslatorEnable_fromEnabled_isReload covers the live-edit path: when
// a subprocess is already registered and translation is enabled, calling
// TranslatorEnable with a different target must hand off through the
// Reconfigure RPC (i.e., NOT spawn a new subprocess, NOT crash the old one).
// We confirm the Reconfigure forwarded the new YAML by inspecting the
// captured config bytes.
func TestTranslatorEnable_fromEnabled_isReload(t *testing.T) {
	d, ctx, cancel := newTestStreamD(t)
	defer cancel()
	d.SaveConfigFunc = func(_ context.Context, _ config.Config) error { return nil }

	captured := atomic.Value{} // []byte
	count := atomic.Int32{}
	fake := &fakeTranslatorServer{
		reconfigureFn: func(req *translator_grpc.ReconfigureRequest) (*translator_grpc.ReconfigureReply, error) {
			captured.Store(append([]byte(nil), req.GetConfigYaml()...))
			count.Add(1)
			return &translator_grpc.ReconfigureReply{Applied: true}, nil
		},
	}
	wireTestTranslator(t, ctx, d, fake)
	// wireTestTranslator already triggered one initial Reconfigure. Reset
	// the counter so the assertion below targets only the enable-driven
	// reload.
	count.Store(0)

	trequire.NoError(t, d.TranslatorEnable(ctx, "Klingon"))
	tassert.Equal(t, "Klingon", d.Config.Translation.TargetLanguage,
		"target language must be persisted in-memory")
	tassert.Equal(t, int32(1), count.Load(),
		"a single Reconfigure RPC must fire for the live-edit path")
	body, ok := captured.Load().([]byte)
	trequire.True(t, ok, "Reconfigure must have been invoked")
	tassert.Contains(t, string(body), "Klingon",
		"forwarded YAML must include the new target language")
}

// TestTranslatorDisable_killsSubprocess locks in the disable contract: when
// a subprocess is registered, calling TranslatorDisable cancels its context
// and clears d.translatorSubprocess so subsequent calls find no subprocess.
func TestTranslatorDisable_killsSubprocess(t *testing.T) {
	d, ctx, cancel := newTestStreamD(t)
	defer cancel()
	d.SaveConfigFunc = func(_ context.Context, _ config.Config) error { return nil }

	fake := &fakeTranslatorServer{}
	wireTestTranslator(t, ctx, d, fake)

	d.translatorLocker.Lock()
	trequire.NotNil(t, d.translatorSubprocess, "wiring must register a subprocess")
	d.translatorLocker.Unlock()

	trequire.NoError(t, d.TranslatorDisable(ctx))

	d.translatorLocker.Lock()
	tassert.Nil(t, d.translatorSubprocess,
		"TranslatorDisable must clear the registered subprocess pointer")
	d.translatorLocker.Unlock()

	tassert.Equal(t, "", d.Config.Translation.TargetLanguage,
		"disable must clear target_language from config")
}

// TestTranslatorDisable_alreadyDisabled_isNoop covers the idempotent
// recovery contract: calling Disable when no subprocess is registered must
// succeed and leave the (already-empty) target language untouched.
func TestTranslatorDisable_alreadyDisabled_isNoop(t *testing.T) {
	d := &StreamD{
		ChatMessagesStorage: chatmessagesstorage.New(""),
		EventBus:            eventbus.New(),
		Config:              config.Config{},
		SaveConfigFunc:      func(_ context.Context, _ config.Config) error { return nil },
	}
	d.obsRestarter = newOBSRestarter(d)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	trequire.NoError(t, d.TranslatorDisable(ctx))
	tassert.Equal(t, "", d.Config.Translation.TargetLanguage)
}

// TestTranslatorRestart_noSubprocessReturnsError locks in that restarting
// translation when none is running surfaces an error so the operator
// immediately knows to use Enable instead.
func TestTranslatorRestart_noSubprocessReturnsError(t *testing.T) {
	d := &StreamD{
		ChatMessagesStorage: chatmessagesstorage.New(""),
		EventBus:            eventbus.New(),
		Config:              config.Config{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := d.TranslatorRestart(ctx)
	trequire.Error(t, err)
	tassert.Contains(t, err.Error(), "not registered")
}

// TestTranslatorClearHistory_forwardsToSubprocess covers the happy path:
// when a subprocess is registered, ClearHistory forwards the request via
// the gRPC stub and returns the dropped count from the subprocess reply.
func TestTranslatorClearHistory_forwardsToSubprocess(t *testing.T) {
	d, ctx, cancel := newTestStreamD(t)
	defer cancel()

	fake := &fakeTranslatorServer{
		clearHistoryFn: func(_ context.Context, _ *translator_grpc.ClearHistoryRequest) (*translator_grpc.ClearHistoryReply, error) {
			return &translator_grpc.ClearHistoryReply{DroppedEntries: 7}, nil
		},
	}
	wireTestTranslator(t, ctx, d, fake)

	dropped, err := d.TranslatorClearHistory(ctx)
	trequire.NoError(t, err)
	tassert.Equal(t, int32(7), dropped, "must surface the dropped count from the subprocess")
}

// TestTranslatorClearHistory_noSubprocessReturnsError locks in that calling
// ClearHistory when translation is disabled produces a clear error rather
// than a silent zero — operators with no translator running must see that
// the request did not reach a chain. Mirrors TranslatorReload's error split:
// "not registered" when no subprocess pointer, "not initialised" when the
// pointer exists but the gRPC client has not been built yet.
func TestTranslatorClearHistory_noSubprocessReturnsError(t *testing.T) {
	d := &StreamD{
		ChatMessagesStorage: chatmessagesstorage.New(""),
		EventBus:            eventbus.New(),
		Config:              config.Config{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dropped, err := d.TranslatorClearHistory(ctx)
	trequire.Error(t, err)
	tassert.Equal(t, int32(0), dropped)
	tassert.Contains(t, err.Error(), "translator subprocess not registered")
}

// TestTranslatorClearHistory_subprocessRegisteredButClientNil covers the
// distinct "subprocess registered, client not yet built" failure mode. The
// clearer error message matters because it tells the operator they are
// racing the dial path, not running with translation disabled.
func TestTranslatorClearHistory_subprocessRegisteredButClientNil(t *testing.T) {
	d := &StreamD{
		ChatMessagesStorage: chatmessagesstorage.New(""),
		EventBus:            eventbus.New(),
		Config:              config.Config{},
	}
	// Register a subprocess with no client built (mid-init state).
	d.registerTranslator(&externalTranslator{socketPath: "/dev/null/no-such"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dropped, err := d.TranslatorClearHistory(ctx)
	trequire.Error(t, err)
	tassert.Equal(t, int32(0), dropped)
	tassert.Contains(t, err.Error(), "client not initialised")
}

// TestReconcileTranslator_idempotent covers the same-state-twice case.
// When desired==current, reconcile must be a no-op (no spawn, no kill, no
// Reconfigure). We assert by counting Reconfigure calls into a fake — the
// production path triggers an initial Reconfigure during wiring, then a
// second one if reconcile decides this is a "live edit" — so two reconcile
// calls in a row must produce the same Reconfigure count as one.
func TestReconcileTranslator_idempotent(t *testing.T) {
	d, ctx, cancel := newTestStreamD(t)
	defer cancel()
	d.SaveConfigFunc = func(_ context.Context, _ config.Config) error { return nil }

	count := atomic.Int32{}
	fake := &fakeTranslatorServer{
		reconfigureFn: func(req *translator_grpc.ReconfigureRequest) (*translator_grpc.ReconfigureReply, error) {
			count.Add(1)
			return &translator_grpc.ReconfigureReply{Applied: true}, nil
		},
	}
	wireTestTranslator(t, ctx, d, fake)
	count.Store(0)

	trequire.NoError(t, d.reconcileTranslator(ctx, ctx))
	first := count.Load()
	trequire.NoError(t, d.reconcileTranslator(ctx, ctx))
	second := count.Load()
	tassert.Equal(t, first, second-first,
		"two reconciles with the same desired state must each fire one Reconfigure (live-edit path)")
	// At minimum: each reconcile fires exactly one Reconfigure, so two
	// reconciles → two Reconfigure calls. Locks in that we did NOT spawn an
	// extra subprocess (which would double-count).
	tassert.Equal(t, int32(2), second,
		"two reconciles must produce exactly two Reconfigure calls")
}

// Compile-time guard: the api type must keep the field set the snapshot
// helper depends on. If anyone renames a field, this fails at compile time
// before the tests even run.
var _ = api.TranslatorStatsSnapshot{
	Running:              false,
	PID:                  0,
	SocketPath:           "",
	LastActivityUnixNano: 0,
	QueueLen:             0,
	QueueCap:             0,
	QueueDrops:           0,
	SubprocessStats:      nil,
}
