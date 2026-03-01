# YouTube Settings E2E Tests — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** E2E tests for managing YouTube streams in Settings — account CRUD, stream CRUD, allowlisting, search, save flow.

**Architecture:** Real `streamd.StreamD` with mock platform clients (`youtube.SetDebugUseMockClient(true)` etc.). Fyne `test.NewApp()` for UI. `traverse()` + `require.Eventually` for async widget discovery. YouTube mock pre-seeds "Stream 1" and "Stream 2".

**Tech Stack:** Go testing, `fyne.io/fyne/v2/test`, `stretchr/testify`, `streamd.StreamD`, `streamcontrol/youtube` mock client.

---

### Task 1: Test scaffolding — shared setup & widget finder helpers

**Files:**
- Create: `pkg/streampanel/youtube_settings_e2e_test.go`

**Step 1: Write the test file with shared setup and helpers**

```go
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

// ytE2EEnv holds the shared state for YouTube settings E2E tests.
type ytE2EEnv struct {
	t         *testing.T
	ctx       context.Context
	cancel    context.CancelFunc
	app       fyne.App
	panel     *Panel
	streamD   *streamd.StreamD
	mockClock *benbjohnsonclock.Mock
}

func setupYouTubeE2E(t *testing.T) *ytE2EEnv {
	t.Helper()

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

	mockClock := benbjohnsonclock.NewMock()
	clock.Set(mockClock)

	expiry := mockClock.Now().Add(24 * time.Hour)
	ytToken := secret.New(oauth2.Token{AccessToken: "dummy", Expiry: expiry})

	cfg := config.Config{
		Backends: map[streamcontrol.PlatformID]*streamcontrol.AbstractPlatformConfig{
			youtube.ID: {
				Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
					"yt1": streamcontrol.ToRawMessage(youtube.AccountConfig{
						ClientID:     "test-client-id",
						ClientSecret: secret.New("test-client-secret"),
						Token:        &ytToken,
					}),
				},
			},
			twitch.ID: {
				Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
					"tw1": streamcontrol.ToRawMessage(twitch.AccountConfig{
						ClientID:     "id1",
						ClientSecret: secret.New("secret1"),
						Channel:      "chan1",
						AuthType:     "user",
					}),
				},
			},
			kick.ID: {
				Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
					"ki1": streamcontrol.ToRawMessage(kick.AccountConfig{
						Channel:      "chan1",
						ClientID:     "id1",
						ClientSecret: secret.New("secret1"),
					}),
				},
			},
			obs.ID: {
				Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
					"obs1": streamcontrol.ToRawMessage(obs.AccountConfig{Host: "localhost", Port: 4455}),
				},
			},
		},
	}

	ctx := t.Context()
	ctx = observability.WithSecretsProvider(ctx, &observability.SecretsStaticProvider{})
	b := belt.New()
	_ = b

	d, err := streamd.New(cfg, &mockUI{}, func(ctx context.Context, cfg config.Config) error {
		return nil
	}, b)
	require.NoError(t, err)

	d.AddOAuthListenPort(18091)
	d.AddOAuthListenPort(18092)

	ctx, cancel := context.WithCancel(ctx)
	go func() {
		_ = d.Run(ctx)
	}()

	// Wait for StreamD to initialize YouTube controller
	require.Eventually(t, func() bool {
		mockClock.Add(time.Second)
		streams, _ := d.GetStreams(ctx)
		return len(streams) >= 2 // Mock pre-seeds "Stream 1" and "Stream 2"
	}, 15*time.Second, 100*time.Millisecond)

	testApp := test.NewApp()
	p, err := New("", OptionApp{App: testApp})
	require.NoError(t, err)
	p.StreamD = d

	// Initialize configCache so GetStreamDConfig works
	sdCfg, err := d.GetConfig(ctx)
	require.NoError(t, err)
	p.configCacheLocker.Do(ctx, func() {
		p.configCache = sdCfg
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

// findButtonByText traverses a window's content tree and returns the first
// button whose text matches exactly. Returns nil if not found.
func findButtonByText(w fyne.Window, text string) *widget.Button {
	var found *widget.Button
	traverse(w.Content(), func(o fyne.CanvasObject) {
		if found != nil {
			return
		}
		if b, ok := o.(*widget.Button); ok && b.Text == text {
			found = b
		}
	})
	return found
}

// findEntryByPlaceholder traverses a window's content tree and returns the
// first Entry whose PlaceHolder matches exactly. Returns nil if not found.
func findEntryByPlaceholder(w fyne.Window, placeholder string) *widget.Entry {
	var found *widget.Entry
	traverse(w.Content(), func(o fyne.CanvasObject) {
		if found != nil {
			return
		}
		if e, ok := o.(*widget.Entry); ok && e.PlaceHolder == placeholder {
			found = e
		}
	})
	return found
}

// findCheckByLabel traverses a window's content tree and returns the first
// Check widget whose Text matches. Returns nil if not found.
func findCheckByLabel(w fyne.Window, label string) *widget.Check {
	var found *widget.Check
	traverse(w.Content(), func(o fyne.CanvasObject) {
		if found != nil {
			return
		}
		if c, ok := o.(*widget.Check); ok && c.Text == label {
			found = c
		}
	})
	return found
}

// findLabelByText traverses a window's content tree and returns the first
// Label whose Text matches. Returns nil if not found.
func findLabelByText(w fyne.Window, text string) *widget.Label {
	var found *widget.Label
	traverse(w.Content(), func(o fyne.CanvasObject) {
		if found != nil {
			return
		}
		if l, ok := o.(*widget.Label); ok && l.Text == text {
			found = l
		}
	})
	return found
}

// waitForWindow polls until a window with the given title substring appears.
func waitForWindow(t *testing.T, app fyne.App, titleSubstr string) fyne.Window {
	t.Helper()
	var found fyne.Window
	require.Eventually(t, func() bool {
		for _, w := range app.Driver().AllWindows() {
			if w.Title() == titleSubstr {
				found = w
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond)
	return found
}

// countChecksByContent counts Check widgets visible in a window's content.
func countChecks(w fyne.Window) int {
	count := 0
	traverse(w.Content(), func(o fyne.CanvasObject) {
		if _, ok := o.(*widget.Check); ok {
			count++
		}
	})
	return count
}
```

