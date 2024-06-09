package streampanel

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/go-ng/xmath"
	"github.com/xaionaro-go/gorex"
	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
)

type Profile struct {
	ProfileMetadata
	Name        streamcontrol.ProfileName
	PerPlatform map[streamcontrol.PlatformName]streamcontrol.AbstractStreamProfile
}

type Panel struct {
	dataLock sync.Mutex
	data     panelData

	app                fyne.App
	config             configT
	startStopMutex     gorex.Mutex
	updateTimerHandler *updateTimerHandler
	streamControllers  struct {
		Twitch  *twitch.Twitch
		YouTube *youtube.YouTube
	}
	profilesOrder         []streamcontrol.ProfileName
	profilesOrderFiltered []streamcontrol.ProfileName
	selectedProfileName   *streamcontrol.ProfileName
	defaultContext        context.Context

	mainWindow             fyne.Window
	startStopButton        *widget.Button
	profilesListWidget     *widget.List
	streamTitleField       *widget.Entry
	streamDescriptionField *widget.Entry

	dataPath    string
	filterValue string

	youtubeCheck *widget.Check
	twitchCheck  *widget.Check

	gitStorage *gitStorage

	cancelGitSyncer context.CancelFunc
	gitSyncerMutex  sync.Mutex
}

func New(
	dataPath string,
	opts ...Option,
) *Panel {
	return &Panel{
		dataPath: dataPath,
		config:   Options(opts).Config(),
	}
}

func (p *Panel) Loop(ctx context.Context) error {
	if p.defaultContext != nil {
		return fmt.Errorf("Loop was already used, and cannot be used the second time")
	}
	p.startStopMutex = gorex.Mutex{
		InfiniteContext: ctx,
	}

	p.defaultContext = ctx
	logger.Debug(ctx, "config", p.config)

	if err := p.loadData(ctx); err != nil {
		return fmt.Errorf("unable to load the data '%s': %w", p.dataPath, err)
	}

	p.app = fyneapp.New()

	go func() {
		p.initGitIfNeeded(ctx)
		go func() {
			if p.gitStorage != nil {
				err := p.sendConfigViaGIT(ctx)
				if err != nil {
					p.displayError(fmt.Errorf("unable to send the config to the remote git repository: %w", err))
				}
			} else {
				logger.Debugf(ctx, "git storage is not initialized")
			}
		}()

		if err := p.initStreamControllers(ctx); err != nil {
			err = fmt.Errorf("unable to initialize stream controllers: %w", err)
			p.displayError(err)
			return
		}

		var wg sync.WaitGroup

		wg.Add(1)
		go func() {
			defer wg.Done()
			p.initTwitchData(ctx)
			p.normalizeTwitchData()
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			p.initYoutubeData(ctx)
			p.normalizeYoutubeData()
		}()

		wg.Wait()

		p.initMainWindow(ctx)
		p.rearrangeProfiles(ctx)
	}()

	p.app.Run()
	return nil
}

func (p *Panel) initTwitchData(ctx context.Context) {
	logger.FromCtx(ctx).Debugf("initializing Twitch data")
	defer logger.FromCtx(ctx).Debugf("endof initializing Twitch data")

	if c := len(p.data.Cache.Twitch.Categories); c != 0 {
		logger.FromCtx(ctx).Debugf("already have categories (count: %d)", c)
		return
	}

	twitch := p.streamControllers.Twitch
	if twitch == nil {
		logger.FromCtx(ctx).Debugf("twitch controller is not initialized")
		return
	}

	allCategories, err := twitch.GetAllCategories(ctx)
	if err != nil {
		p.displayError(err)
		return
	}

	logger.FromCtx(ctx).Debugf("got categories: %#+v", allCategories)

	func() {
		p.dataLock.Lock()
		defer p.dataLock.Unlock()
		p.data.Cache.Twitch.Categories = allCategories
	}()

	err = p.saveData(ctx)
	errmon.ObserveErrorCtx(ctx, err)
}

func (p *Panel) normalizeTwitchData() {
	s := p.data.Cache.Twitch.Categories
	sort.Slice(s, func(i, j int) bool {
		return s[i].Name < s[j].Name
	})
}

func (p *Panel) initYoutubeData(ctx context.Context) {
	logger.FromCtx(ctx).Debugf("initializing Youtube data")
	defer logger.FromCtx(ctx).Debugf("endof initializing Youtube data")

	if c := len(p.data.Cache.Youtube.Broadcasts); c != 0 {
		logger.FromCtx(ctx).Debugf("already have broadcasts (count: %d)", c)
		return
	}

	youtube := p.streamControllers.YouTube
	if youtube == nil {
		logger.FromCtx(ctx).Debugf("youtube controller is not initialized")
		return
	}

	broadcasts, err := youtube.ListBroadcasts(ctx)
	if err != nil {
		p.displayError(err)
		return
	}

	logger.FromCtx(ctx).Debugf("got broadcasts: %#+v", broadcasts)

	func() {
		p.dataLock.Lock()
		defer p.dataLock.Unlock()
		p.data.Cache.Youtube.Broadcasts = broadcasts
	}()

	err = p.saveData(ctx)
	errmon.ObserveErrorCtx(ctx, err)
}

func (p *Panel) normalizeYoutubeData() {
	s := p.data.Cache.Youtube.Broadcasts
	sort.Slice(s, func(i, j int) bool {
		return s[i].Snippet.Title < s[j].Snippet.Title
	})
}

func expandPath(rawPath string) (string, error) {
	switch {
	case strings.HasPrefix(rawPath, "~/"):
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("unable to get user home dir: %w", err)
		}
		return path.Join(homeDir, rawPath[2:]), nil
	}
	return rawPath, nil
}

func (p *Panel) getExpandedDataPath() (string, error) {
	return expandPath(p.dataPath)
}

func (p *Panel) loadData(ctx context.Context) error {
	dataPath, err := p.getExpandedDataPath()
	if err != nil {
		return fmt.Errorf("unable to get the path to the data file: %w", err)
	}

	_, err = os.Stat(dataPath)
	switch {
	case err == nil:
		return readPanelDataFromPath(ctx, dataPath, &p.data)
	case os.IsNotExist(err):
		logger.Debugf(ctx, "cannot find file '%s', creating", dataPath)
		p.data = newPanelData()
		if err := p.saveData(ctx); err != nil {
			logger.Errorf(ctx, "cannot create file '%s': %v", dataPath, err)
		}
		return nil
	default:
		return fmt.Errorf("unable to access file '%s': %w", dataPath, err)
	}
}

