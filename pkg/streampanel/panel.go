package streampanel

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path"
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
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
)

const (
	delayAutoSubmit = time.Second * 30
)

type Profile struct {
	Name        streamcontrol.ProfileName
	PerPlatform map[streamcontrol.PlatformName]streamcontrol.AbstractStreamProfile
	MaxOrder    int
}

type Panel struct {
	dataLock sync.Mutex
	data     panelData

	config             config
	startStopMutex     sync.Mutex
	updateTimerHandler *updateTimerHandler
	streamControllers  struct {
		Twitch  *twitch.Twitch
		YouTube *youtube.YouTube
	}
	profilesOrder         []streamcontrol.ProfileName
	profilesOrderFiltered []streamcontrol.ProfileName
	selectedProfileName   *streamcontrol.ProfileName
	defaultContext        context.Context

	mainWindow           fyne.Window
	startStopButton      *widget.Button
	activitiesListWidget fyne.Widget
	commentField         *widget.Entry

	dataPath    string
	filterValue string
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
	p.defaultContext = ctx
	logger.Debug(ctx, "config", p.config)

	if err := p.loadData(); err != nil {
		return fmt.Errorf("unable to load the data '%s': %w", p.dataPath, err)
	}

	if err := p.initStreamControllers(); err != nil {
		return fmt.Errorf("unable to initialize stream controllers: %w", err)
	}

	a := fyneapp.New()
	p.initMainWindow(ctx, a)

	p.initTwitchData(a)

	p.mainWindow.ShowAndRun()
	return nil
}