**Step 2: Verify it compiles**

Run: `go build ./pkg/streampanel/...`
Expected: success (no test functions yet, just helpers)

**Step 3: Commit**

```bash
git add pkg/streampanel/youtube_settings_e2e_test.go
git commit -m "Add YouTube settings E2E test scaffolding with helpers"
```

---

### Task 2: OpenAccountManagement test

**Files:**
- Modify: `pkg/streampanel/youtube_settings_e2e_test.go`

**Step 1: Write the test**

Add to `youtube_settings_e2e_test.go`:

```go
func TestYouTubeSettingsE2E(t *testing.T) {
	env := setupYouTubeE2E(t)

	t.Run("OpenAccountManagement", func(t *testing.T) {
		env.panel.OpenAccountManagementWindow(env.ctx)

		w := waitForWindow(t, env.app, "Account Management")
		require.NotNil(t, w)

		// Verify YouTube platform header is visible
		ytLabel := findLabelByText(w, "Platform: youtube")
		require.NotNil(t, ytLabel, "YouTube platform header should be visible")

		// Verify existing "yt1" account is shown
		yt1Label := findLabelByText(w, "yt1")
		require.NotNil(t, yt1Label, "Account 'yt1' should be listed")

		w.Close()
	})
}
```

**Step 2: Run test**

Run: `go test ./pkg/streampanel/... -run TestYouTubeSettingsE2E/OpenAccountManagement -v -count=1 -timeout 60s`
Expected: PASS

**Step 3: Commit**