func (p *Panel) savePlatformConfig(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	platCfg *streamcontrol.AbstractPlatformConfig,
) error {
	logger.Debugf(ctx, "savePlatformConfig('%s', '%#+v')", platID, platCfg)
	defer logger.Debugf(ctx, "endof savePlatformConfig('%s', '%#+v')", platID, platCfg)
	p.dataLock.Lock()
	defer p.dataLock.Unlock()
	p.data.Backends[platID] = platCfg
	return p.saveData(ctx)
}

func (p *Panel) oauthHandlerTwitch(ctx context.Context, arg oauthhandler.OAuthHandlerArgument) error {
	logger.Infof(ctx, "oauthHandlerTwitch: %#+v", arg)
	defer logger.Infof(ctx, "/oauthHandlerTwitch")
	return p.oauthHandler(ctx, arg)
}

func (p *Panel) oauthHandlerYouTube(ctx context.Context, arg oauthhandler.OAuthHandlerArgument) error {
	logger.Infof(ctx, "oauthHandlerYouTube: %#+v", arg)
	defer logger.Infof(ctx, "/oauthHandlerYouTube")
	return p.oauthHandler(ctx, arg)
}

func (p *Panel) oauthHandler(ctx context.Context, arg oauthhandler.OAuthHandlerArgument) error {
	codeCh, err := oauthhandler.NewCodeReceiver(arg.RedirectURL)
	if err != nil {
		return err
	}

	if err := p.openBrowser(arg.AuthURL); err != nil {
		return fmt.Errorf("unable to open browser with URL '%s': %w", arg.AuthURL, err)
	}

	logger.Infof(ctx, "Your browser has been launched (URL: %s).\nPlease approve the permissions.\n", arg.AuthURL)

	// Wait for the web server to get the code.
	code := <-codeCh
	logger.Debugf(ctx, "received the auth code")
	return arg.ExchangeFn(code)
}

func (p *Panel) openBrowser(authURL string) error {
	var browserCmd string
	switch runtime.GOOS {
	case "darwin":
		browserCmd = "open"
	case "linux":
		if envBrowser := os.Getenv("BROWSER"); envBrowser != "" {
			browserCmd = envBrowser
		} else {
			browserCmd = "xdg-open"
		}
	default:
		return oauthhandler.LaunchBrowser(authURL)
	}

	waitCh := make(chan struct{})

	w := p.app.NewWindow("Browser selection window")
	promptText := widget.NewRichTextWithText("It is required to confirm access in Twitch/YouTube using browser. Select a browser for that (or leave the field empty for auto-selection):")
	promptText.Wrapping = fyne.TextWrapWord
	browserField := widget.NewEntry()
	browserField.PlaceHolder = "command to execute the browser"
	browserField.OnSubmitted = func(s string) {
		close(waitCh)
	}
	okButton := widget.NewButton("OK", func() {
		close(waitCh)
	})
	w.SetContent(container.NewBorder(
		container.NewVBox(
			promptText,
			browserField,
		),
		okButton,
		nil,
		nil,
		nil,
	))

	w.Show()
	<-waitCh
	w.Hide()

	if browserField.Text != "" {
		browserCmd = browserField.Text
	}

	return exec.Command(browserCmd, authURL).Start()
}

var twitchAppsCreateLink, _ = url.Parse("https://dev.twitch.tv/console/apps/create")

func (p *Panel) inputTwitchUserInfo(
	ctx context.Context,
	cfg *streamcontrol.PlatformConfig[twitch.PlatformSpecificConfig, twitch.StreamProfile],
) (bool, error) {
	w := p.app.NewWindow("Input Twitch user info")
	w.Resize(fyne.NewSize(600, 200))

	channelField := widget.NewEntry()
	channelField.SetPlaceHolder("channel ID (copy&paste it from the browser: https://www.twitch.tv/<the channel ID is here>)")
	clientIDField := widget.NewEntry()
	clientIDField.SetPlaceHolder("client ID")
	clientSecretField := widget.NewEntry()
	clientSecretField.SetPlaceHolder("client secret")
	instructionText := widget.NewRichText(
		&widget.TextSegment{Text: "Go to\n", Style: widget.RichTextStyle{Inline: true}},
		&widget.HyperlinkSegment{Text: twitchAppsCreateLink.String(), URL: twitchAppsCreateLink},
		&widget.TextSegment{Text: `,` + "\n" + `create an application (enter "http://localhost:8091/" as the "OAuth Redirect URLs" value), then click "Manage" then "New Secret", and copy&paste client ID and client secret.`, Style: widget.RichTextStyle{Inline: true}},
	)
	instructionText.Wrapping = fyne.TextWrapWord

	waitCh := make(chan struct{})
	skip := false
	skipButton := widget.NewButtonWithIcon("Skip", theme.ConfirmIcon(), func() {
		skip = true
		close(waitCh)
	})
	okButton := widget.NewButtonWithIcon("OK", theme.ConfirmIcon(), func() {
		close(waitCh)
	})

	w.SetContent(container.NewBorder(
		widget.NewRichTextWithText("Enter Twitch user info:"),
		container.NewHBox(skipButton, okButton),
		nil,
		nil,
		container.NewVBox(
			channelField,
			clientIDField,
			clientSecretField,
			instructionText,
		),
	))
	w.Show()
	<-waitCh
	w.Hide()

	if skip {
		cfg.Enable = ptr(false)
		return false, nil
	}
	cfg.Config.AuthType = "user"
	channelWords := strings.Split(channelField.Text, "/")
	cfg.Config.Channel = channelWords[len(channelWords)-1]
	cfg.Config.ClientID = clientIDField.Text
	cfg.Config.ClientSecret = clientSecretField.Text

	return true, nil
}

var youtubeCredentialsCreateLink, _ = url.Parse("https://console.cloud.google.com/apis/credentials/oauthclient")

