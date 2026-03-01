package streampanel

import (
	"context"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	benbjohnsonclock "github.com/benbjohnson/clock"
	"github.com/facebookincubator/go-belt"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/clock"
	"github.com/xaionaro-go/streamctl/pkg/secret"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"golang.org/x/oauth2"
)

// ytE2EEnv holds the shared test environment for YouTube settings E2E tests.
type ytE2EEnv struct {
	t         *testing.T
	ctx       context.Context
	cancel    context.CancelFunc
	app       fyne.App
	panel     *Panel
	streamD   *streamd.StreamD
	mockClock *benbjohnsonclock.Mock
}

// setupYouTubeE2E creates a shared test environment with a real StreamD instance
// using mock clients, a test Fyne app, and a configured Panel.
func setupYouTubeE2E(t *testing.T) *ytE2EEnv {
	t.Helper()

	// Enable mock clients for all platforms.
	youtube.SetDebugUseMockClient(true)
	twitch.SetDebugUseMockClient(true)
	kick.SetDebugUseMockClient(true)
	obs.SetDebugUseMockClient(true)
	t.Cleanup(func() {
		youtube.SetDebugUseMockClient(false)
		twitch.SetDebugUseMockClient(false)
		kick.SetDebugUseMockClient(false)
		obs.SetDebugUseMockClient(false)
	})

	// Set up a mock clock.
	mockClock := benbjohnsonclock.NewMock()
	clock.Set(mockClock)

	// Build config with one YouTube account and minimal platform accounts.
	expiry := mockClock.Now().Add(24 * time.Hour)
	ytToken := secret.New(oauth2.Token{AccessToken: "dummy", Expiry: expiry})

	cfg := config.Config{
		Backends: make(map[streamcontrol.PlatformID]*streamcontrol.AbstractPlatformConfig),
	}

	cfg.Backends[youtube.ID] = &streamcontrol.AbstractPlatformConfig{
		Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
			"yt1": streamcontrol.ToRawMessage(youtube.AccountConfig{
				ClientID:     "test-client-id",
				ClientSecret: secret.New("test-client-secret"),
				Token:        &ytToken,
			}),
		},
	}

	cfg.Backends[twitch.ID] = &streamcontrol.AbstractPlatformConfig{
		Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
			"tw1": streamcontrol.ToRawMessage(twitch.AccountConfig{
				ClientID:     "tw-id",
				ClientSecret: secret.New("tw-secret"),
				Channel:      "twchan",
				AuthType:     "user",
			}),
		},
	}

	cfg.Backends[kick.ID] = &streamcontrol.AbstractPlatformConfig{
		Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
			"ki1": streamcontrol.ToRawMessage(kick.AccountConfig{
				Channel:      "kickchan",
				ClientID:     "ki-id",
				ClientSecret: secret.New("ki-secret"),
			}),
		},
	}

	cfg.Backends[obs.ID] = &streamcontrol.AbstractPlatformConfig{
		Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
			"obs1": streamcontrol.ToRawMessage(obs.AccountConfig{Host: "localhost", Port: 4455}),
		},
	}

	// Initialize StreamD.
	ctx, cancel := context.WithCancel(context.Background())
	ctx = observability.WithSecretsProvider(ctx, &observability.SecretsStaticProvider{})
	b := belt.New()

	d, err := streamd.New(cfg, &mockUI{}, func(ctx context.Context, cfg config.Config) error {
		return nil
	}, b)
	require.NoError(t, err)

	d.AddOAuthListenPort(8091)
	d.AddOAuthListenPort(8092)

	// Run StreamD in background.
	go func() {
		runErr := d.Run(ctx)
		if runErr != nil && runErr != context.Canceled {
			t.Errorf("StreamD.Run returned error: %v", runErr)
		}
	}()

	// Wait for mock streams to become available.
	require.Eventually(t, func() bool {
		mockClock.Add(time.Second)
		streams, _ := d.GetStreams(ctx)
		return len(streams) >= 1
	}, 15*time.Second, 100*time.Millisecond, "expected at least 1 stream from mock YouTube")

	// Create test Fyne app and Panel.
	testApp := test.NewApp()
	p, err := New("", OptionApp{App: testApp})
	require.NoError(t, err)
	p.app = testApp
	p.StreamD = d

	// Initialize config cache.
	cachedCfg, err := d.GetConfig(ctx)
	require.NoError(t, err)
	p.configCacheLocker.Do(ctx, func() {
		p.configCache = cachedCfg
	})

	t.Cleanup(func() {
		cancel()
		testApp.Quit()
	})

	return &ytE2EEnv{
		t:         t,
		ctx:       ctx,
		cancel:    cancel,
		app:       testApp,
		panel:     p,
		streamD:   d,
		mockClock: mockClock,
	}
}

// findButtonByText traverses the window's content tree and returns the first
// widget.Button whose Text matches the given string.
func findButtonByText(w fyne.Window, text string) *widget.Button {
	var found *widget.Button
	traverse(w.Content(), func(obj fyne.CanvasObject) {
		if btn, ok := obj.(*widget.Button); ok && btn.Text == text {
			if found == nil {
				found = btn
			}
		}
	})
	return found
}

// findEntryByPlaceholder traverses the window's content tree and returns the first
// widget.Entry whose PlaceHolder matches the given string.
func findEntryByPlaceholder(w fyne.Window, placeholder string) *widget.Entry {
	var found *widget.Entry
	traverse(w.Content(), func(obj fyne.CanvasObject) {
		if entry, ok := obj.(*widget.Entry); ok && entry.PlaceHolder == placeholder {
			if found == nil {
				found = entry
			}
		}
	})
	return found
}