```bash
git add pkg/streampanel/youtube_settings_e2e_test.go
git commit -m "Add OpenAccountManagement E2E test"
```

---

### Task 3: AddYouTubeAccount test

**Files:**
- Modify: `pkg/streampanel/youtube_settings_e2e_test.go`

**Step 1: Write the test**

Add subtest after OpenAccountManagement:

```go
	t.Run("AddYouTubeAccount", func(t *testing.T) {
		env.panel.OpenAccountManagementWindow(env.ctx)
		accMgmt := waitForWindow(t, env.app, "Account Management")
		require.NotNil(t, accMgmt)
		defer accMgmt.Close()

		// Find the "Add account" button in the YouTube section.
		// The account management window lists platforms in order: obs, twitch, kick, youtube.
		// Each platform has an "Add account" button.
		// We need the one associated with YouTube.
		// Strategy: find all "Add account" buttons, tap the last one (YouTube is last).
		var addButtons []*widget.Button
		traverse(accMgmt.Content(), func(o fyne.CanvasObject) {
			if b, ok := o.(*widget.Button); ok && b.Text == "Add account" {
				addButtons = append(addButtons, b)
			}
		})
		require.Len(t, addButtons, 4, "Should have 4 'Add account' buttons (one per platform)")

		// YouTube is the 4th platform (index 3)
		test.Tap(addButtons[3])

		addWin := waitForWindow(t, env.app, "Add youtube account")
		require.NotNil(t, addWin)
		defer addWin.Close()

		// Fill in Account ID
		accIDEntry := findEntryByPlaceholder(addWin, "Account ID (e.g. 'myaccount1')")
		require.NotNil(t, accIDEntry)
		test.Type(accIDEntry, "yt-new")

		// Fill in Client ID
		clientIDEntry := findEntryByPlaceholder(addWin, "client ID")
		require.NotNil(t, clientIDEntry, "Client ID field should exist")
		test.Type(clientIDEntry, "new-client-id")

		// Fill in Client Secret
		clientSecretEntry := findEntryByPlaceholder(addWin, "client secret")
		require.NotNil(t, clientSecretEntry, "Client secret field should exist")
		test.Type(clientSecretEntry, "new-client-secret")

		// Tap "Add account"
		addBtn := findButtonByText(addWin, "Add account")
		require.NotNil(t, addBtn)
		test.Tap(addBtn)

		// Verify: reopen account management and check "yt-new" appears
		// The addWin should have closed, and accMgmt content should have updated
		require.Eventually(t, func() bool {
			return findLabelByText(accMgmt, "yt-new") != nil
		}, 5*time.Second, 50*time.Millisecond, "New account 'yt-new' should appear in account list")
	})
```

**Note:** The placeholder texts for `clientIDField` and `clientSecretField` come from `newClientIDField` and `newClientSecretField` — check the actual placeholders. They may be different. Look at `pkg/streampanel/profile_platform.go` for `newClientIDField`/`newClientSecretField`.

**Step 2: Run test**

Run: `go test ./pkg/streampanel/... -run TestYouTubeSettingsE2E/AddYouTubeAccount -v -count=1 -timeout 60s`
Expected: PASS (may need placeholder text adjustments)

**Step 3: Commit**

```bash
git add pkg/streampanel/youtube_settings_e2e_test.go
git commit -m "Add AddYouTubeAccount E2E test"
```

---

### Task 4: EditYouTubeAccount test

**Files:**
- Modify: `pkg/streampanel/youtube_settings_e2e_test.go`

**Step 1: Write the test**