func (p *Panel) inputYouTubeUserInfo(
	ctx context.Context,
	cfg *streamcontrol.PlatformConfig[youtube.PlatformSpecificConfig, youtube.StreamProfile],
) (bool, error) {
	w := p.app.NewWindow("Input YouTube user info")
	w.Resize(fyne.NewSize(600, 200))

	clientIDField := widget.NewEntry()
	clientIDField.SetPlaceHolder("client ID")
	clientSecretField := widget.NewEntry()
	clientSecretField.SetPlaceHolder("client secret")
	instructionText := widget.NewRichText(
		&widget.TextSegment{Text: "Go to\n", Style: widget.RichTextStyle{Inline: true}},
		&widget.HyperlinkSegment{Text: youtubeCredentialsCreateLink.String(), URL: youtubeCredentialsCreateLink},
		&widget.TextSegment{Text: `,` + "\n" + `configure "consent screen" (note: you may add yourself into Test Users to avoid problems further on, and don't forget to add "YouTube Data API v3" scopes) and go back to` + "\n", Style: widget.RichTextStyle{Inline: true}},
		&widget.HyperlinkSegment{Text: youtubeCredentialsCreateLink.String(), URL: youtubeCredentialsCreateLink},
		&widget.TextSegment{Text: `,` + "\n" + `choose "Desktop app", confirm and copy&paste client ID and client secret.`, Style: widget.RichTextStyle{Inline: true}},
	)
	instructionText.Wrapping = fyne.TextWrapWord

	waitCh := make(chan struct{})
	skip := false
	skipButton := widget.NewButtonWithIcon("Skip", theme.ConfirmIcon(), func() {
		skip = true
		close(waitCh)
	})
	okButton := widget.NewButtonWithIcon("OK", theme.ConfirmIcon(), func() {
		close(waitCh)
	})

	w.SetContent(container.NewBorder(
		widget.NewRichTextWithText("Enter YouTube user info:"),
		container.NewHBox(skipButton, okButton),
		nil,
		nil,
		container.NewVBox(
			clientIDField,
			clientSecretField,
			instructionText,
		),
	))
	w.Show()
	<-waitCh
	w.Hide()

	if skip {
		cfg.Enable = ptr(false)
		return false, nil
	}
	cfg.Config.ClientID = clientIDField.Text
	cfg.Config.ClientSecret = clientSecretField.Text

	return true, nil
}

func (p *Panel) initStreamControllers(ctx context.Context) error {
	platNames := make([]streamcontrol.PlatformName, 0, len(p.data.Backends))
	for platName := range p.data.Backends {
		platNames = append(platNames, platName)
	}
	sort.Slice(platNames, func(i, j int) bool {
		return platNames[i] < platNames[j]
	})
	for _, platName := range platNames {
		var err error
		switch strings.ToLower(string(platName)) {
		case strings.ToLower(string(twitch.ID)):
			err = p.initTwitchBackend(ctx)
		case strings.ToLower(string(youtube.ID)):
			err = p.initYouTubeBackend(ctx)
		}
		if err != nil && err != ErrSkipBackend {
			return fmt.Errorf("unable to initialize '%s': %w", platName, err)
		}
	}
	return nil
}

func (p *Panel) profileCreateOrUpdate(ctx context.Context, profile Profile) error {
	logger.Tracef(ctx, "profileCreateOrUpdate(%s)", profile.Name)
	for platformName, platformProfile := range profile.PerPlatform {
		p.data.Backends[platformName].StreamProfiles[profile.Name] = platformProfile
		logger.Tracef(ctx, "profileCreateOrUpdate(%s): p.data.Backends[%s].StreamProfiles[%s] = %#+v", profile.Name, platformName, profile.Name, platformProfile)
	}
	p.data.ProfileMetadata[profile.Name] = profile.ProfileMetadata

	logger.Tracef(ctx, "profileCreateOrUpdate(%s): p.data.Backends == %#+v", profile.Name, p.data.Backends)
	p.rearrangeProfiles(ctx)
	if err := p.saveData(ctx); err != nil {
		return fmt.Errorf("unable to save the profile: %w", err)
	}
	return nil
}

func (p *Panel) profileDelete(ctx context.Context, profileName streamcontrol.ProfileName) error {
	logger.Tracef(p.defaultContext, "onProfileDeleted(%s)", profileName)
	for platformName := range p.data.Backends {
		delete(p.data.Backends[platformName].StreamProfiles, profileName)
	}
	delete(p.data.ProfileMetadata, profileName)

	p.rearrangeProfiles(ctx)
	if err := p.saveData(ctx); err != nil {
		return fmt.Errorf("unable to save the profile: %w", err)
	}

	return nil
}

func (p *Panel) saveDataToConfigFile(ctx context.Context) error {
	dataPath, err := p.getExpandedDataPath()
	if err != nil {
		return fmt.Errorf("unable to get the path to the data file: %w", err)
	}

	err = writePanelDataToPath(ctx, dataPath, p.data)
	if err != nil {
		return fmt.Errorf("unable to save the config: %w", err)
	}

	return nil
}

func (p *Panel) saveData(ctx context.Context) error {
	err := p.saveDataToConfigFile(ctx)
	if err != nil {
		return err
	}

	if p.gitStorage != nil {
		err = p.sendConfigViaGIT(ctx)
		if err != nil {
			return fmt.Errorf("unable to send the config to the remote git repository: %w", err)
		}
	}

	return nil
}

func (p *Panel) getProfile(profileName streamcontrol.ProfileName) Profile {
	prof := Profile{
		ProfileMetadata: p.data.ProfileMetadata[profileName],
		Name:            profileName,
		PerPlatform:     map[streamcontrol.PlatformName]streamcontrol.AbstractStreamProfile{},
	}
	for platName, platCfg := range p.data.Backends {
		platProf, ok := platCfg.GetStreamProfile(profileName)
		if !ok {
			continue
		}
		prof.PerPlatform[platName] = platProf
	}
	return prof
}

func (p *Panel) rearrangeProfiles(ctx context.Context) {
	curProfilesMap := map[streamcontrol.ProfileName]*Profile{}
	for platName, platCfg := range p.data.Backends {
		for profName, platProf := range platCfg.StreamProfiles {
			prof := curProfilesMap[profName]
			if prof == nil {
				prof = &Profile{
					Name:        profName,
					PerPlatform: map[streamcontrol.PlatformName]streamcontrol.AbstractStreamProfile{},
				}
				curProfilesMap[profName] = prof
			}
			prof.PerPlatform[platName] = platProf
			prof.MaxOrder = xmath.Max(prof.MaxOrder, platProf.GetOrder())
		}
	}

	curProfiles := make([]Profile, 0, len(curProfilesMap))
	for idx, profile := range curProfilesMap {
		curProfiles = append(curProfiles, *profile)
		logger.Tracef(ctx, "rearrangeProfiles(): curProfiles[%3d] = %#+v", idx, *profile)
	}

	sort.Slice(curProfiles, func(i, j int) bool {
		aa := curProfiles[i]
		ab := curProfiles[j]
		return aa.MaxOrder < ab.MaxOrder
	})

	if cap(p.profilesOrder) < len(curProfiles) {
		p.profilesOrder = make([]streamcontrol.ProfileName, 0, len(curProfiles)*2)
	} else {
		p.profilesOrder = p.profilesOrder[:0]
	}
	for idx, profile := range curProfiles {
		p.profilesOrder = append(p.profilesOrder, profile.Name)
		logger.Tracef(ctx, "rearrangeProfiles(): profilesOrder[%3d] = %#+v", idx, profile)
	}

	p.refilterProfiles(ctx)
}

