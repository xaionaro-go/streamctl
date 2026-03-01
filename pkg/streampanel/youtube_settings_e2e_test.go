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
	p.defaultContext = ctx

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

		// Wait for streams to load asynchronously.
		var stream1Check *widget.Check
		require.Eventually(t, func() bool {
			stream1Check = findCheckByLabel(editWin, "Stream 1")
			return stream1Check != nil
		}, 10*time.Second, 100*time.Millisecond, "expected 'Stream 1' checkbox to appear")

		var stream2Check *widget.Check
		require.Eventually(t, func() bool {
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
		env.panel.OpenAccountManagementWindow(env.ctx)

		acctMgmtWindow := waitForWindow(t, env.app, "Account Management")

		// Find and tap the "Edit" button in the same row as "yt1".
		editBtn := findButtonInRowWithLabel(acctMgmtWindow, "yt1", "Edit")
		require.NotNil(t, editBtn, "expected 'Edit' button in the same row as 'yt1'")
		test.Tap(editBtn)

		// Wait for the edit window.
		editWin := waitForWindow(t, env.app, "Edit youtube account: yt1")

		// Wait for streams to load.
		require.Eventually(t, func() bool {
			return findCheckByLabel(editWin, "Stream 1") != nil
		}, 10*time.Second, 100*time.Millisecond, "expected 'Stream 1' checkbox to appear")

		// Find and tap "Create Stream" button.
		createBtn := findButtonByText(editWin, "Create Stream")
		require.NotNil(t, createBtn, "expected 'Create Stream' button")
		test.Tap(createBtn)

		// The "Create Stream" button opens a ModalPopUp on the canvas overlay.
		var titleEntry *widget.Entry
		require.Eventually(t, func() bool {
			overlayObj := editWin.Canvas().Overlays().Top()
			if overlayObj == nil {
				return false
			}
			// The overlay is a *widget.PopUp with a Content field.
			if popup, ok := overlayObj.(*widget.PopUp); ok {
				traverse(popup.Content, func(obj fyne.CanvasObject) {
					if entry, ok := obj.(*widget.Entry); ok && entry.PlaceHolder == "Stream title" {
						titleEntry = entry
					}
				})
			}
			return titleEntry != nil
		}, 10*time.Second, 100*time.Millisecond, "expected 'Stream title' entry in overlay")

		// Type the new stream title.
		titleEntry.SetText("Test Stream 3")

		// Find and tap "Create" button in the overlay.
		var createConfirmBtn *widget.Button
		overlayObj := editWin.Canvas().Overlays().Top()
		if popup, ok := overlayObj.(*widget.PopUp); ok {
			traverse(popup.Content, func(obj fyne.CanvasObject) {
				if btn, ok := obj.(*widget.Button); ok && btn.Text == "Create" {
					createConfirmBtn = btn
				}
			})
		}
		require.NotNil(t, createConfirmBtn, "expected 'Create' button in overlay")
		test.Tap(createConfirmBtn)

		// Wait for the stream list to refresh — "Test Stream 3" should appear.
		require.Eventually(t, func() bool {
			return findCheckByLabel(editWin, "Test Stream 3") != nil
		}, 10*time.Second, 100*time.Millisecond, "expected 'Test Stream 3' checkbox to appear after creation")

		// Close windows.
		editWin.Close()
		acctMgmtWindow.Close()
	})

	t.Run("DeleteStream", func(t *testing.T) {
		env.panel.OpenAccountManagementWindow(env.ctx)

		acctMgmtWindow := waitForWindow(t, env.app, "Account Management")

		// Find and tap the "Edit" button in the same row as "yt1".
		editBtn := findButtonInRowWithLabel(acctMgmtWindow, "yt1", "Edit")
		require.NotNil(t, editBtn, "expected 'Edit' button in the same row as 'yt1'")
		test.Tap(editBtn)

		// Wait for the edit window.
		editWin := waitForWindow(t, env.app, "Edit youtube account: yt1")

		// Wait for streams to load.
		require.Eventually(t, func() bool {
			return findCheckByLabel(editWin, "Stream 1") != nil
		}, 10*time.Second, 100*time.Millisecond, "expected 'Stream 1' checkbox to appear")

		// Count streams before deletion.
		streamsBefore, err := env.streamD.GetStreams(env.ctx)
		require.NoError(t, err)
		countBefore := len(streamsBefore)

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

		// A confirmation modal popup appears. Find "Delete" button in overlay.
		var confirmDeleteBtn *widget.Button
		require.Eventually(t, func() bool {
			overlayObj := editWin.Canvas().Overlays().Top()
			if overlayObj == nil {
				return false
			}
			if popup, ok := overlayObj.(*widget.PopUp); ok {
				traverse(popup.Content, func(obj fyne.CanvasObject) {
					if btn, ok := obj.(*widget.Button); ok && btn.Text == "Delete" {
						confirmDeleteBtn = btn
					}
				})
			}
			return confirmDeleteBtn != nil
		}, 10*time.Second, 100*time.Millisecond, "expected 'Delete' button in confirmation overlay")
		test.Tap(confirmDeleteBtn)

		// Wait for stream count to decrease.
		require.Eventually(t, func() bool {
			streams, err := env.streamD.GetStreams(env.ctx)
			if err != nil {
				return false
			}
			return len(streams) < countBefore
		}, 10*time.Second, 100*time.Millisecond, "expected stream count to decrease after deletion")

		// Close windows.
		editWin.Close()
		acctMgmtWindow.Close()
	})
}