```go
	t.Run("EditYouTubeAccount", func(t *testing.T) {
		env.panel.OpenAccountManagementWindow(env.ctx)
		accMgmt := waitForWindow(t, env.app, "Account Management")
		require.NotNil(t, accMgmt)
		defer accMgmt.Close()

		// Find the "Edit" button for "yt1"
		// The layout is: Label("yt1"), Spacer, Button("Edit"), Button("Delete")
		// Strategy: find the Edit button that is a sibling of the "yt1" label
		editBtn := findButtonByText(accMgmt, "Edit")
		require.NotNil(t, editBtn, "Edit button should exist for yt1")
		test.Tap(editBtn)

		editWin := waitForWindow(t, env.app, "Edit youtube account: yt1")
		require.NotNil(t, editWin)
		defer editWin.Close()

		// Find and modify Client ID field
		clientIDEntry := findEntryByPlaceholder(editWin, "client ID")
		require.NotNil(t, clientIDEntry)
		clientIDEntry.SetText("updated-client-id")

		// Tap Save
		saveBtn := findButtonByText(editWin, "Save")
		require.NotNil(t, saveBtn)
		test.Tap(saveBtn)

		// editWin should close after save
		require.Eventually(t, func() bool {
			for _, w := range env.app.Driver().AllWindows() {
				if w.Title() == "Edit youtube account: yt1" {
					return false
				}
			}
			return true
		}, 5*time.Second, 50*time.Millisecond, "Edit window should close after save")
	})
```

**Step 2: Run test and fix**

Run: `go test ./pkg/streampanel/... -run TestYouTubeSettingsE2E/EditYouTubeAccount -v -count=1 -timeout 60s`

**Step 3: Commit**

```bash
git add pkg/streampanel/youtube_settings_e2e_test.go
git commit -m "Add EditYouTubeAccount E2E test"
```

---

### Task 5: DeleteYouTubeAccount test

**Files:**
- Modify: `pkg/streampanel/youtube_settings_e2e_test.go`

**Step 1: Write the test**

```go
	t.Run("DeleteYouTubeAccount", func(t *testing.T) {
		env.panel.OpenAccountManagementWindow(env.ctx)
		accMgmt := waitForWindow(t, env.app, "Account Management")
		require.NotNil(t, accMgmt)
		defer accMgmt.Close()

		// Verify yt1 is present
		require.NotNil(t, findLabelByText(accMgmt, "yt1"))

		// Find the Delete button for yt1 (it's adjacent to the Edit button)
		deleteBtn := findButtonByText(accMgmt, "Delete")
		require.NotNil(t, deleteBtn)
		test.Tap(deleteBtn)

		// Verify yt1 is removed from the list
		require.Eventually(t, func() bool {
			return findLabelByText(accMgmt, "yt1") == nil
		}, 5*time.Second, 50*time.Millisecond, "Account 'yt1' should be removed")
	})
```

**Step 2: Run test**

Run: `go test ./pkg/streampanel/... -run TestYouTubeSettingsE2E/DeleteYouTubeAccount -v -count=1 -timeout 60s`

**Step 3: Commit**

```bash
git add pkg/streampanel/youtube_settings_e2e_test.go
git commit -m "Add DeleteYouTubeAccount E2E test"
```

---

### Task 6: StreamList test

**Files:**
- Modify: `pkg/streampanel/youtube_settings_e2e_test.go`

**Step 1: Write the test**

The stream list is part of the Edit Account dialog for YouTube.
The mock pre-seeds "Stream 1" (id: stream-1) and "Stream 2" (id: stream-2).
When the edit dialog opens, `NewStreamManagementUI` is called which loads streams asynchronously.

```go
	t.Run("StreamList", func(t *testing.T) {
		env.panel.OpenAccountManagementWindow(env.ctx)
		accMgmt := waitForWindow(t, env.app, "Account Management")
		require.NotNil(t, accMgmt)
		defer accMgmt.Close()

		// Find Edit button for yt1 and tap it
		editBtn := findButtonByText(accMgmt, "Edit")
		require.NotNil(t, editBtn)
		test.Tap(editBtn)

		editWin := waitForWindow(t, env.app, "Edit youtube account: yt1")
		require.NotNil(t, editWin)
		defer editWin.Close()

		// Wait for streams to load asynchronously
		// The mock returns "Stream 1" and "Stream 2" as Check widgets
		require.Eventually(t, func() bool {
			return findCheckByLabel(editWin, "Stream 1") != nil
		}, 10*time.Second, 100*time.Millisecond, "Stream 1 should appear as a checkbox")

		require.NotNil(t, findCheckByLabel(editWin, "Stream 2"), "Stream 2 should also appear")

		editWin.Close()
	})
```