func (p *Panel) refilterProfiles(ctx context.Context) {
	if cap(p.profilesOrderFiltered) < len(p.profilesOrder) {
		p.profilesOrderFiltered = make([]streamcontrol.ProfileName, 0, len(p.profilesOrder)*2)
	} else {
		p.profilesOrderFiltered = p.profilesOrderFiltered[:0]
	}
	if p.filterValue == "" {
		p.profilesOrderFiltered = p.profilesOrderFiltered[:len(p.profilesOrder)]
		copy(p.profilesOrderFiltered, p.profilesOrder)
		logger.Tracef(ctx, "refilterProfiles(): profilesOrderFiltered <- p.profilesOrder: %#+v", p.profilesOrder)
		logger.Tracef(ctx, "refilterProfiles(): p.profilesListWidget.Refresh()")
		p.profilesListWidget.Refresh()
		return
	}

	filterValue := strings.ToLower(p.filterValue)
	for _, profileName := range p.profilesOrder {
		titleMatch := strings.Contains(strings.ToLower(string(profileName)), filterValue)
		subValueMatch := false
		for _, platCfg := range p.data.Backends {
			prof, ok := platCfg.GetStreamProfile(profileName)
			if !ok {
				continue
			}

			switch prof := prof.(type) {
			case twitch.StreamProfile:
				if containTagSubstringCI(prof.Tags, filterValue) {
					subValueMatch = true
					break
				}
				if ptrStringMatchCI(prof.Language, filterValue) {
					subValueMatch = true
					break
				}
			case youtube.StreamProfile:
				if containTagSubstringCI(prof.Tags, filterValue) {
					subValueMatch = true
					break
				}
			}
		}

		if titleMatch || subValueMatch {
			logger.Tracef(ctx, "refilterProfiles(): profilesOrderFiltered[%3d] = %s", len(p.profilesOrderFiltered), profileName)
			p.profilesOrderFiltered = append(p.profilesOrderFiltered, profileName)
		}
	}

	logger.Tracef(ctx, "refilterProfiles(): p.profilesListWidget.Refresh()")
	p.profilesListWidget.Refresh()
}

func containTagSubstringCI(tags []string, s string) bool {
	for _, tag := range tags {
		if strings.Contains(strings.ToLower(tag), s) {
			return true
		}
	}
	return false
}

func ptrStringMatchCI(ptrString *string, s string) bool {
	if ptrString == nil {
		return false
	}

	return strings.Contains(strings.ToLower(*ptrString), s)
}

func (p *Panel) profilesListLength() int {
	return len(p.profilesOrderFiltered)
}

func (p *Panel) profilesListItemCreate() fyne.CanvasObject {
	return widget.NewLabel("")
}

func (p *Panel) profilesListItemUpdate(
	itemID widget.ListItemID,
	obj fyne.CanvasObject,
) {
	w := obj.(*widget.Label)

	profileName := streamcontrol.ProfileName(p.profilesOrderFiltered[itemID])
	profile := p.getProfile(profileName)

	w.SetText(string(profile.Name))
}

func ptrCopy[T any](v T) *T {
	return &v
}

func (p *Panel) onProfilesListSelect(
	id widget.ListItemID,
) {
	p.startStopButton.Enable()

	shouldRestart := p.updateTimerHandler != nil
	if shouldRestart {
		p.startStopButton.OnTapped()
	}
	profileName := p.profilesOrder[id]
	profile := p.getProfile(profileName)
	p.selectedProfileName = ptrCopy(profileName)
	p.streamTitleField.SetText(profile.DefaultStreamTitle)
	p.streamDescriptionField.SetText(profile.DefaultStreamDescription)
	if shouldRestart {
		p.startStopButton.OnTapped()
	}
}

func (p *Panel) onProfilesListUnselect(
	_ widget.ListItemID,
) {
	if p.updateTimerHandler != nil {
		p.startStopButton.OnTapped()
		p.streamTitleField.SetText("")
	}
	p.startStopButton.Disable()
}

func (p *Panel) setFilter(ctx context.Context, filter string) {
	p.filterValue = filter
	p.refilterProfiles(ctx)
}

func (p *Panel) openSettingsWindow(ctx context.Context) {
	w := p.app.NewWindow("Settings")
	w.Resize(fyne.NewSize(400, 900))

	startStreamCommandEntry := widget.NewEntry()
	startStreamCommandEntry.SetText(p.data.Commands.OnStartStream)
	stopStreamCommandEntry := widget.NewEntry()
	stopStreamCommandEntry.SetText(p.data.Commands.OnStopStream)

	cancelButton := widget.NewButtonWithIcon("Cancel", theme.CancelIcon(), func() {
		w.Close()
	})
	saveButton := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() {
		p.data.Commands.OnStartStream = startStreamCommandEntry.Text
		p.data.Commands.OnStopStream = stopStreamCommandEntry.Text

		if err := p.saveData(ctx); err != nil {
			p.displayError(err)
		}

		w.Close()
	})

	templateInstruction := widget.NewRichTextFromMarkdown("Commands support [Go templates](https://pkg.go.dev/text/template) with two custom functions predefined:\n* `devnull` nullifies any inputs\n* `httpGET` makes an HTTP GET request and inserts the response body")
	templateInstruction.Wrapping = fyne.TextWrapWord

	twitchAlreadyLoggedIn := widget.NewLabel("")
	if p.streamControllers.Twitch == nil {
		twitchAlreadyLoggedIn.SetText("(not logged in)")
	} else {
		twitchAlreadyLoggedIn.SetText("(already logged in)")
	}

	youtubeAlreadyLoggedIn := widget.NewLabel("")
	if p.streamControllers.YouTube == nil {
		youtubeAlreadyLoggedIn.SetText("(not logged in)")
	} else {
		youtubeAlreadyLoggedIn.SetText("(already logged in)")
	}

	gitAlreadyLoggedIn := widget.NewLabel("")
	if p.gitStorage == nil {
		gitAlreadyLoggedIn.SetText("(not logged in)")
	} else {
		gitAlreadyLoggedIn.SetText("(already logged in)")
	}

	w.SetContent(container.NewBorder(
		container.NewVBox(
			widget.NewSeparator(),
			widget.NewSeparator(),
			widget.NewRichTextFromMarkdown(`# Streaming platforms`),
			container.NewHBox(
				widget.NewButtonWithIcon("(Re-)login in Twitch", theme.LoginIcon(), func() {
					oldCfg := p.data.Backends[twitch.ID].Config
					p.data.Backends[twitch.ID].Config = twitch.PlatformSpecificConfig{}
					err := p.initTwitchBackend(ctx)
					if err != nil {
						p.displayError(err)
						p.data.Backends[twitch.ID].Config = oldCfg
						return
					}
				}),
				twitchAlreadyLoggedIn,
			),
			container.NewHBox(
				widget.NewButtonWithIcon("(Re-)login in YouTube", theme.LoginIcon(), func() {
					oldCfg := p.data.Backends[youtube.ID].Config
					p.data.Backends[youtube.ID].Config = youtube.PlatformSpecificConfig{}
					err := p.initYouTubeBackend(ctx)
					if err != nil {
						p.displayError(err)
						p.data.Backends[youtube.ID].Config = oldCfg
						return
					}
				}),
				youtubeAlreadyLoggedIn,
			),
			widget.NewSeparator(),
			widget.NewSeparator(),
			widget.NewRichTextFromMarkdown(`# Commands`),
			templateInstruction,
			widget.NewSeparator(),
			widget.NewLabel("Run command on stream start:"),
			startStreamCommandEntry,
			widget.NewSeparator(),
			widget.NewLabel("Run command on stream stop:"),
			stopStreamCommandEntry,
			widget.NewSeparator(),
			widget.NewSeparator(),
			widget.NewRichTextFromMarkdown(`# Syncing (via git)`),
			container.NewHBox(
				widget.NewButtonWithIcon("(Re-)login in GIT", theme.LoginIcon(), func() {
					alreadyLoggedIn := p.gitStorage != nil
					oldCfg := p.data.GitRepo
					p.data.GitRepo = gitRepoConfig{}
					p.initGitIfNeeded(ctx)
					if p.gitStorage == nil {
						p.data.GitRepo = oldCfg
						if alreadyLoggedIn {
							p.initGitIfNeeded(ctx)
						}
					}
				}),
				twitchAlreadyLoggedIn,
			),
		),
		container.NewHBox(
			cancelButton,
			saveButton,
		),
		nil,
		nil,
	))

	w.Show()
}