func (p *Panel) initTwitchData(a fyne.App) {
	logger.FromCtx(p.defaultContext).Debugf("initializing Twitch data")
	defer logger.FromCtx(p.defaultContext).Debugf("endof initializing Twitch data")

	if c := len(p.data.Cache.Twitch.Categories); c != 0 {
		logger.FromCtx(p.defaultContext).Debugf("already have categories (count: %d)", c)
		return
	}

	twitch := p.streamControllers.Twitch
	if twitch == nil {
		logger.FromCtx(p.defaultContext).Debugf("twitch controller is not initialized")
		return
	}

	allCategories, err := twitch.GetAllCategories(p.defaultContext)
	if err != nil {
		p.displayError(a, err)
		return
	}

	logger.FromCtx(p.defaultContext).Debugf("got categories: %#+v", allCategories)

	func() {
		p.dataLock.Lock()
		defer p.dataLock.Unlock()
		p.data.Cache.Twitch.Categories = allCategories
	}()

	err = p.saveData()
	errmon.ObserveErrorCtx(p.defaultContext, err)
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

func (p *Panel) loadData() error {
	dataPath, err := p.getExpandedDataPath()
	if err != nil {
		return fmt.Errorf("unable to get the path to the data file: %w", err)
	}

	_, err = os.Stat(dataPath)
	switch {
	case err == nil:
		return readPanelDataFromPath(p.defaultContext, dataPath, &p.data)
	case os.IsNotExist(err):
		logger.FromCtx(p.defaultContext).Debugf("cannot find file '%s', creating", dataPath)
		p.data = newPanelData()
		if err := p.saveData(); err != nil {
			logger.FromCtx(p.defaultContext).Errorf("cannot create file '%s': %v", dataPath, err)
		}
		return nil
	default:
		return fmt.Errorf("unable to access file '%s': %w", dataPath, err)
	}
}

func (p *Panel) savePlatformConfig(
	platID streamcontrol.PlatformName,
	platCfg *streamcontrol.AbstractPlatformConfig,
) error {
	logger.FromCtx(p.defaultContext).Debugf("savePlatformConfig('%s', '%#+v')", platID, platCfg)
	defer logger.FromCtx(p.defaultContext).Debugf("endof savePlatformConfig('%s', '%#+v')", platID, platCfg)
	p.dataLock.Lock()
	defer p.dataLock.Unlock()
	p.data.Backends[platID] = platCfg
	return p.saveData()
}

func (p *Panel) initStreamControllers() error {
	for platName, cfg := range p.data.Backends {
		var err error
		switch strings.ToLower(string(platName)) {
		case strings.ToLower(string(twitch.ID)):
			p.streamControllers.Twitch, err = newTwitch(p.defaultContext, cfg, func(cfg *streamcontrol.AbstractPlatformConfig) error {
				return p.savePlatformConfig(twitch.ID, cfg)
			})
		case strings.ToLower(string(youtube.ID)):
			p.streamControllers.YouTube, err = newYouTube(p.defaultContext, cfg, func(cfg *streamcontrol.AbstractPlatformConfig) error {
				return p.savePlatformConfig(youtube.ID, cfg)
			})
		}
		if err != nil {
			return fmt.Errorf("unable to initialize '%s': %w", platName, err)
		}
	}
	return nil
}

func (p *Panel) onProfileCreatedOrUpdated(profile Profile) {
	logger.Trace(p.defaultContext, "onProfileCreatedOrUpdated(%s)", profile.Name)
	for platformName, platformProfile := range profile.PerPlatform {
		p.data.Backends[platformName].StreamProfiles[profile.Name] = platformProfile
	}
	p.rearrangeProfiles()
	p.saveData()
}

func (p *Panel) onProfileDeleted(profileName streamcontrol.ProfileName) {
	logger.Trace(p.defaultContext, "onProfileDeleted(%s)", profileName)
	for platformName := range p.data.Backends {
		delete(p.data.Backends[platformName].StreamProfiles, profileName)
	}
	p.rearrangeProfiles()
	p.saveData()
}

func (p *Panel) saveData() error {
	dataPath, err := p.getExpandedDataPath()
	if err != nil {
		return fmt.Errorf("unable to get the path to the data file: %w", err)
	}

	return writePanelDataToPath(p.defaultContext, dataPath, p.data)
}

func (p *Panel) getProfile(profileName streamcontrol.ProfileName) Profile {
	prof := Profile{
		Name: profileName,
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

func (p *Panel) rearrangeProfiles() {
	curProfilesMap := map[streamcontrol.ProfileName]*Profile{}
	for platName, platCfg := range p.data.Backends {
		for profName, platProf := range platCfg.StreamProfiles {
			prof := curProfilesMap[profName]
			if prof == nil {
				prof = &Profile{
					Name: profName,
				}
				curProfilesMap[profName] = prof
			}
			prof.PerPlatform[platName] = platProf
			prof.MaxOrder = xmath.Max(prof.MaxOrder, platProf.GetOrder())
		}
	}

	curProfiles := make([]Profile, 0, len(curProfilesMap))
	for _, profile := range curProfilesMap {
		curProfiles = append(curProfiles, *profile)
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
	for _, profile := range curProfiles {
		p.profilesOrder = append(p.profilesOrder, profile.Name)
	}

	p.refilterProfiles()
}

func (p *Panel) refilterProfiles() {
	if cap(p.profilesOrderFiltered) < len(p.profilesOrder) {
		p.profilesOrderFiltered = make([]streamcontrol.ProfileName, 0, len(p.profilesOrder)*2)
	} else {
		p.profilesOrderFiltered = p.profilesOrderFiltered[:0]
	}
	if p.filterValue == "" {
		p.profilesOrderFiltered = p.profilesOrderFiltered[:len(p.profilesOrder)]
		copy(p.profilesOrderFiltered, p.profilesOrder)
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
			p.profilesOrderFiltered = append(p.profilesOrderFiltered, profileName)
		}
	}
	p.activitiesListWidget.Refresh()
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

func (p *Panel) onActivitiesListSelect(
	id widget.ListItemID,
) {
	p.startStopButton.Enable()

	shouldRestart := p.updateTimerHandler != nil
	if shouldRestart {
		p.startStopButton.OnTapped()
		p.commentField.SetText("")
	}
	p.selectedProfileName = ptrCopy(p.profilesOrder[id])
	if shouldRestart {
		p.startStopButton.OnTapped()
	}
}

func (p *Panel) setFilter(filter string) {
	p.filterValue = filter
	p.refilterProfiles()
}

func (p *Panel) initMainWindow(ctx context.Context, a fyne.App) {
	w := a.NewWindow("TimeTracker")

	p.startStopButton = widget.NewButtonWithIcon("", theme.MediaPlayIcon(), p.onStartStopButton)
	p.startStopButton.Importance = widget.SuccessImportance
	p.startStopButton.Disable()

	profilesList := widget.NewList(p.profilesListLength, p.profilesListItemCreate, p.profilesListItemUpdate)
	profilesList.OnSelected = func(id widget.ListItemID) {
		p.onActivitiesListSelect(id)
	}
	profileFilter := widget.NewEntry()
	profileFilter.SetPlaceHolder("filter")
	profileFilter.OnChanged = p.setFilter
	topPanel := container.NewVBox(
		container.NewHBox(
			widget.NewButtonWithIcon("Profile", theme.ContentAddIcon(), func() {
				p.newProfileWindow(ctx, a)
			}),
		),
		profileFilter,
	)

	p.commentField = widget.NewEntry()
	p.commentField.OnSubmitted = func(s string) {
		if p.updateTimerHandler == nil {
			return
		}

		p.startStopButton.OnTapped()
		p.startStopButton.OnTapped()
	}

	bottomPanel := container.NewAdaptiveGrid(
		1,
		p.commentField,
		p.startStopButton,
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
	p.activitiesListWidget = profilesList
}

func (p *Panel) onStartStopButton() {
	p.startStopMutex.Lock()
	defer p.startStopMutex.Unlock()

	if p.updateTimerHandler != nil {
		p.startStopButton.Icon = theme.MediaPlayIcon()
		p.startStopButton.Importance = widget.SuccessImportance
		p.updateTimerHandler.Stop()
		panic("stream stopping is not implemented")
		p.updateTimerHandler = nil
		p.startStopButton.SetText("")
	} else {
		p.startStopButton.Icon = theme.DownloadIcon()
		p.startStopButton.Importance = widget.DangerImportance
		p.updateTimerHandler = newUpdateTimerHandler(p.startStopButton)
		panic("stream starting is not implemented")
	}

	p.startStopButton.Refresh()
}

func (p *Panel) newProfileWindow(_ context.Context, a fyne.App) fyne.Window {
	w := a.NewWindow("Create a profile")
	w.Resize(fyne.NewSize(400, 300))
	activityTitle := widget.NewEntry()
	activityTitle.SetPlaceHolder("title")
	activityDescription := widget.NewMultiLineEntry()
	activityDescription.SetPlaceHolder("description")

	tagsEntryField := widget.NewEntry()
	tagsEntryField.SetPlaceHolder("add a tag")
	s := tagsEntryField.Size()
	s.Width = 200
	tags := map[string]struct{}{}
	tagsEntryField.Resize(s)
	tagsContainer := container.NewHBox()
	tagsEntryField.OnSubmitted = func(text string) {
		tags[text] = struct{}{}
		tagContainer := container.NewHBox(
			widget.NewLabel(text),
		)
		tagContainerRemoveButton := widget.NewButtonWithIcon("", theme.ContentClearIcon(), func() {
			tagsContainer.Remove(tagContainer)
			delete(tags, text)
		})
		tagContainer.Add(tagContainerRemoveButton)
		tagsContainer.Add(tagContainer)
		tagsEntryField.SetText("")
	}

	var bottomContent []fyne.CanvasObject
	if p.streamControllers.Twitch != nil {
		var options []string
		for _, category := range p.data.Cache.Twitch.Categories {
			options = append(options, category.Name)
		}
		sort.Slice(options, func(i, j int) bool {
			return options[i] < options[j]
		})
		twitchCategory := widget.NewSelectEntry(options)
		bottomContent = append(bottomContent, twitchCategory)
	}

	bottomContent = append(bottomContent,
		container.NewAdaptiveGrid(1,
			tagsContainer,
			tagsEntryField,
		),
		widget.NewButton("Create", func() {
			if tagsEntryField.Text != "" {
				tagsEntryField.OnSubmitted(tagsEntryField.Text)
			}
			err := fmt.Errorf("creating a profile is not implemented")
			if err != nil {
				p.displayError(a, err)
				return
			}
			w.Close()
		}),
	)

	w.SetContent(container.NewBorder(
		container.NewVBox(
			activityTitle,
		),
		container.NewVBox(
			bottomContent...,
		),
		nil,
		nil,
		activityDescription,
	))
	w.Show()
	return w
}

func (p *Panel) displayError(a fyne.App, err error) {
	w := a.NewWindow("Got an error: " + err.Error())
	errorMessage := fmt.Sprintf("Error: %v\n\nstack trace:\n%s", err, debug.Stack())
	w.Resize(fyne.NewSize(400, 300))
	w.SetContent(widget.NewLabelWithStyle(errorMessage, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
	w.Show()
}