**Step 2: Run test**

Run: `go test ./pkg/streampanel/... -run TestYouTubeSettingsE2E/StreamList -v -count=1 -timeout 60s`

**Step 3: Commit**

```bash
git add pkg/streampanel/youtube_settings_e2e_test.go
git commit -m "Add StreamList E2E test"
```

---

### Task 7: CreateStream test

**Files:**
- Modify: `pkg/streampanel/youtube_settings_e2e_test.go`

**Step 1: Write the test**

The "Create Stream" button opens a modal popup with a title entry and "Create" button.
Note: modal popups in Fyne tests may appear as overlay on the window canvas, not as separate windows. We may need to traverse the canvas overlays.

```go
	t.Run("CreateStream", func(t *testing.T) {
		env.panel.OpenAccountManagementWindow(env.ctx)
		accMgmt := waitForWindow(t, env.app, "Account Management")
		require.NotNil(t, accMgmt)
		defer accMgmt.Close()

		editBtn := findButtonByText(accMgmt, "Edit")
		require.NotNil(t, editBtn)
		test.Tap(editBtn)

		editWin := waitForWindow(t, env.app, "Edit youtube account: yt1")
		require.NotNil(t, editWin)
		defer editWin.Close()

		// Wait for stream list to load
		require.Eventually(t, func() bool {
			return findCheckByLabel(editWin, "Stream 1") != nil
		}, 10*time.Second, 100*time.Millisecond)

		// Tap "Create Stream"
		createBtn := findButtonByText(editWin, "Create Stream")
		require.NotNil(t, createBtn, "Create Stream button should exist")
		test.Tap(createBtn)

		// The modal popup creates a title entry with placeholder "Stream title"
		// Modal popups appear as overlays on the canvas.
		// We need to find the entry in the overlay content.
		var titleEntry *widget.Entry
		require.Eventually(t, func() bool {
			overlays := editWin.Canvas().Overlays()
			top := overlays.Top()
			if top == nil {
				return false
			}
			traverse(top, func(o fyne.CanvasObject) {
				if e, ok := o.(*widget.Entry); ok && e.PlaceHolder == "Stream title" {
					titleEntry = e
				}
			})
			return titleEntry != nil
		}, 5*time.Second, 50*time.Millisecond, "Modal with title entry should appear")

		test.Type(titleEntry, "Test Stream 3")

		// Find and tap "Create" button in the overlay
		var createConfirmBtn *widget.Button
		overlays := editWin.Canvas().Overlays()
		traverse(overlays.Top(), func(o fyne.CanvasObject) {
			if b, ok := o.(*widget.Button); ok && b.Text == "Create" {
				createConfirmBtn = b
			}
		})
		require.NotNil(t, createConfirmBtn)
		test.Tap(createConfirmBtn)

		// Wait for stream list to refresh and show the new stream
		require.Eventually(t, func() bool {
			return findCheckByLabel(editWin, "Test Stream 3") != nil
		}, 10*time.Second, 100*time.Millisecond, "Newly created 'Test Stream 3' should appear")

		editWin.Close()
	})
```

**Step 2: Run test**

Run: `go test ./pkg/streampanel/... -run TestYouTubeSettingsE2E/CreateStream -v -count=1 -timeout 60s`

**Step 3: Commit**

```bash
git add pkg/streampanel/youtube_settings_e2e_test.go
git commit -m "Add CreateStream E2E test"
```

---

### Task 8: DeleteStream test

**Files:**
- Modify: `pkg/streampanel/youtube_settings_e2e_test.go`

**Step 1: Write the test**

