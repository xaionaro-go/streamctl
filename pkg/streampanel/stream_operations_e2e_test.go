//go:build broken
// +build broken

// This file uses types (PlatformID, StreamIDFullyQualified) that don't exist in the old branch.
// Excluded from default compilation until adapted.

package streampanel

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	benbjohnsonclock "github.com/benbjohnson/clock"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/clock"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	streamdconfig "github.com/xaionaro-go/streamctl/pkg/streamd/config"
)

// setupStreamOpsPanel creates a minimal Panel with a dummyStreamD for testing
// stream operation handlers (setup/start/stop).
func setupStreamOpsPanel(t *testing.T) (*Panel, *dummyStreamD, context.Context, context.CancelFunc) {
	t.Helper()

	testApp := test.NewApp()
	testApp.Settings().SetTheme(theme.DefaultTheme())
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	ctx = observability.WithSecretsProvider(ctx, &observability.SecretsStaticProvider{})

	mockClock := benbjohnsonclock.NewMock()
	clock.Set(mockClock)

	p, err := New("", OptionApp{App: testApp})
	require.NoError(t, err)
	p.app = testApp
	p.defaultContext = ctx
	p.backgroundRenderer = newBackgroundRenderer(ctx, p)

	d := &dummyStreamD{}
	p.StreamD = d
	cfg, _ := d.GetConfig(ctx)
	d.Config = cfg
	p.configCache = cfg

	p.selectedProfileName = new(streamcontrol.ProfileName)
	*p.selectedProfileName = "default"
	if p.configCache.ProfileMetadata == nil {
		p.configCache.ProfileMetadata = make(map[streamcontrol.ProfileName]streamdconfig.ProfileMetadata)
	}
	p.configCache.ProfileMetadata["default"] = streamdconfig.ProfileMetadata{}

	p.createMainWindow(ctx)
	p.initMainWindow(ctx, "control")

	// Set a title so setupStream doesn't bail early.
	p.streamTitleField.SetText("Test Title")

	return p, d, ctx, cancel
}

// TestSetupStreamDoesNotBlockUI verifies that onSetupStreamButton returns
// immediately (does not block the calling goroutine) even when gRPC calls
// are slow.
func TestSetupStreamDoesNotBlockUI(t *testing.T) {
	p, d, ctx, cancel := setupStreamOpsPanel(t)
	defer cancel()
	defer p.app.Quit()

	// Inject a slow IsBackendEnabled that blocks until we signal.
	unblock := make(chan struct{})
	var isBackendEnabledCalled atomic.Bool
	d.IsBackendEnabledFn = func(ctx context.Context, id streamcontrol.PlatformID) (bool, error) {
		isBackendEnabledCalled.Store(true)
		<-unblock
		return true, nil
	}

	// Call the handler — should return immediately if async.
	done := make(chan struct{})
	go func() {
		p.onSetupStreamButton(ctx)
		close(done)
	}()

	// If handler is async (our fix), it returns immediately.
	// If handler is sync (current bug), it blocks until unblock.
	select {
	case <-done:
		// Good — handler returned without blocking.
	case <-time.After(2 * time.Second):
		t.Fatal("onSetupStreamButton blocked the calling goroutine")
	}

	// Now let the background work complete.
	close(unblock)

	// Verify the operation actually ran.
	require.Eventually(t, func() bool {
		return isBackendEnabledCalled.Load()
	}, 5*time.Second, 100*time.Millisecond, "expected IsBackendEnabled to be called")
}

// TestStartStreamDoesNotBlockUI verifies that startStream works correctly
// when called from a background goroutine (as the dialog callback now does),
// and that UI mutations via DoFromGoroutine don't cause deadlocks.
func TestStartStreamDoesNotBlockUI(t *testing.T) {
	p, d, ctx, cancel := setupStreamOpsPanel(t)
	defer cancel()
	defer p.app.Quit()

	// Inject a slow IsBackendEnabled that blocks until we signal.
	unblock := make(chan struct{})
	var isBackendEnabledCalled atomic.Bool
	d.IsBackendEnabledFn = func(ctx context.Context, id streamcontrol.PlatformID) (bool, error) {
		isBackendEnabledCalled.Store(true)
		<-unblock
		return true, nil
	}

	var setStreamActiveCalled atomic.Bool
	d.SetStreamActiveFn = func(ctx context.Context, streamID streamcontrol.StreamIDFullyQualified, active bool) error {
		setStreamActiveCalled.Store(true)
		return nil
	}

	// Ensure the button is enabled so startStream doesn't bail early.
	p.startStopButton.Enable()

	// Launch startStream the same way the fixed onStartStopButton dialog does.
	// Use GoSafe to tolerate the pre-existing Fyne test driver icon rendering
	// panic (nil reader in Image.Refresh when themed icons are decoded).
	observability.GoSafe(ctx, func(ctx context.Context) {
		p.startStream(ctx)
	})

	// Verify that IsBackendEnabled was called (i.e., the goroutine started).
	require.Eventually(t, func() bool {
		return isBackendEnabledCalled.Load()
	}, 5*time.Second, 100*time.Millisecond, "expected IsBackendEnabled to be called")

	// Now let the background work complete.
	close(unblock)

	// Verify the operation actually completed.
	require.Eventually(t, func() bool {
		return setStreamActiveCalled.Load()
	}, 5*time.Second, 100*time.Millisecond, "expected SetStreamActive to be called")
}

// TestStopStreamDoesNotBlockUI verifies that stopStream works correctly
// when called from a background goroutine (as the dialog callback now does),
// and that UI mutations via DoFromGoroutine don't cause deadlocks.
func TestStopStreamDoesNotBlockUI(t *testing.T) {
	p, d, ctx, cancel := setupStreamOpsPanel(t)
	defer cancel()
	defer p.app.Quit()

	// Mark stream as running so onStartStopButton treats it as "stop".
	p.updateStreamClockHandler = newUpdateTimerHandler(p.startStopButton, clock.Get().Now())

	// Inject a slow IsBackendEnabled that blocks until we signal.
	unblock := make(chan struct{})
	var isBackendEnabledCalled atomic.Bool
	d.IsBackendEnabledFn = func(ctx context.Context, id streamcontrol.PlatformID) (bool, error) {
		isBackendEnabledCalled.Store(true)
		<-unblock
		return true, nil
	}

	// Launch stopStream the same way the fixed onStartStopButton dialog does.
	// Use GoSafe to tolerate the pre-existing Fyne test driver icon rendering
	// panic (nil reader in Image.Refresh when themed icons are decoded).
	stopDone := make(chan struct{})
	observability.GoSafe(ctx, func(ctx context.Context) {
		defer close(stopDone)
		p.stopStream(ctx)
	})

	// Verify that IsBackendEnabled was called (i.e., the goroutine started).
	require.Eventually(t, func() bool {
		return isBackendEnabledCalled.Load()
	}, 5*time.Second, 100*time.Millisecond, "expected IsBackendEnabled to be called")

	// Now let the background work complete.
	close(unblock)

	// Wait for the goroutine to finish.
	select {
	case <-stopDone:
	case <-time.After(5 * time.Second):
		t.Fatal("stopStream goroutine did not complete")
	}
}
