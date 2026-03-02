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
	"github.com/goccy/go-yaml"
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
	app       fyne.App
	panel     *Panel
	streamD   *streamd.StreamD
	mockClock *benbjohnsonclock.Mock
}

// eventuallyWithClock wraps require.Eventually and advances the mock clock
// on each poll iteration to unblock clock-dependent operations.
func (env *ytE2EEnv) eventuallyWithClock(t *testing.T, condition func() bool, waitFor, tick time.Duration, msgAndArgs ...interface{}) {
	t.Helper()
	require.Eventually(t, func() bool {
		env.mockClock.Add(time.Second)
		return condition()
	}, waitFor, tick, msgAndArgs...)
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
	// Use the real clock for token expiry because oauth2 uses time.Now()
	// internally, not the mock clock.
	expiry := time.Now().Add(24 * time.Hour)
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
	ctx := t.Context()
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

	// Wait for YouTube mock streams to become available.
	// We must wait specifically for YouTube streams (not just any streams)
	// because getStreamsForAccount returns (nil,nil) when a controller is not
	// yet registered, and that empty result gets cached for 30 minutes.
	ytAccountID := streamcontrol.NewAccountIDFullyQualified(youtube.ID, "yt1")
	require.Eventually(t, func() bool {
		mockClock.Add(time.Second)
		// Invalidate cache on each poll so a previously-cached empty result
		// does not prevent us from detecting when the controller is ready.
		d.StreamsCache.InvalidateCache(ctx)
		streams, _ := d.GetStreams(ctx, ytAccountID)
		return len(streams) >= 1
	}, 15*time.Second, 100*time.Millisecond, "expected at least 1 stream from mock YouTube")

	// Create test Fyne app and Panel.
	testApp := test.NewApp()
	p, err := New("", OptionApp{App: testApp})
	require.NoError(t, err)
	p.app = testApp
	p.StreamD = d
	p.defaultContext = ctx

	// Keep a sentinel window alive for the duration of the test suite.
	// Background goroutines spawned by stream management UI (via
	// observability.Go) may outlive their source window. The Fyne test
	// driver's CanvasForObject panics when AllWindows() is empty
	// (index out of range [-1]). A permanent sentinel window prevents
	// this by ensuring AllWindows() is never empty.
	sentinel := testApp.NewWindow("sentinel")
	sentinel.Resize(fyne.NewSize(1, 1))
	sentinel.Show()
	t.Cleanup(func() { sentinel.Close() })

	// Initialize config cache.
	cachedCfg, err := d.GetConfig(ctx)
	require.NoError(t, err)
	p.configCacheLocker.Do(ctx, func() {
		p.configCache = cachedCfg
	})

	return &ytE2EEnv{
		t:         t,
		ctx:       ctx,
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

// findButtonInRowWithLabel traverses the window's content tree looking for an HBox
// container that contains both a widget.Label with the given labelText and a
// widget.Button with the given buttonText. Returns the matching button or nil.
func findButtonInRowWithLabel(w fyne.Window, labelText, buttonText string) *widget.Button {
	var found *widget.Button
	traverse(w.Content(), func(obj fyne.CanvasObject) {
		if found != nil {
			return
		}
		cont, ok := obj.(*fyne.Container)
		if !ok {
			return
		}
		// Check if this container has both the target label and target button.
		var hasLabel bool
		var btn *widget.Button
		for _, child := range cont.Objects {
			if lbl, ok := child.(*widget.Label); ok && lbl.Text == labelText {
				hasLabel = true
			}
			if b, ok := child.(*widget.Button); ok && b.Text == buttonText {
				btn = b
			}
		}
		if hasLabel && btn != nil {
			found = btn
		}
	})
	return found
}

// findOverlayPopUp searches all windows' canvas overlays for the topmost
// *widget.PopUp. The stream management code places modal popups on
// AllWindows()[0], which may differ from the edit window.
func findOverlayPopUp(app fyne.App) *widget.PopUp {
	for _, w := range app.Driver().AllWindows() {
		overlayObj := w.Canvas().Overlays().Top()
		if overlayObj == nil {
			continue
		}
		if popup, ok := overlayObj.(*widget.PopUp); ok {
			return popup
		}
	}
	return nil
}

// closeAllWindows safely closes all open windows except the sentinel.
// Since Close() modifies the internal windows slice, we snapshot the list
// first and close in reverse order.
func closeAllWindows(app fyne.App) {
	windows := append([]fyne.Window{}, app.Driver().AllWindows()...)
	for i := len(windows) - 1; i >= 0; i-- {
		if windows[i].Title() == "sentinel" {
			continue
		}
		windows[i].Close()
	}
}

// waitForWindowClosed polls until no window with the given title exists.
func waitForWindowClosed(t *testing.T, app fyne.App, title string) {
	t.Helper()
	require.Eventually(t, func() bool {
		for _, w := range app.Driver().AllWindows() {
			if w.Title() == title {
				return false
			}
		}
		return true
	}, 10*time.Second, 100*time.Millisecond, "window %q still open", title)
}

func TestYouTubeSettingsE2E(t *testing.T) {
	env := setupYouTubeE2E(t)

	t.Run("OpenAccountManagement", func(t *testing.T) {
		env.panel.OpenAccountManagementWindow(env.ctx)

		w := waitForWindow(t, env.app, "Account Management")

		// Verify YouTube platform header is visible.
		platformLabel := findLabelByText(w, "Platform: youtube")
		require.NotNil(t, platformLabel, "expected 'Platform: youtube' label in account management window")

		// Verify existing "yt1" account is shown.
		accountLabel := findLabelByText(w, "yt1")
		require.NotNil(t, accountLabel, "expected 'yt1' account label in account management window")

		w.Close()
	})

	t.Run("AddYouTubeAccount", func(t *testing.T) {
		env.panel.OpenAccountManagementWindow(env.ctx)

		acctMgmtWindow := waitForWindow(t, env.app, "Account Management")

		// Find all "Add account" buttons — one per platform (obs, twitch, kick, youtube).
		addButtons := findAllButtonsByText(acctMgmtWindow, "Add account")
		require.Len(t, addButtons, 4, "expected 4 'Add account' buttons (one per platform)")

		// Tap the 4th button (YouTube, index 3).
		test.Tap(addButtons[3])

		// Wait for the "Add youtube account" window.
		addWindow := waitForWindow(t, env.app, "Add youtube account")

		// Fill in the Account ID.
		accountIDEntry := findEntryByPlaceholder(addWindow, "Account ID (e.g. 'myaccount1')")
		require.NotNil(t, accountIDEntry, "expected Account ID entry field")
		accountIDEntry.SetText("yt-new")

		// Fill in Client ID.
		clientIDEntry := findEntryByPlaceholder(addWindow, "client ID")
		require.NotNil(t, clientIDEntry, "expected client ID entry field")
		clientIDEntry.SetText("new-client-id")

		// Fill in Client Secret.
		clientSecretEntry := findEntryByPlaceholder(addWindow, "client secret")
		require.NotNil(t, clientSecretEntry, "expected client secret entry field")
		clientSecretEntry.SetText("new-client-secret")

		// Tap "Add account" button in the add window.
		addBtn := findButtonByText(addWindow, "Add account")
		require.NotNil(t, addBtn, "expected 'Add account' button in add window")
		test.Tap(addBtn)

		// Verify the account management window now shows "yt-new".
		require.Eventually(t, func() bool {
			return findLabelByText(acctMgmtWindow, "yt-new") != nil
		}, 5*time.Second, 100*time.Millisecond, "expected 'yt-new' label to appear in account management window")

		// Close windows explicitly (tapping "Add account" already closes addWindow).
		acctMgmtWindow.Close()
	})

	t.Run("EditYouTubeAccount", func(t *testing.T) {
		env.panel.OpenAccountManagementWindow(env.ctx)

		acctMgmtWindow := waitForWindow(t, env.app, "Account Management")

		// Find and tap the "Edit" button in the same row as "yt1".
		editBtn := findButtonInRowWithLabel(acctMgmtWindow, "yt1", "Edit")
		require.NotNil(t, editBtn, "expected 'Edit' button in the same row as 'yt1'")
		test.Tap(editBtn)

		// Wait for the edit window.
		editWindowTitle := "Edit youtube account: yt1"
		editWindow := waitForWindow(t, env.app, editWindowTitle)

		// Find the Client ID entry and change its text.
		clientIDEntry := findEntryByPlaceholder(editWindow, "client ID")
		require.NotNil(t, clientIDEntry, "expected client ID entry field in edit window")
		clientIDEntry.SetText("updated-client-id")

		// Tap "Save".
		saveBtn := findButtonByText(editWindow, "Save")
		require.NotNil(t, saveBtn, "expected 'Save' button in edit window")
		test.Tap(saveBtn)

		// Verify edit window closes.
		waitForWindowClosed(t, env.app, editWindowTitle)

		// Close account management window.
		acctMgmtWindow.Close()
	})

	t.Run("DeleteYouTubeAccount", func(t *testing.T) {
		env.panel.OpenAccountManagementWindow(env.ctx)

		acctMgmtWindow := waitForWindow(t, env.app, "Account Management")

		// First, add a temporary account to delete.
		addButtons := findAllButtonsByText(acctMgmtWindow, "Add account")
		require.Len(t, addButtons, 4, "expected 4 'Add account' buttons (one per platform)")

		// Tap the 4th button (YouTube, index 3).
		test.Tap(addButtons[3])

		addWindow := waitForWindow(t, env.app, "Add youtube account")

		// Fill in temp account details.
		accountIDEntry := findEntryByPlaceholder(addWindow, "Account ID (e.g. 'myaccount1')")
		require.NotNil(t, accountIDEntry, "expected Account ID entry field")
		accountIDEntry.SetText("yt-temp")

		clientIDEntry := findEntryByPlaceholder(addWindow, "client ID")
		require.NotNil(t, clientIDEntry, "expected client ID entry field")
		clientIDEntry.SetText("temp-id")

		clientSecretEntry := findEntryByPlaceholder(addWindow, "client secret")
		require.NotNil(t, clientSecretEntry, "expected client secret entry field")
		clientSecretEntry.SetText("temp-secret")

		addBtn := findButtonByText(addWindow, "Add account")
		require.NotNil(t, addBtn, "expected 'Add account' button in add window")
		test.Tap(addBtn)

		// Verify "yt-temp" appears.
		require.Eventually(t, func() bool {
			return findLabelByText(acctMgmtWindow, "yt-temp") != nil
		}, 5*time.Second, 100*time.Millisecond, "expected 'yt-temp' label to appear")

		// Find and tap the "Delete" button for "yt-temp".
		deleteBtn := findButtonInRowWithLabel(acctMgmtWindow, "yt-temp", "Delete")
		require.NotNil(t, deleteBtn, "expected 'Delete' button in the same row as 'yt-temp'")
		test.Tap(deleteBtn)

		// Verify "yt-temp" is no longer in the list.
		require.Eventually(t, func() bool {
			return findLabelByText(acctMgmtWindow, "yt-temp") == nil
		}, 5*time.Second, 100*time.Millisecond, "expected 'yt-temp' label to disappear after delete")

		// Close account management window.
		acctMgmtWindow.Close()
	})

	t.Run("StreamList", func(t *testing.T) {
		env.panel.OpenAccountManagementWindow(env.ctx)

		acctMgmtWindow := waitForWindow(t, env.app, "Account Management")

		// Find and tap the "Edit" button in the same row as "yt1".
		editBtn := findButtonInRowWithLabel(acctMgmtWindow, "yt1", "Edit")
		require.NotNil(t, editBtn, "expected 'Edit' button in the same row as 'yt1'")
		test.Tap(editBtn)

		// Wait for the edit window.
		editWin := waitForWindow(t, env.app, "Edit youtube account: yt1")

		// Wait for streams to load asynchronously. Advance the mock clock to
		// unblock any clock-dependent operations in the background.
		var stream1Check *widget.Check
		env.eventuallyWithClock(t, func() bool {
			stream1Check = findCheckByLabel(editWin, "Stream 1")
			return stream1Check != nil
		}, 15*time.Second, 100*time.Millisecond, "expected 'Stream 1' checkbox to appear")

		var stream2Check *widget.Check
		env.eventuallyWithClock(t, func() bool {
			stream2Check = findCheckByLabel(editWin, "Stream 2")
			return stream2Check != nil
		}, 10*time.Second, 100*time.Millisecond, "expected 'Stream 2' checkbox to appear")

		// Verify both checkboxes exist.
		require.NotNil(t, stream1Check, "'Stream 1' checkbox must exist")
		require.NotNil(t, stream2Check, "'Stream 2' checkbox must exist")

		// Close windows.
		editWin.Close()
		acctMgmtWindow.Close()
	})

	t.Run("CreateStream", func(t *testing.T) {
		// Record stream count before creation.
		streamsBefore, err := env.streamD.GetStreams(env.ctx)
		require.NoError(t, err)
		countBefore := len(streamsBefore)

		env.panel.OpenAccountManagementWindow(env.ctx)

		acctMgmtWindow := waitForWindow(t, env.app, "Account Management")

		// Find and tap the "Edit" button in the same row as "yt1".
		editBtn := findButtonInRowWithLabel(acctMgmtWindow, "yt1", "Edit")
		require.NotNil(t, editBtn, "expected 'Edit' button in the same row as 'yt1'")
		test.Tap(editBtn)

		// Wait for the edit window.
		editWin := waitForWindow(t, env.app, "Edit youtube account: yt1")

		// Wait for streams to load.
		env.eventuallyWithClock(t, func() bool {
			return findCheckByLabel(editWin, "Stream 1") != nil
		}, 15*time.Second, 100*time.Millisecond, "expected 'Stream 1' checkbox to appear")

		// Find and tap "Create Stream" button.
		createBtn := findButtonByText(editWin, "Create Stream")
		require.NotNil(t, createBtn, "expected 'Create Stream' button")
		test.Tap(createBtn)

		// The "Create Stream" button opens a ModalPopUp on the first window's
		// canvas (AllWindows()[0]), which may be acctMgmtWindow rather than
		// editWin. Use findOverlayPopUp to search all windows.
		var titleEntry *widget.Entry
		env.eventuallyWithClock(t, func() bool {
			popup := findOverlayPopUp(env.app)
			if popup == nil {
				return false
			}
			traverse(popup.Content, func(obj fyne.CanvasObject) {
				if entry, ok := obj.(*widget.Entry); ok && entry.PlaceHolder == "Stream title" {
					titleEntry = entry
				}
			})
			return titleEntry != nil
		}, 10*time.Second, 100*time.Millisecond, "expected 'Stream title' entry in overlay")

		// Type the new stream title.
		titleEntry.SetText("Test Stream 3")

		// Find and tap "Create" button in the overlay.
		var createConfirmBtn *widget.Button
		popup := findOverlayPopUp(env.app)
		require.NotNil(t, popup, "expected overlay popup to still be present")
		traverse(popup.Content, func(obj fyne.CanvasObject) {
			if btn, ok := obj.(*widget.Button); ok && btn.Text == "Create" {
				createConfirmBtn = btn
			}
		})
		require.NotNil(t, createConfirmBtn, "expected 'Create' button in overlay")
		test.Tap(createConfirmBtn)

		// The popup's "Create" handler calls AllWindows()[-1].Close() which
		// closes a real window (side effect of the production code). Verify
		// creation via the backend instead of looking at the (possibly closed)
		// edit window.
		env.eventuallyWithClock(t, func() bool {
			env.streamD.StreamsCache.InvalidateCache(env.ctx)
			streams, err := env.streamD.GetStreams(env.ctx)
			if err != nil {
				return false
			}
			return len(streams) > countBefore
		}, 15*time.Second, 100*time.Millisecond, "expected stream count to increase after creation")

		// Verify the new stream exists in the backend.
		streams, err := env.streamD.GetStreams(env.ctx)
		require.NoError(t, err)
		found := false
		for _, s := range streams {
			if s.Name == "Test Stream 3" {
				found = true
				break
			}
		}
		require.True(t, found, "expected 'Test Stream 3' to exist in backend")

		// Close remaining windows (the popup handler may have already closed one).
		// Close in reverse order to avoid index issues as Close modifies the slice.
		closeAllWindows(env.app)
	})

	t.Run("ToggleAllowlist", func(t *testing.T) {
		env.panel.OpenAccountManagementWindow(env.ctx)

		acctMgmtWindow := waitForWindow(t, env.app, "Account Management")

		// Find and tap the "Edit" button in the same row as "yt1".
		editBtn := findButtonInRowWithLabel(acctMgmtWindow, "yt1", "Edit")
		require.NotNil(t, editBtn, "expected 'Edit' button in the same row as 'yt1'")
		test.Tap(editBtn)

		// Wait for the edit window.
		editWin := waitForWindow(t, env.app, "Edit youtube account: yt1")

		// Wait for streams to load and find a stream checkbox.
		var streamCheck *widget.Check
		var streamName string
		env.eventuallyWithClock(t, func() bool {
			// Find any stream checkbox (the exact streams available depend on
			// earlier test ordering, so we pick the first one we see).
			traverse(editWin.Content(), func(obj fyne.CanvasObject) {
				if streamCheck != nil {
					return
				}
				if chk, ok := obj.(*widget.Check); ok && chk.Text != "" && chk.Text != "Auto-numerate" {
					streamCheck = chk
					streamName = chk.Text
				}
			})
			return streamCheck != nil
		}, 15*time.Second, 100*time.Millisecond, "expected at least one stream checkbox to appear")

		// Toggle the checkbox on.
		require.False(t, streamCheck.Checked, "stream checkbox should initially be unchecked")
		test.Tap(streamCheck)
		require.True(t, streamCheck.Checked, "stream checkbox should now be checked")

		// Tap "Save" to persist the edit (updates in-memory config).
		saveBtn := findButtonByText(editWin, "Save")
		require.NotNil(t, saveBtn, "expected 'Save' button in edit window")
		test.Tap(saveBtn)

		// Wait for edit window to close.
		waitForWindowClosed(t, env.app, "Edit youtube account: yt1")

		// Tap "Save and Close" on the Account Management window to persist
		// the config to the StreamD backend.
		saveAndCloseBtn := findButtonByText(acctMgmtWindow, "Save and Close")
		require.NotNil(t, saveAndCloseBtn, "expected 'Save and Close' button")
		test.Tap(saveAndCloseBtn)

		// Wait for Account Management window to close.
		waitForWindowClosed(t, env.app, "Account Management")

		// Verify AllowlistedStreamIDs in the persisted config.
		cfg, err := env.streamD.GetConfig(env.ctx)
		require.NoError(t, err)

		ytPlatCfg := cfg.Backends[youtube.ID]
		require.NotNil(t, ytPlatCfg, "expected youtube platform config")
		ytAccRaw, ok := ytPlatCfg.Accounts["yt1"]
		require.True(t, ok, "expected 'yt1' account in config")

		var ytAccCfg youtube.AccountConfig
		err = yaml.Unmarshal(ytAccRaw, &ytAccCfg)
		require.NoError(t, err)

		// Verify that the allowlist is non-empty (we checked exactly one
		// stream). The allowlist stores stream IDs (e.g. "stream-1"), not
		// display names, so we verify the count rather than matching by name.
		require.NotEmpty(t, ytAccCfg.AllowlistedStreamIDs,
			"expected AllowlistedStreamIDs to contain at least one stream after checking %q", streamName)
	})

	t.Run("SaveAndClose", func(t *testing.T) {
		// Get the config before to compare later.
		cfgBefore, err := env.streamD.GetConfig(env.ctx)
		require.NoError(t, err)

		env.panel.OpenAccountManagementWindow(env.ctx)

		acctMgmtWindow := waitForWindow(t, env.app, "Account Management")

		// Tap "Save and Close".
		saveAndCloseBtn := findButtonByText(acctMgmtWindow, "Save and Close")
		require.NotNil(t, saveAndCloseBtn, "expected 'Save and Close' button")
		test.Tap(saveAndCloseBtn)

		// Verify the window closes.
		waitForWindowClosed(t, env.app, "Account Management")

		// Verify config is still persisted (it should at least match what
		// was there before, since we didn't make changes).
		cfgAfter, err := env.streamD.GetConfig(env.ctx)
		require.NoError(t, err)
		require.NotNil(t, cfgAfter)
		require.NotNil(t, cfgAfter.Backends[youtube.ID], "youtube config should still exist after Save and Close")

		// Verify the yt1 account still exists.
		_, ok := cfgAfter.Backends[youtube.ID].Accounts["yt1"]
		require.True(t, ok, "expected 'yt1' account to still exist after Save and Close")

		// Verify that the allowlist from the ToggleAllowlist test persisted.
		ytAccRawBefore := cfgBefore.Backends[youtube.ID].Accounts["yt1"]
		ytAccRawAfter := cfgAfter.Backends[youtube.ID].Accounts["yt1"]
		var ytCfgBefore, ytCfgAfter youtube.AccountConfig
		require.NoError(t, yaml.Unmarshal(ytAccRawBefore, &ytCfgBefore))
		require.NoError(t, yaml.Unmarshal(ytAccRawAfter, &ytCfgAfter))
		require.Equal(t, ytCfgBefore.AllowlistedStreamIDs, ytCfgAfter.AllowlistedStreamIDs,
			"AllowlistedStreamIDs should be preserved through Save and Close")
	})

	t.Run("Search", func(t *testing.T) {
		env.panel.OpenAccountManagementWindow(env.ctx)

		acctMgmtWindow := waitForWindow(t, env.app, "Account Management")

		// Find and tap the "Edit" button in the same row as "yt1".
		editBtn := findButtonInRowWithLabel(acctMgmtWindow, "yt1", "Edit")
		require.NotNil(t, editBtn, "expected 'Edit' button in the same row as 'yt1'")
		test.Tap(editBtn)

		// Wait for the edit window.
		editWin := waitForWindow(t, env.app, "Edit youtube account: yt1")

		// Wait for streams to load. Collect all stream check labels.
		var allStreamNames []string
		env.eventuallyWithClock(t, func() bool {
			allStreamNames = nil
			traverse(editWin.Content(), func(obj fyne.CanvasObject) {
				if chk, ok := obj.(*widget.Check); ok && chk.Text != "" && chk.Text != "Auto-numerate" {
					allStreamNames = append(allStreamNames, chk.Text)
				}
			})
			return len(allStreamNames) >= 2
		}, 15*time.Second, 100*time.Millisecond, "expected at least 2 stream checkboxes")

		// Find the search entry and type a filter.
		searchEntry := findEntryByPlaceholder(editWin, "Search streams...")
		require.NotNil(t, searchEntry, "expected 'Search streams...' entry")

		// Use the first character that distinguishes the streams. The mock
		// streams have names like "Stream 1", "Stream 2", "Test Stream 3".
		// Typing "1" should match only "Stream 1".
		searchEntry.SetText("1")

		// Wait for the filtered results. The OnChanged handler triggers
		// refreshStreams which spawns a goroutine.
		var filteredNames []string
		env.eventuallyWithClock(t, func() bool {
			filteredNames = nil
			traverse(editWin.Content(), func(obj fyne.CanvasObject) {
				if chk, ok := obj.(*widget.Check); ok && chk.Text != "" && chk.Text != "Auto-numerate" {
					filteredNames = append(filteredNames, chk.Text)
				}
			})
			return len(filteredNames) < len(allStreamNames)
		}, 15*time.Second, 100*time.Millisecond, "expected search filter to reduce visible streams")

		// Verify filtered results all contain "1".
		for _, name := range filteredNames {
			require.Contains(t, name, "1", "filtered stream %q should contain '1'", name)
		}

		// Clear search and verify all streams reappear.
		searchEntry.SetText("")
		env.eventuallyWithClock(t, func() bool {
			var names []string
			traverse(editWin.Content(), func(obj fyne.CanvasObject) {
				if chk, ok := obj.(*widget.Check); ok && chk.Text != "" && chk.Text != "Auto-numerate" {
					names = append(names, chk.Text)
				}
			})
			return len(names) >= len(allStreamNames)
		}, 15*time.Second, 100*time.Millisecond, "expected all streams to reappear after clearing search")

		editWin.Close()
		acctMgmtWindow.Close()
	})

	t.Run("DeleteStream", func(t *testing.T) {
		// Count streams before deletion.
		streamsBefore, err := env.streamD.GetStreams(env.ctx)
		require.NoError(t, err)
		countBefore := len(streamsBefore)
		require.Greater(t, countBefore, 0, "need at least one stream to delete")

		env.panel.OpenAccountManagementWindow(env.ctx)

		acctMgmtWindow := waitForWindow(t, env.app, "Account Management")

		// Find and tap the "Edit" button in the same row as "yt1".
		editBtn := findButtonInRowWithLabel(acctMgmtWindow, "yt1", "Edit")
		require.NotNil(t, editBtn, "expected 'Edit' button in the same row as 'yt1'")
		test.Tap(editBtn)

		// Wait for the edit window.
		editWin := waitForWindow(t, env.app, "Edit youtube account: yt1")

		// Wait for streams to load.
		env.eventuallyWithClock(t, func() bool {
			return findCheckByLabel(editWin, "Stream 1") != nil
		}, 15*time.Second, 100*time.Millisecond, "expected 'Stream 1' checkbox to appear")

		// Find a delete button. Each stream row is: HBox(check, spacer, deleteButton)
		// The delete button has empty text and DeleteIcon.
		var deleteBtn *widget.Button
		traverse(editWin.Content(), func(obj fyne.CanvasObject) {
			if deleteBtn != nil {
				return
			}
			if btn, ok := obj.(*widget.Button); ok && btn.Text == "" && btn.Icon != nil {
				deleteBtn = btn
			}
		})
		require.NotNil(t, deleteBtn, "expected a delete button (empty text, icon) in stream list")
		test.Tap(deleteBtn)

		// A confirmation modal popup appears on AllWindows()[0]. Use
		// findOverlayPopUp to search all windows.
		var confirmDeleteBtn *widget.Button
		env.eventuallyWithClock(t, func() bool {
			popup := findOverlayPopUp(env.app)
			if popup == nil {
				return false
			}
			traverse(popup.Content, func(obj fyne.CanvasObject) {
				if btn, ok := obj.(*widget.Button); ok && btn.Text == "Delete" {
					confirmDeleteBtn = btn
				}
			})
			return confirmDeleteBtn != nil
		}, 10*time.Second, 100*time.Millisecond, "expected 'Delete' button in confirmation overlay")
		test.Tap(confirmDeleteBtn)

		// The popup's "Delete" handler calls AllWindows()[-1].Close() which
		// closes a real window, then spawns a goroutine to delete the stream.
		// Verify deletion via the backend directly.
		env.eventuallyWithClock(t, func() bool {
			env.streamD.StreamsCache.InvalidateCache(env.ctx)
			streams, err := env.streamD.GetStreams(env.ctx)
			if err != nil {
				return false
			}
			return len(streams) < countBefore
		}, 15*time.Second, 100*time.Millisecond, "expected stream count to decrease after deletion")

		// Close remaining windows (the popup handler may have already closed one).
		// Close in reverse order to avoid index issues as Close modifies the slice.
		closeAllWindows(env.app)
	})
}