Each stream row has a delete button (icon-only, no text). When tapped, a modal confirmation appears with "Cancel" and "Delete" buttons.

```go
	t.Run("DeleteStream", func(t *testing.T) {
		env.panel.OpenAccountManagementWindow(env.ctx)
		accMgmt := waitForWindow(t, env.app, "Account Management")
		require.NotNil(t, accMgmt)
		defer accMgmt.Close()

		editBtn := findButtonByText(accMgmt, "Edit")
		require.NotNil(t, editBtn)
		test.Tap(editBtn)

		editWin := waitForWindow(t, env.app, "Edit youtube account: yt1")
		require.NotNil(t, editWin)
		defer editWin.Close()

		// Wait for streams to load
		require.Eventually(t, func() bool {
			return findCheckByLabel(editWin, "Stream 2") != nil
		}, 10*time.Second, 100*time.Millisecond)

		// Find the delete button (icon-only with DeleteIcon) associated with "Stream 2"
		// Each stream row is: HBox(Check("Stream N"), Spacer, ButtonWithIcon("", DeleteIcon))
		// Strategy: find all icon-only buttons with empty text in the stream list area
		var deleteButtons []*widget.Button
		traverse(editWin.Content(), func(o fyne.CanvasObject) {
			if b, ok := o.(*widget.Button); ok && b.Text == "" && b.Icon != nil {
				deleteButtons = append(deleteButtons, b)
			}
		})
		require.NotEmpty(t, deleteButtons, "Should find delete buttons for streams")

		// Tap the first delete button (for one of the streams)
		test.Tap(deleteButtons[0])

		// Confirm deletion in the modal
		var confirmDeleteBtn *widget.Button
		require.Eventually(t, func() bool {
			overlays := editWin.Canvas().Overlays()
			top := overlays.Top()
			if top == nil {
				return false
			}
			traverse(top, func(o fyne.CanvasObject) {
				if b, ok := o.(*widget.Button); ok && b.Text == "Delete" {
					confirmDeleteBtn = b
				}
			})
			return confirmDeleteBtn != nil
		}, 5*time.Second, 50*time.Millisecond, "Delete confirmation modal should appear")

		test.Tap(confirmDeleteBtn)

		// Wait for stream list to refresh — should have fewer streams now
		// We verify the deleted stream is gone. Since we don't know which one
		// was deleted (order of map iteration), check that total streams decreased.
		require.Eventually(t, func() bool {
			streams, _ := env.streamD.GetStreams(env.ctx)
			return len(streams) < 2
		}, 10*time.Second, 100*time.Millisecond, "Stream count should decrease after deletion")

		editWin.Close()
	})
```

**Step 2: Run test**

Run: `go test ./pkg/streampanel/... -run TestYouTubeSettingsE2E/DeleteStream -v -count=1 -timeout 60s`

**Step 3: Commit**

```bash
git add pkg/streampanel/youtube_settings_e2e_test.go
git commit -m "Add DeleteStream E2E test"
```

---

### Task 9: ToggleStreamAllowlist test

**Files:**
- Modify: `pkg/streampanel/youtube_settings_e2e_test.go`

**Step 1: Write the test**