func (p *Panel) initTwitchBackend(ctx context.Context) error {
	twitch, err := newTwitch(
		ctx,
		p.data.Backends[twitch.ID],
		p.inputTwitchUserInfo,
		func(cfg *streamcontrol.AbstractPlatformConfig) error {
			return p.savePlatformConfig(ctx, twitch.ID, cfg)
		},
		p.oauthHandlerTwitch)
	if err != nil {
		return err
	}
	p.streamControllers.Twitch = twitch
	return nil
}

func (p *Panel) initYouTubeBackend(ctx context.Context) error {
	youTube, err := newYouTube(
		ctx,
		p.data.Backends[youtube.ID],
		p.inputYouTubeUserInfo,
		func(cfg *streamcontrol.AbstractPlatformConfig) error {
			return p.savePlatformConfig(ctx, youtube.ID, cfg)
		},
		p.oauthHandlerYouTube,
	)
	if err != nil {
		return err
	}
	p.streamControllers.YouTube = youTube
	return nil
}

func (p *Panel) resetCache(ctx context.Context) {
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		p.data.Cache.Twitch = twitchCache{}
		p.initTwitchData(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		p.data.Cache.Youtube = youTubeCache{}
		p.initYoutubeData(ctx)
	}()

	wg.Wait()
}

func (p *Panel) openMenuWindow(ctx context.Context) {
	popupMenu := widget.NewPopUpMenu(fyne.NewMenu("menu",
		fyne.NewMenuItem("Settings", func() {
			p.openSettingsWindow(ctx)
		}),
		fyne.NewMenuItem("Reset cache", func() {
			p.resetCache(ctx)
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Quit", func() {
			p.app.Quit()
		}),
	), p.mainWindow.Canvas())
	popupMenu.Show()
}

func (p *Panel) initMainWindow(ctx context.Context) {
	w := p.app.NewWindow("StreamPanel")
	w.Resize(fyne.NewSize(400, 600))

	profileFilter := widget.NewEntry()
	profileFilter.SetPlaceHolder("filter")
	profileFilter.OnChanged = func(s string) {
		p.setFilter(ctx, s)
	}

	selectedProfileButtons := []*widget.Button{
		widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
			p.cloneProfileWindow(ctx)
		}),
		widget.NewButtonWithIcon("", theme.DocumentIcon(), func() {
			p.editProfileWindow(ctx)
		}),
		widget.NewButtonWithIcon("", theme.ContentRemoveIcon(), func() {
			p.deleteProfileWindow(ctx)
		}),
	}

	menuButton := widget.NewButtonWithIcon("", theme.MenuIcon(), func() {
		p.openMenuWindow(ctx)
	})

	buttonPanel := container.NewHBox(
		menuButton,
		widget.NewSeparator(),
		widget.NewRichTextWithText("Profile:"),
		widget.NewButtonWithIcon("", theme.ContentAddIcon(), func() {
			p.newProfileWindow(ctx)
		}),
	)

	for _, button := range selectedProfileButtons {
		button.Disable()
		buttonPanel.Add(button)
	}

	topPanel := container.NewVBox(
		buttonPanel,
		profileFilter,
	)

	p.startStopButton = widget.NewButtonWithIcon("Start stream", theme.MediaRecordIcon(), func() {
		p.onStartStopButton(ctx)
	})
	p.startStopButton.Importance = widget.SuccessImportance
	p.startStopButton.Disable()

	profilesList := widget.NewList(p.profilesListLength, p.profilesListItemCreate, p.profilesListItemUpdate)
	profilesList.OnSelected = func(id widget.ListItemID) {
		p.onProfilesListSelect(id)
		for _, button := range selectedProfileButtons {
			button.Enable()
		}
	}
	profilesList.OnUnselected = func(id widget.ListItemID) {
		p.onProfilesListUnselect(id)
		for _, button := range selectedProfileButtons {
			button.Disable()
		}
	}
	p.streamTitleField = widget.NewEntry()
	p.streamTitleField.SetPlaceHolder("stream title")
	p.streamTitleField.OnSubmitted = func(s string) {
		if p.updateTimerHandler == nil {
			return
		}

		p.startStopButton.OnTapped()
		p.startStopButton.OnTapped()
	}

	p.streamDescriptionField = widget.NewMultiLineEntry()
	p.streamDescriptionField.SetPlaceHolder("stream description")
	p.streamDescriptionField.OnSubmitted = func(s string) {
		if p.updateTimerHandler == nil {
			return
		}

		p.startStopButton.OnTapped()
		p.startStopButton.OnTapped()
	}

	p.twitchCheck = widget.NewCheck("Twitch", nil)
	p.twitchCheck.SetChecked(true)
	if p.streamControllers.Twitch == nil {
		p.twitchCheck.SetChecked(false)
		p.twitchCheck.Disable()
	}
	p.youtubeCheck = widget.NewCheck("YouTube", nil)
	p.youtubeCheck.SetChecked(true)
	if p.streamControllers.YouTube == nil {
		p.youtubeCheck.SetChecked(false)
		p.youtubeCheck.Disable()
	}

	bottomPanel := container.NewVBox(
		p.streamTitleField,
		p.streamDescriptionField,
		container.NewBorder(
			nil,
			nil,
			container.NewHBox(p.twitchCheck, p.youtubeCheck),
			nil,
			p.startStopButton,
		),
	)
	w.SetContent(container.NewBorder(
		topPanel,
		bottomPanel,
		nil,
		nil,
		profilesList,
	))

	w.Show()
	p.mainWindow = w
	p.profilesListWidget = profilesList
}

