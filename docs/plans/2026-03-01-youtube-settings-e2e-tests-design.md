# YouTube Settings E2E Tests Design

## Goal

E2E tests for managing YouTube streams in Settings, covering account CRUD, stream CRUD, allowlisting, search/filter, and save flow.

## Architecture

- Real `streamd.StreamD` instance (not dummyStreamD)
- Mock platform clients: `youtube.SetDebugUseMockClient(true)` etc.
- YouTube mock pre-seeds "Stream 1" and "Stream 2" with IDs "stream-1", "stream-2"
- Fyne `test.NewApp()` panel with `test.Tap()` for UI interactions
- `traverse()` helper to find buttons/widgets in window content trees
- `require.Eventually` for async UI updates (stream list loading, create/delete)

## Test File

`pkg/streampanel/youtube_settings_e2e_test.go`

## Shared Setup

Reuses integration test pattern:
1. Enable all mock clients
2. Create config with one YouTube account ("yt1") with valid mock token
3. Initialize real StreamD with mock UI
4. Run StreamD, wait for initialization
5. Create Panel with `test.NewApp()`, wire to StreamD
6. Helper functions: `findButtonByText(window, text)`, `findEntryByPlaceholder(window, text)`, `findCheckByLabel(window, label)`

## Test Cases

### `TestYouTubeSettingsE2E` subtests:

1. **OpenAccountManagement** - Open account management window, verify YouTube platform header and existing "yt1" account visible
2. **AddYouTubeAccount** - Tap "Add account" for YouTube, fill AccountID + ClientID + ClientSecret, tap "Add account" button, verify new account row appears
3. **EditYouTubeAccount** - Tap "Edit" on "yt1", modify ClientID field, tap "Save", verify config updated
4. **DeleteYouTubeAccount** - Add a temp account, tap "Delete" on it, verify it disappears
5. **StreamList** - Open Edit for "yt1", verify "Stream 1" and "Stream 2" appear in stream management UI
6. **CreateStream** - Tap "Create Stream", enter title "Test Stream 3", tap "Create", verify it appears in list
7. **DeleteStream** - Tap delete on a stream, confirm in modal popup, verify stream disappears from list
8. **ToggleStreamAllowlist** - Check stream checkbox, save, verify AllowlistedStreamIDs updated in config
9. **SearchStreams** - Type "Stream 1" in search, verify only "Stream 1" shown, clear search, verify both shown
10. **SaveAndClose** - Make changes, tap "Save and Close", verify config persisted in StreamD

## Key Helpers Needed

- `findButtonByText(w fyne.Window, text string) *widget.Button` - traverse window content to find button
- `findEntryByPlaceholder(w fyne.Window, placeholder string) *widget.Entry` - find entry by placeholder text
- `findCheckByLabel(w fyne.Window, label string) *widget.Check` - find checkbox by label
- `getAccountManagementWindow(app fyne.App) fyne.Window` - find the account management window among all app windows
- `waitForWindow(t, app, title) fyne.Window` - wait for window with specific title to appear