```go
	t.Run("ToggleStreamAllowlist", func(t *testing.T) {
		env.panel.OpenAccountManagementWindow(env.ctx)
		accMgmt := waitForWindow(t, env.app, "Account Management")
		require.NotNil(t, accMgmt)
		defer accMgmt.Close()

		editBtn := findButtonByText(accMgmt, "Edit")
		require.NotNil(t, editBtn)
		test.Tap(editBtn)

		editWin := waitForWindow(t, env.app, "Edit youtube account: yt1")
		require.NotNil(t, editWin)
		defer editWin.Close()

		// Wait for streams
		var stream1Check *widget.Check
		require.Eventually(t, func() bool {
			stream1Check = findCheckByLabel(editWin, "Stream 1")
			return stream1Check != nil
		}, 10*time.Second, 100*time.Millisecond)

		// Initially unchecked (no allowlisted streams configured)
		require.False(t, stream1Check.Checked, "Stream 1 should be initially unchecked")

		// Check it
		stream1Check.SetChecked(true)

		// Tap Save to persist allowlist change
		saveBtn := findButtonByText(editWin, "Save")
		require.NotNil(t, saveBtn)
		test.Tap(saveBtn)

		// Now tap "Save and Close" on account management to persist to StreamD
		saveCloseBtn := findButtonByText(accMgmt, "Save and Close")
		require.NotNil(t, saveCloseBtn)
		test.Tap(saveCloseBtn)

		// Verify the config now has AllowlistedStreamIDs for yt1
		sdCfg, err := env.streamD.GetConfig(env.ctx)
		require.NoError(t, err)
		ytPlatCfg := sdCfg.Backends[youtube.ID]
		require.NotNil(t, ytPlatCfg)
		yt1Raw := ytPlatCfg.Accounts["yt1"]
		var yt1Cfg youtube.AccountConfig
		err = yaml.Unmarshal(yt1Raw, &yt1Cfg)
		require.NoError(t, err)
		require.NotEmpty(t, yt1Cfg.AllowlistedStreamIDs, "AllowlistedStreamIDs should contain the checked stream")
	})
```

**Note:** Will need to import `"github.com/goccy/go-yaml"` for unmarshaling.

**Step 2: Run test**

Run: `go test ./pkg/streampanel/... -run TestYouTubeSettingsE2E/ToggleStreamAllowlist -v -count=1 -timeout 60s`

**Step 3: Commit**

```bash
git add pkg/streampanel/youtube_settings_e2e_test.go
git commit -m "Add ToggleStreamAllowlist E2E test"
```

---

### Task 10: SearchStreams test

**Files:**
- Modify: `pkg/streampanel/youtube_settings_e2e_test.go`

**Step 1: Write the test**

```go
	t.Run("SearchStreams", func(t *testing.T) {
		env.panel.OpenAccountManagementWindow(env.ctx)
		accMgmt := waitForWindow(t, env.app, "Account Management")
		require.NotNil(t, accMgmt)
		defer accMgmt.Close()

		editBtn := findButtonByText(accMgmt, "Edit")
		require.NotNil(t, editBtn)
		test.Tap(editBtn)

		editWin := waitForWindow(t, env.app, "Edit youtube account: yt1")
		require.NotNil(t, editWin)
		defer editWin.Close()

		// Wait for streams to load
		require.Eventually(t, func() bool {
			return findCheckByLabel(editWin, "Stream 1") != nil &&
				findCheckByLabel(editWin, "Stream 2") != nil
		}, 10*time.Second, 100*time.Millisecond)

		// Find search entry
		searchEntry := findEntryByPlaceholder(editWin, "Search streams...")
		require.NotNil(t, searchEntry, "Search entry should exist")

		// Type "Stream 1" to filter
		searchEntry.SetText("Stream 1")
		// OnChanged triggers refreshStreams which is async
		require.Eventually(t, func() bool {
			// Stream 1 should be visible
			s1 := findCheckByLabel(editWin, "Stream 1")
			// Stream 2 should be gone
			s2 := findCheckByLabel(editWin, "Stream 2")
			return s1 != nil && s2 == nil
		}, 10*time.Second, 100*time.Millisecond, "Only 'Stream 1' should be visible after filtering")

		// Clear search — both should reappear
		searchEntry.SetText("")
		require.Eventually(t, func() bool {
			return findCheckByLabel(editWin, "Stream 1") != nil &&
				findCheckByLabel(editWin, "Stream 2") != nil
		}, 10*time.Second, 100*time.Millisecond, "Both streams should reappear after clearing search")

		editWin.Close()
	})
```

**Step 2: Run test**

Run: `go test ./pkg/streampanel/... -run TestYouTubeSettingsE2E/SearchStreams -v -count=1 -timeout 60s`