func (p *Panel) getSelectedProfile() Profile {
	return p.getProfile(*p.selectedProfileName)
}

func (p *Panel) execCommand(ctx context.Context, cmdString string) {
	cmdExpanded, err := expandCommand(cmdString)
	if err != nil {
		p.displayError(err)
	}

	if len(cmdExpanded) == 0 {
		return
	}

	logger.Infof(ctx, "executing %s with arguments %v", cmdExpanded[0], cmdExpanded[1:])
	cmd := exec.Command(cmdExpanded[0], cmdExpanded[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	go func() {
		err := cmd.Run()
		if err != nil {
			p.displayError(err)
		}

		logger.Debugf(ctx, "stdout: %s", stdout.Bytes())
		logger.Debugf(ctx, "stderr: %s", stderr.Bytes())
	}()
}

func (p *Panel) startStream(ctx context.Context) {
	p.startStopMutex.Lock()
	defer p.startStopMutex.Unlock()

	if p.startStopButton.Disabled() {
		return
	}
	p.startStopButton.Disable()
	defer p.startStopButton.Enable()

	p.twitchCheck.Disable()
	p.youtubeCheck.Disable()

	p.startStopButton.SetText("Stop stream")
	p.startStopButton.Icon = theme.MediaStopIcon()
	p.startStopButton.Importance = widget.DangerImportance
	if p.updateTimerHandler != nil {
		p.updateTimerHandler.Stop()
	}
	p.updateTimerHandler = newUpdateTimerHandler(p.startStopButton)
	profile := p.getSelectedProfile()

	var twitchProfile *twitch.StreamProfile
	if p.twitchCheck.Checked && p.streamControllers.Twitch != nil {
		var err error
		twitchProfile, err = streamcontrol.GetStreamProfile[twitch.StreamProfile](p.defaultContext, profile.PerPlatform[twitch.ID])
		if err != nil {
			p.displayError(fmt.Errorf("unable to get the streaming profile for Twitch: %w", err))
			return
		}
	}

	var youtubeProfile *youtube.StreamProfile
	if p.youtubeCheck.Checked && p.streamControllers.YouTube != nil {
		var err error
		youtubeProfile, err = streamcontrol.GetStreamProfile[youtube.StreamProfile](p.defaultContext, profile.PerPlatform[youtube.ID])
		if err != nil {
			p.displayError(fmt.Errorf("unable to get the streaming profile for YouTube: %w", err))
			return
		}
	}

	if p.twitchCheck.Checked && p.streamControllers.Twitch != nil {
		err := p.streamControllers.Twitch.StartStream(
			p.defaultContext,
			p.streamTitleField.Text,
			p.streamDescriptionField.Text,
			*twitchProfile,
		)
		if err != nil {
			p.displayError(fmt.Errorf("unable to setup the stream on Twitch: %w", err))
		}
	}

	if p.youtubeCheck.Checked && p.streamControllers.YouTube != nil {
		err := p.streamControllers.YouTube.StartStream(
			p.defaultContext,
			p.streamTitleField.Text,
			p.streamDescriptionField.Text,
			*youtubeProfile,
		)
		if err != nil {
			p.displayError(fmt.Errorf("unable to start the stream on YouTube: %w", err))
		}
	}

	p.execCommand(ctx, p.data.Commands.OnStartStream)
}

func (p *Panel) stopStream(ctx context.Context) {
	p.startStopMutex.Lock()
	defer p.startStopMutex.Unlock()

	if p.streamControllers.Twitch != nil {
		p.twitchCheck.Enable()
	}
	if p.streamControllers.YouTube != nil {
		p.youtubeCheck.Enable()
	}

	p.startStopButton.SetText("Start stream")
	p.startStopButton.Icon = theme.MediaRecordIcon()
	p.startStopButton.Importance = widget.SuccessImportance

	p.updateTimerHandler.Stop()
	if p.updateTimerHandler != nil {
		p.updateTimerHandler.Stop()
	}
	p.updateTimerHandler = nil

	if p.streamControllers.Twitch != nil {
		err := p.streamControllers.Twitch.EndStream(p.defaultContext)
		if err != nil {
			p.displayError(fmt.Errorf("unable to stop the stream on Twitch: %w", err))
		}
	}

	if p.streamControllers.YouTube != nil {
		err := p.streamControllers.YouTube.EndStream(p.defaultContext)
		if err != nil {
			p.displayError(fmt.Errorf("unable to stop the stream on YouTube: %w", err))
		}
	}

	p.execCommand(ctx, p.data.Commands.OnStopStream)
}

func (p *Panel) onStartStopButton(ctx context.Context) {
	p.startStopMutex.Lock()
	defer p.startStopMutex.Unlock()

	if p.updateTimerHandler != nil {
		p.stopStream(ctx)
	} else {
		p.startStream(ctx)
	}

	p.startStopButton.Refresh()
}

func cleanTwitchCategoryName(in string) string {
	return strings.ToLower(strings.Trim(in, " "))
}

func cleanYoutubeRecordingName(in string) string {
	return strings.ToLower(strings.Trim(in, " "))
}

func (p *Panel) editProfileWindow(ctx context.Context) fyne.Window {
	oldProfile := p.getProfile(*p.selectedProfileName)
	return p.profileWindow(
		ctx,
		fmt.Sprintf("Edit the profile '%s'", oldProfile.Name),
		oldProfile,
		func(ctx context.Context, profile Profile) error {
			if err := p.profileCreateOrUpdate(ctx, profile); err != nil {
				return fmt.Errorf("unable to create profile '%s': %w", profile.Name, err)
			}
			if profile.Name != oldProfile.Name {
				if err := p.profileDelete(ctx, oldProfile.Name); err != nil {
					return fmt.Errorf("unable to delete profile '%s': %w", oldProfile.Name, err)
				}
			}
			p.profilesListWidget.UnselectAll()
			return nil
		},
	)
}

func (p *Panel) cloneProfileWindow(ctx context.Context) fyne.Window {
	oldProfile := p.getProfile(*p.selectedProfileName)
	return p.profileWindow(
		ctx,
		"Create a profile",
		oldProfile,
		func(ctx context.Context, profile Profile) error {
			if oldProfile.Name == profile.Name {
				return fmt.Errorf("profile with name '%s' already exists", profile.Name)
			}
			if err := p.profileCreateOrUpdate(ctx, profile); err != nil {
				return err
			}
			p.profilesListWidget.UnselectAll()
			return nil
		},
	)
}

func (p *Panel) deleteProfileWindow(ctx context.Context) fyne.Window {
	w := p.app.NewWindow("Delete the profile?")

	yesButton := widget.NewButton("YES", func() {
		err := p.profileDelete(ctx, *p.selectedProfileName)
		if err != nil {
			p.displayError(err)
		}
		p.profilesListWidget.UnselectAll()
		w.Close()
	})

	noButton := widget.NewButton("NO", func() {
		w.Close()
	})

	w.SetContent(container.NewBorder(
		nil,
		container.NewHBox(
			yesButton, noButton,
		),
		nil,
		nil,
		widget.NewRichTextWithText(fmt.Sprintf("Delete profile '%s'", *p.selectedProfileName)),
	))
	w.Show()
	return w
}

func (p *Panel) newProfileWindow(ctx context.Context) fyne.Window {
	return p.profileWindow(
		ctx,
		"Create a profile",
		Profile{},
		func(ctx context.Context, profile Profile) error {
			found := false
			for _, platCfg := range p.data.Backends {
				_, ok := platCfg.GetStreamProfile(profile.Name)
				if ok {
					found = true
					break
				}
			}
			if found {
				return fmt.Errorf("profile with name '%s' already exists", profile.Name)
			}
			if err := p.profileCreateOrUpdate(ctx, profile); err != nil {
				return err
			}
			p.profilesListWidget.UnselectAll()
			return nil
		},
	)
}

func ptr[T any](in T) *T {
	return &in
}

func (p *Panel) profileWindow(
	ctx context.Context,
	windowName string,
	values Profile,
	commitFn func(context.Context, Profile) error,
) fyne.Window {
	var (
		twitchProfile  *twitch.StreamProfile
		youtubeProfile *youtube.StreamProfile
	)

	w := p.app.NewWindow(windowName)
	w.Resize(fyne.NewSize(400, 300))
	profileName := widget.NewEntry()
	profileName.SetPlaceHolder("profile name")
	profileName.SetText(string(values.Name))
	defaultStreamTitle := widget.NewEntry()
	defaultStreamTitle.SetPlaceHolder("default stream title")
	defaultStreamTitle.SetText(values.DefaultStreamTitle)
	defaultStreamDescription := widget.NewMultiLineEntry()
	defaultStreamDescription.SetPlaceHolder("default stream description")
	defaultStreamDescription.SetText(values.DefaultStreamDescription)

	tagsEntryField := widget.NewEntry()
	tagsEntryField.SetPlaceHolder("add a tag")
	s := tagsEntryField.Size()
	s.Width = 200
	tags := map[string]struct{}{}
	tagsEntryField.Resize(s)
	tagsContainer := container.NewHBox()

	addTag := func(tag string) {
		if tag == "" {
			return
		}
		if _, ok := tags[tag]; ok {
			return
		}
		tags[tag] = struct{}{}
		tagContainer := container.NewHBox(
			widget.NewLabel(tag),
		)
		tagContainerRemoveButton := widget.NewButtonWithIcon("", theme.ContentClearIcon(), func() {
			tagsContainer.Remove(tagContainer)
			delete(tags, tag)
		})
		tagContainer.Add(tagContainerRemoveButton)
		tagsContainer.Add(tagContainer)
	}
	tagsEntryField.OnSubmitted = func(text string) {
		addTag(text)
		tagsEntryField.SetText("")
	}

	var bottomContent []fyne.CanvasObject

	bottomContent = append(bottomContent, widget.NewSeparator())
	bottomContent = append(bottomContent, widget.NewLabel("Twitch:"))
	if p.streamControllers.Twitch != nil {
		if platProfile := values.PerPlatform[twitch.ID]; platProfile != nil {
			twitchProfile = ptr(streamcontrol.GetPlatformSpecificConfig[twitch.StreamProfile](ctx, platProfile))
			for _, tag := range twitchProfile.Tags {
				addTag(tag)
			}
		} else {
			twitchProfile = &twitch.StreamProfile{}
		}

		twitchCategory := widget.NewEntry()
		twitchCategory.SetPlaceHolder("twitch category")

		selectTwitchCategoryBox := container.NewHBox()
		bottomContent = append(bottomContent, selectTwitchCategoryBox)
		twitchCategory.OnChanged = func(text string) {
			selectTwitchCategoryBox.RemoveAll()
			if text == "" {
				return
			}
			text = cleanTwitchCategoryName(text)
			count := 0
			for _, cat := range p.data.Cache.Twitch.Categories {
				if strings.Contains(cleanTwitchCategoryName(cat.Name), text) {
					selectedTwitchCategoryContainer := container.NewHBox()
					catName := cat.Name
					tagContainerRemoveButton := widget.NewButtonWithIcon(catName, theme.ContentAddIcon(), func() {
						twitchCategory.OnSubmitted(catName)
					})
					selectedTwitchCategoryContainer.Add(tagContainerRemoveButton)
					selectTwitchCategoryBox.Add(selectedTwitchCategoryContainer)
					count++
					if count > 10 {
						break
					}
				}
			}
		}

		selectedTwitchCategoryBox := container.NewHBox()
		bottomContent = append(bottomContent, selectedTwitchCategoryBox)

		setSelectedTwitchCategory := func(catName string) {
			selectedTwitchCategoryBox.RemoveAll()
			selectedTwitchCategoryContainer := container.NewHBox()
			tagContainerRemoveButton := widget.NewButtonWithIcon(catName, theme.ContentClearIcon(), func() {
				selectedTwitchCategoryBox.Remove(selectedTwitchCategoryContainer)
				twitchProfile.CategoryName = nil
			})
			selectedTwitchCategoryContainer.Add(tagContainerRemoveButton)
			selectedTwitchCategoryBox.Add(selectedTwitchCategoryContainer)
			twitchProfile.CategoryName = &catName
		}

		if twitchProfile.CategoryName != nil {
			setSelectedTwitchCategory(*twitchProfile.CategoryName)
		}
		if twitchProfile.CategoryID != nil {
			catID := *twitchProfile.CategoryID
			for _, cat := range p.data.Cache.Twitch.Categories {
				if cat.ID == catID {
					setSelectedTwitchCategory(cat.Name)
					break
				}
			}
		}

		twitchCategory.OnSubmitted = func(text string) {
			if text == "" {
				return
			}
			text = cleanTwitchCategoryName(text)
			for _, cat := range p.data.Cache.Twitch.Categories {
				if cleanTwitchCategoryName(cat.Name) == text {
					setSelectedTwitchCategory(cat.Name)
					go func() {
						time.Sleep(100 * time.Millisecond)
						twitchCategory.SetText("")
					}()
					return
				}
			}
		}
		bottomContent = append(bottomContent, twitchCategory)
	} else {
		bottomContent = append(bottomContent, widget.NewLabel("Twitch is disabled"))
	}

	bottomContent = append(bottomContent, widget.NewSeparator())
	bottomContent = append(bottomContent, widget.NewLabel("YouTube:"))
	if p.streamControllers.YouTube != nil {
		if platProfile := values.PerPlatform[youtube.ID]; platProfile != nil {
			youtubeProfile = ptr(streamcontrol.GetPlatformSpecificConfig[youtube.StreamProfile](ctx, platProfile))
			for _, tag := range youtubeProfile.Tags {
				addTag(tag)
			}
		} else {
			youtubeProfile = &youtube.StreamProfile{}
		}

		youtubeTemplate := widget.NewEntry()
		youtubeTemplate.SetPlaceHolder("youtube live recording template")

		selectYoutubeTemplateBox := container.NewHBox()
		bottomContent = append(bottomContent, selectYoutubeTemplateBox)
		youtubeTemplate.OnChanged = func(text string) {
			selectYoutubeTemplateBox.RemoveAll()
			if text == "" {
				return
			}
			text = cleanYoutubeRecordingName(text)
			count := 0
			for _, bc := range p.data.Cache.Youtube.Broadcasts {
				if strings.Contains(cleanYoutubeRecordingName(bc.Snippet.Title), text) {
					selectedYoutubeRecordingsContainer := container.NewHBox()
					recName := bc.Snippet.Title
					tagContainerRemoveButton := widget.NewButtonWithIcon(recName, theme.ContentAddIcon(), func() {
						youtubeTemplate.OnSubmitted(recName)
					})
					selectedYoutubeRecordingsContainer.Add(tagContainerRemoveButton)
					selectYoutubeTemplateBox.Add(selectedYoutubeRecordingsContainer)
					count++
					if count > 10 {
						break
					}
				}
			}
		}

		selectedYoutubeBroadcastBox := container.NewHBox()
		bottomContent = append(bottomContent, selectedYoutubeBroadcastBox)

		setSelectedYoutubeBroadcast := func(bc *youtube.LiveBroadcast) {
			selectedYoutubeBroadcastBox.RemoveAll()
			selectedYoutubeBroadcastContainer := container.NewHBox()
			recName := bc.Snippet.Title
			tagContainerRemoveButton := widget.NewButtonWithIcon(recName, theme.ContentClearIcon(), func() {
				selectedYoutubeBroadcastBox.Remove(selectedYoutubeBroadcastContainer)
				youtubeProfile.TemplateBroadcastIDs = youtubeProfile.TemplateBroadcastIDs[:0]
			})
			selectedYoutubeBroadcastContainer.Add(tagContainerRemoveButton)
			selectedYoutubeBroadcastBox.Add(selectedYoutubeBroadcastContainer)
			youtubeProfile.TemplateBroadcastIDs = []string{bc.Id}
		}

		for _, bcID := range youtubeProfile.TemplateBroadcastIDs {
			for _, bc := range p.data.Cache.Youtube.Broadcasts {
				if bc.Id != bcID {
					continue
				}
				setSelectedYoutubeBroadcast(bc)
			}
		}

		youtubeTemplate.OnSubmitted = func(text string) {
			if text == "" {
				return
			}
			text = cleanYoutubeRecordingName(text)
			for _, bc := range p.data.Cache.Youtube.Broadcasts {
				if cleanYoutubeRecordingName(bc.Snippet.Title) == text {
					setSelectedYoutubeBroadcast(bc)
					go func() {
						time.Sleep(100 * time.Millisecond)
						youtubeTemplate.SetText("")
					}()
					return
				}
			}
		}
		bottomContent = append(bottomContent, youtubeTemplate)
	} else {
		bottomContent = append(bottomContent, widget.NewLabel("YouTube is disabled"))
	}

	bottomContent = append(bottomContent,
		widget.NewSeparator(),
		container.NewVBox(
			widget.NewLabel("common:"),
			tagsContainer,
			tagsEntryField,
		),
		widget.NewButton("Save", func() {
			if tagsEntryField.Text != "" {
				tagsEntryField.OnSubmitted(tagsEntryField.Text)
			}
			_tags := make([]string, len(tags))
			for k := range tags {
				if k == "" {
					continue
				}
				_tags = append(_tags, k)
			}
			profile := Profile{
				Name:        streamcontrol.ProfileName(profileName.Text),
				PerPlatform: map[streamcontrol.PlatformName]streamcontrol.AbstractStreamProfile{},
				ProfileMetadata: ProfileMetadata{
					DefaultStreamTitle:       defaultStreamTitle.Text,
					DefaultStreamDescription: defaultStreamDescription.Text,
					MaxOrder:                 0,
				},
			}
			if twitchProfile != nil {
				twitchProfile.Tags = _tags
				profile.PerPlatform[twitch.ID] = twitchProfile
			}
			if youtubeProfile != nil {
				youtubeProfile.Tags = _tags
				profile.PerPlatform[youtube.ID] = youtubeProfile
			}
			err := commitFn(ctx, profile)
			if err != nil {
				p.displayError(err)
				return
			}
			w.Close()
		}),
	)

	w.SetContent(container.NewBorder(
		container.NewVBox(
			profileName,
			defaultStreamTitle,
		),
		container.NewVBox(
			bottomContent...,
		),
		nil,
		nil,
		defaultStreamDescription,
	))
	w.Show()
	return w
}

func (p *Panel) displayError(err error) {
	w := p.app.NewWindow("Got an error: " + err.Error())
	errorMessage := fmt.Sprintf("Error: %v\n\nstack trace:\n%s", err, debug.Stack())
	w.Resize(fyne.NewSize(400, 300))
	textWidget := widget.NewMultiLineEntry()
	textWidget.SetText(errorMessage)
	textWidget.Wrapping = fyne.TextWrapWord
	textWidget.TextStyle = fyne.TextStyle{
		Bold:      true,
		Monospace: true,
	}
	w.SetContent(textWidget)
	w.Show()
}