// findCheckByLabel traverses the window's content tree and returns the first
// widget.Check whose Text matches the given label.
func findCheckByLabel(w fyne.Window, label string) *widget.Check {
	var found *widget.Check
	traverse(w.Content(), func(obj fyne.CanvasObject) {
		if chk, ok := obj.(*widget.Check); ok && chk.Text == label {
			if found == nil {
				found = chk
			}
		}
	})
	return found
}

// findLabelByText traverses the window's content tree and returns the first
// widget.Label whose Text matches the given string.
func findLabelByText(w fyne.Window, text string) *widget.Label {
	var found *widget.Label
	traverse(w.Content(), func(obj fyne.CanvasObject) {
		if lbl, ok := obj.(*widget.Label); ok && lbl.Text == text {
			if found == nil {
				found = lbl
			}
		}
	})
	return found
}

// waitForWindow polls until a window with the given title appears in the app.
func waitForWindow(t *testing.T, app fyne.App, title string) fyne.Window {
	t.Helper()
	var found fyne.Window
	require.Eventually(t, func() bool {
		for _, w := range app.Driver().AllWindows() {
			if w.Title() == title {
				found = w
				return true
			}
		}
		return false
	}, 10*time.Second, 100*time.Millisecond, "window %q not found", title)
	return found
}

// countChecks counts the number of widget.Check instances in the window's content tree.
func countChecks(w fyne.Window) int {
	count := 0
	traverse(w.Content(), func(obj fyne.CanvasObject) {
		if _, ok := obj.(*widget.Check); ok {
			count++
		}
	})
	return count
}

// findAllButtonsByText traverses the window's content tree and returns all
// widget.Button instances whose Text matches the given string.
func findAllButtonsByText(w fyne.Window, text string) []*widget.Button {
	var buttons []*widget.Button
	traverse(w.Content(), func(obj fyne.CanvasObject) {
		if btn, ok := obj.(*widget.Button); ok && btn.Text == text {
			buttons = append(buttons, btn)
		}
	})
	return buttons
}

func TestYouTubeSettingsE2E(t *testing.T) {
	env := setupYouTubeE2E(t)

	t.Run("OpenAccountManagement", func(t *testing.T) {
		env.panel.OpenAccountManagementWindow(env.ctx)

		w := waitForWindow(t, env.app, "Account Management")
		defer w.Close()

		// Verify YouTube platform header is visible.
		platformLabel := findLabelByText(w, "Platform: youtube")
		require.NotNil(t, platformLabel, "expected 'Platform: youtube' label in account management window")

		// Verify existing "yt1" account is shown.
		accountLabel := findLabelByText(w, "yt1")
		require.NotNil(t, accountLabel, "expected 'yt1' account label in account management window")
	})

	t.Run("AddYouTubeAccount", func(t *testing.T) {
		env.panel.OpenAccountManagementWindow(env.ctx)

		acctMgmtWindow := waitForWindow(t, env.app, "Account Management")
		defer acctMgmtWindow.Close()

		// Find all "Add account" buttons — one per platform (obs, twitch, kick, youtube).
		addButtons := findAllButtonsByText(acctMgmtWindow, "Add account")
		require.Len(t, addButtons, 4, "expected 4 'Add account' buttons (one per platform)")

		t.Log("About to tap YouTube Add account button")
		// Tap the 4th button (YouTube, index 3).
		test.Tap(addButtons[3])
		t.Log("Tapped YouTube Add account button")

		// Wait for the "Add youtube account" window.
		addWindow := waitForWindow(t, env.app, "Add youtube account")
		t.Log("Found Add youtube account window")
		defer addWindow.Close()

		// Fill in the Account ID.
		t.Log("Looking for Account ID entry...")
		accountIDEntry := findEntryByPlaceholder(addWindow, "Account ID (e.g. 'myaccount1')")
		require.NotNil(t, accountIDEntry, "expected Account ID entry field")
		accountIDEntry.SetText("yt-new")
		t.Log("Set Account ID to yt-new")

		// Fill in Client ID.
		t.Log("Looking for Client ID entry...")
		clientIDEntry := findEntryByPlaceholder(addWindow, "client ID")
		require.NotNil(t, clientIDEntry, "expected client ID entry field")
		clientIDEntry.SetText("new-client-id")
		t.Log("Set Client ID")

		// Fill in Client Secret.
		t.Log("Looking for Client Secret entry...")
		clientSecretEntry := findEntryByPlaceholder(addWindow, "client secret")
		require.NotNil(t, clientSecretEntry, "expected client secret entry field")
		clientSecretEntry.SetText("new-client-secret")
		t.Log("Set Client Secret")

		// Tap "Add account" button in the add window.
		t.Log("Looking for Add account button in add window...")
		addBtn := findButtonByText(addWindow, "Add account")
		require.NotNil(t, addBtn, "expected 'Add account' button in add window")
		t.Log("About to tap Add account button")
		test.Tap(addBtn)
		t.Log("Tapped Add account button")

		// Verify the account management window now shows "yt-new".
		t.Log("Waiting for yt-new label to appear...")
		require.Eventually(t, func() bool {
			return findLabelByText(acctMgmtWindow, "yt-new") != nil
		}, 5*time.Second, 100*time.Millisecond, "expected 'yt-new' label to appear in account management window")
		t.Log("Found yt-new label")
	})
}