**Step 3: Commit**

```bash
git add pkg/streampanel/youtube_settings_e2e_test.go
git commit -m "Add SearchStreams E2E test"
```

---

### Task 11: SaveAndClose test

**Files:**
- Modify: `pkg/streampanel/youtube_settings_e2e_test.go`

**Step 1: Write the test**

```go
	t.Run("SaveAndClose", func(t *testing.T) {
		env.panel.OpenAccountManagementWindow(env.ctx)
		accMgmt := waitForWindow(t, env.app, "Account Management")
		require.NotNil(t, accMgmt)

		// Tap "Save and Close"
		saveCloseBtn := findButtonByText(accMgmt, "Save and Close")
		require.NotNil(t, saveCloseBtn)
		test.Tap(saveCloseBtn)

		// Window should close
		require.Eventually(t, func() bool {
			for _, w := range env.app.Driver().AllWindows() {
				if w.Title() == "Account Management" {
					return false
				}
			}
			return true
		}, 5*time.Second, 50*time.Millisecond, "Account Management window should close after save")

		// Verify config is accessible in StreamD (no error)
		cfg, err := env.streamD.GetConfig(env.ctx)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		require.NotNil(t, cfg.Backends[youtube.ID], "YouTube backend should still exist in config")
	})
```

**Step 2: Run test**

Run: `go test ./pkg/streampanel/... -run TestYouTubeSettingsE2E/SaveAndClose -v -count=1 -timeout 60s`

**Step 3: Commit**

```bash
git add pkg/streampanel/youtube_settings_e2e_test.go
git commit -m "Add SaveAndClose E2E test"
```

---

### Task 12: Run full test suite and fix issues

**Step 1: Run all E2E tests together**

Run: `go test ./pkg/streampanel/... -run TestYouTubeSettingsE2E -v -count=1 -timeout 180s`

**Important:** Since subtests share `env`, they run sequentially. State mutations in earlier tests (like DeleteAccount deleting "yt1") will affect later tests. If this is an issue, either:
- Reorder tests so destructive ones run last
- Make each subtest create its own env (slower but isolated)
- Re-add deleted accounts in cleanup

The recommended order in the plan already puts Delete tests before stream-specific tests, but this needs attention. The implementer should consider test isolation.

**Step 2: Run full package tests**

Run: `go test ./pkg/streampanel/... -count=1 -timeout 300s`
Expected: all tests pass

**Step 3: Run go vet**

Run: `go vet ./pkg/streampanel/...`
Expected: no issues

**Step 4: Final commit**

```bash
git add pkg/streampanel/youtube_settings_e2e_test.go
git commit -m "Fix test ordering and finalize YouTube settings E2E tests"
```

---

### Implementation Notes

1. **Placeholder texts**: The exact placeholder strings for Client ID / Client Secret fields need verification. Check `newClientIDField` and `newClientSecretField` in `pkg/streampanel/profile_platform.go`. Adjust test expectations accordingly.

2. **Modal popups**: Fyne modal popups appear as canvas overlays, not separate windows. Use `window.Canvas().Overlays().Top()` and `traverse()` to find widgets inside modals.

3. **Async stream loading**: `NewStreamManagementUI` loads streams in a goroutine via `observability.Go`. The UI updates via `DoFromGoroutine`. Tests must use `require.Eventually` to wait for these async updates.

4. **Streams cache**: `StreamD.GetStreams` uses memoization cache. After `CreateStream`/`DeleteStream`, the cache is invalidated, but the UI refresh is still async.

5. **Test isolation**: The subtests share a single `ytE2EEnv`. If a subtest mutates state (e.g., deletes an account), subsequent subtests see that change. Handle by either re-creating state or ordering tests carefully.

6. **traverse() limitations**: The existing `traverse()` helper only descends into known container types. If widgets are nested in types not covered by `traverse()`, they won't be found. May need to extend `traverse()` to handle `widget.ModalPopUp` or other container types.
