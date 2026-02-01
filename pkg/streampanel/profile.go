package streampanel

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/go-ng/xmath"
	"github.com/goccy/go-yaml"
	gconsts "github.com/xaionaro-go/streamctl/pkg/consts"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	streamdconfig "github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/xsync"
)

type Profile struct {
	streamdconfig.ProfileMetadata
	Name      streamcontrol.ProfileName
	PerStream map[streamcontrol.StreamIDFullyQualified]streamcontrol.StreamProfile
}

func getStreamProfileBase(prof streamcontrol.StreamProfile) *streamcontrol.StreamProfileBase {
	if prof == nil {
		return &streamcontrol.StreamProfileBase{}
	}
	raw := streamcontrol.ToRawMessage(prof)
	var base streamcontrol.StreamProfileBase
	_ = yaml.Unmarshal(raw, &base)
	return &base
}

func (p *Panel) editProfileWindow(ctx context.Context) fyne.Window {
	if p.selectedProfileName == nil {
		return nil
	}
	oldProfile := xsync.DoA2R1(ctx, &p.configCacheLocker, getProfile, p.configCache, *p.selectedProfileName)
	w := p.profileWindow(
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
	return w
}

func (p *Panel) cloneProfileWindow(ctx context.Context) fyne.Window {
	if p.selectedProfileName == nil {
		return nil
	}
	oldProfile := xsync.DoA2R1(ctx, &p.configCacheLocker, getProfile, p.configCache, *p.selectedProfileName)
	w := p.profileWindow(
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
	return w
}

func (p *Panel) deleteProfileWindow(ctx context.Context) fyne.Window {
	if p.selectedProfileName == nil {
		return nil
	}
	w := p.app.NewWindow(gconsts.AppName + ": Delete the profile?")

	yesButton := widget.NewButton("YES", func() {
		err := p.profileDelete(ctx, *p.selectedProfileName)
		if err != nil {
			p.DisplayError(err)
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
	w := p.profileWindow(
		ctx,
		"Create a profile",
		Profile{},
		func(ctx context.Context, profile Profile) error {
			found := false
			p.configCacheLocker.Do(ctx, func() {
				for _, platCfg := range p.configCache.Backends {
					for _, accRaw := range platCfg.Accounts {
						for _, sProfs := range accRaw.GetStreamProfiles() {
							if _, ok := sProfs[profile.Name]; ok {
								found = true
								break
							}
						}
						if found {
							break
						}
					}
					if found {
						break
					}
				}
			})
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
	return w
}

func (p *Panel) profileWindow(
	ctx context.Context,
	windowName string,
	values Profile,
	commitFn func(context.Context, Profile) error,
) fyne.Window {
	w := p.app.NewWindow(windowName)
	resizeWindow(w, fyne.NewSize(1500, 1000))
	profileName := widget.NewEntry()
	profileName.SetPlaceHolder("profile name")
	profileName.SetText(string(values.Name))
	defaultStreamTitle := widget.NewEntry()
	defaultStreamTitle.OnChanged = func(s string) {
		if len(s) > youtubeTitleLength {
			defaultStreamTitle.SetText(s[:youtubeTitleLength])
		}
	}
	defaultStreamTitle.SetPlaceHolder("default stream title")
	defaultStreamTitle.SetText(values.DefaultStreamTitle)
	defaultStreamDescription := widget.NewMultiLineEntry()
	defaultStreamDescription.SetPlaceHolder("default stream description")
	defaultStreamDescription.SetText(values.DefaultStreamDescription)
	defaultStreamDescription.SetMinRowsVisible(3)

	backendEnabled := map[streamcontrol.PlatformID]bool{}
	backendData := map[streamcontrol.PlatformID]any{}
	for _, backendID := range []streamcontrol.PlatformID{
		obs.ID,
		twitch.ID,
		kick.ID,
		youtube.ID,
	} {
		isEnabled, err := p.StreamD.IsBackendEnabled(ctx, backendID)
		if err != nil {
			w.Close()
			p.DisplayError(
				fmt.Errorf("unable to get info if backend '%s' is enabled: %w", backendID, err),
			)
			return nil
		}
		backendEnabled[backendID] = isEnabled

		info, err := p.StreamD.GetBackendInfo(ctx, backendID, true)
		if err != nil {
			w.Close()
			p.DisplayError(fmt.Errorf("unable to get data of backend '%s': %w", backendID, err))
			return nil
		}

		backendData[backendID] = info.Data
	}

	var bottomContentLeft, bottomContentRight, bottomContentCommon []fyne.CanvasObject

	// Initialize individual platform profiles to track their streams
	perStream := make(map[streamcontrol.StreamIDFullyQualified]streamcontrol.StreamProfile)
	maps.Copy(perStream, values.PerStream)

	streamsUI, streamsSaveFunc := p.newGlobalStreamsSelectUI(
		ctx,
		w,
		backendEnabled,
		backendData,
		perStream,
	)

	bottomContentRight = append(bottomContentRight, widget.NewRichTextFromMarkdown("# STREAMS:"))
	bottomContentRight = append(bottomContentRight, streamsUI)

	bottomContentCommon = append(bottomContentCommon,
		widget.NewButton("Save", func() {
			profile := Profile{
				Name: streamcontrol.ProfileName(profileName.Text),
				ProfileMetadata: streamdconfig.ProfileMetadata{
					DefaultStreamTitle:       defaultStreamTitle.Text,
					DefaultStreamDescription: defaultStreamDescription.Text,
					MaxOrder:                 0,
				},
				PerStream: map[streamcontrol.StreamIDFullyQualified]streamcontrol.StreamProfile{},
			}

			updatedStreams, err := streamsSaveFunc()
			if err != nil {
				p.DisplayError(err)
				return
			}
			profile.PerStream = updatedStreams

			err = commitFn(ctx, profile)
			if err != nil {
				p.DisplayError(err)
				return
			}
			w.Close()
		}),
		widget.NewButton("Cancel", func() {
			w.Close()
		}),
	)

	var content fyne.CanvasObject
	if isMobile() {
		content = container.NewVScroll(container.NewVBox(
			profileName,
			defaultStreamTitle,
			defaultStreamDescription,
			container.NewVBox(bottomContentLeft...),
			container.NewVBox(bottomContentRight...),
			container.NewVBox(bottomContentCommon...),
		))
	} else {
		content = container.NewBorder(
			nil,
			container.NewVBox(bottomContentCommon...),
			nil,
			nil,
			container.NewHSplit(
				container.NewVScroll(container.NewBorder(
					container.NewVBox(
						profileName,
						defaultStreamTitle,
					),
					container.NewVBox(
						bottomContentLeft...,
					),
					nil,
					nil,
					defaultStreamDescription,
				)),
				container.NewVScroll(container.NewVBox(
					bottomContentRight...,
				)),
			),
		)
	}

	w.SetContent(content)
	w.Show()
	return w
}

func (p *Panel) profileCreateOrUpdate(ctx context.Context, profile Profile) error {
	cfg, err := p.GetStreamDConfig(ctx)
	if err != nil {
		fmt.Printf("DEBUG: profileCreateOrUpdate GetStreamDConfig FAILED: %v\n", err)
		return fmt.Errorf("unable to get config: %w", err)
	}

	logger.Debugf(ctx, "profileCreateOrUpdate(%s)", profile.Name)

	// First, remove this profile from all accounts across all platforms.
	// We will then add it back to the specific accounts it belongs to.
	for _, platCfg := range cfg.Backends {
		if platCfg == nil {
			continue
		}
		for accID, accRaw := range platCfg.Accounts {
			var accMap map[string]any
			_ = yaml.Unmarshal(accRaw, &accMap)
			if accMap == nil {
				continue
			}
			profilesRaw, ok := accMap["stream_profiles"]
			var allStreamsProfiles map[streamcontrol.StreamID]map[streamcontrol.ProfileName]any
			if ok {
				_ = yaml.Unmarshal(streamcontrol.ToRawMessage(profilesRaw), &allStreamsProfiles)
			}
			if allStreamsProfiles == nil {
				allStreamsProfiles = make(map[streamcontrol.StreamID]map[streamcontrol.ProfileName]any)
			}
			for _, streamsProfiles := range allStreamsProfiles {
				if _, ok := streamsProfiles[profile.Name]; ok {
					delete(streamsProfiles, profile.Name)
				}
			}
			accMap["stream_profiles"] = allStreamsProfiles
			newRaw, _ := yaml.Marshal(accMap)
			platCfg.Accounts[accID] = newRaw
		}
	}

	// Now add/update the profile in the specified accounts.
	for sID, sProf := range profile.PerStream {
		platCfg := cfg.Backends[sID.PlatformID]
		if platCfg == nil {
			platCfg = &streamcontrol.AbstractPlatformConfig{
				Accounts: make(map[streamcontrol.AccountID]streamcontrol.RawMessage),
			}
			cfg.Backends[sID.PlatformID] = platCfg
		}
		accRaw, ok := platCfg.Accounts[sID.AccountID]
		if !ok {
			// If account doesn't exist, we skip or create a minimal one.
			// Let's assume it should exist if it's in the UI.
			continue
		}

		var accMap map[string]any
		_ = yaml.Unmarshal(accRaw, &accMap)
		if accMap == nil {
			accMap = make(map[string]any)
		}
		profilesRaw, ok := accMap["stream_profiles"]
		var allStreamsProfiles map[streamcontrol.StreamID]map[streamcontrol.ProfileName]any
		if ok {
			_ = yaml.Unmarshal(streamcontrol.ToRawMessage(profilesRaw), &allStreamsProfiles)
		}
		if allStreamsProfiles == nil {
			allStreamsProfiles = make(map[streamcontrol.StreamID]map[streamcontrol.ProfileName]any)
		}
		profiles := allStreamsProfiles[sID.StreamID]
		if profiles == nil {
			profiles = make(map[streamcontrol.ProfileName]any)
			allStreamsProfiles[sID.StreamID] = profiles
		}
		profiles[profile.Name] = sProf
		accMap["stream_profiles"] = allStreamsProfiles
		newAccRaw, _ := yaml.Marshal(accMap)
		platCfg.Accounts[sID.AccountID] = newAccRaw
	}

	cfg.ProfileMetadata[profile.Name] = profile.ProfileMetadata

	err = p.SetStreamDConfig(ctx, cfg)
	if err != nil {
		fmt.Printf("DEBUG: profileCreateOrUpdate SetStreamDConfig FAILED: %v\n", err)
		return fmt.Errorf("unable to set config: %w", err)
	}

	if err := p.rearrangeProfiles(ctx); err != nil {
		return fmt.Errorf("unable to re-arrange profiles: %w", err)
	}

	if err := p.StreamD.SaveConfig(ctx); err != nil {
		return fmt.Errorf("unable to save the profile: %w", err)
	}
	fmt.Printf("DEBUG: profileCreateOrUpdate FINISHED for profile: %s\n", profile.Name)
	return nil
}

func (p *Panel) profileDelete(ctx context.Context, profileName streamcontrol.ProfileName) error {
	fmt.Printf("DEBUG: profileDelete called for profile: %s\n", profileName)
	cfg, err := p.GetStreamDConfig(ctx)
	if err != nil {
		return fmt.Errorf("unable to get config: %w", err)
	}

	logger.Debugf(ctx, "onProfileDeleted(%s)", profileName)
	for _, platCfg := range cfg.Backends {
		if platCfg == nil {
			continue
		}
		for accID, accRaw := range platCfg.Accounts {
			var accMap map[string]any
			_ = yaml.Unmarshal(accRaw, &accMap)
			if accMap == nil {
				continue
			}
			profilesRaw, ok := accMap["stream_profiles"]
			if !ok {
				continue
			}
			var allStreamsProfiles map[streamcontrol.StreamID]map[streamcontrol.ProfileName]any
			_ = yaml.Unmarshal(streamcontrol.ToRawMessage(profilesRaw), &allStreamsProfiles)
			if allStreamsProfiles == nil {
				continue
			}
			changed := false
			for _, profiles := range allStreamsProfiles {
				if _, ok := profiles[profileName]; ok {
					delete(profiles, profileName)
					changed = true
				}
			}
			if changed {
				accMap["stream_profiles"] = allStreamsProfiles
				newRaw, _ := yaml.Marshal(accMap)
				platCfg.Accounts[accID] = newRaw
			}
		}
	}
	delete(cfg.ProfileMetadata, profileName)

	err = p.SetStreamDConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("unable to set config: %w", err)
	}

	if err := p.rearrangeProfiles(ctx); err != nil {
		return fmt.Errorf("unable to re-arrange the profiles: %w", err)
	}
	if err := p.StreamD.SaveConfig(ctx); err != nil {
		return fmt.Errorf("unable to save the profile: %w", err)
	}

	return nil
}

func (p Profile) GetStreamProfile(sID streamcontrol.StreamIDFullyQualified) streamcontrol.StreamProfile {
	return p.PerStream[sID]
}

func getProfile(cfg *streamdconfig.Config, profileName streamcontrol.ProfileName) Profile {
	prof := Profile{
		ProfileMetadata: cfg.ProfileMetadata[profileName],
		Name:            profileName,
		PerStream:       map[streamcontrol.StreamIDFullyQualified]streamcontrol.StreamProfile{},
	}
	for platID, platCfg := range cfg.Backends {
		if platCfg == nil {
			continue
		}
		for accID, accRaw := range platCfg.Accounts {
			for sID, sProfs := range accRaw.GetStreamProfiles() {
				for name, sProf := range sProfs {
					if name != profileName {
						continue
					}
					fqID := streamcontrol.NewStreamIDFullyQualified(platID, accID, sID)
					prof.PerStream[fqID] = sProf
				}
			}
		}
	}
	return prof
}

func (p *Panel) rearrangeProfiles(ctx context.Context) error {
	var cfg *streamdconfig.Config
	p.configCacheLocker.Do(ctx, func() {
		cfg = p.configCache
	})

	if cfg == nil {
		return nil
	}

	curProfilesMap := map[streamcontrol.ProfileName]*Profile{}
	for platID, platCfg := range cfg.Backends {
		if platCfg == nil {
			continue
		}
		for accID, accRaw := range platCfg.Accounts {
			for sID, sProfs := range accRaw.GetStreamProfiles() {
				for profName, platProf := range sProfs {
					prof := curProfilesMap[profName]
					if prof == nil {
						prof = &Profile{
							Name:      profName,
							PerStream: map[streamcontrol.StreamIDFullyQualified]streamcontrol.StreamProfile{},
						}
						curProfilesMap[profName] = prof
					}
					fqID := streamcontrol.NewStreamIDFullyQualified(platID, accID, sID)
					prof.PerStream[fqID] = platProf
					prof.MaxOrder = xmath.Max(prof.MaxOrder, platProf.GetOrder())
				}
			}
		}
	}
	for profName := range cfg.ProfileMetadata {
		prof := curProfilesMap[profName]
		if prof == nil {
			prof = &Profile{
				Name:      profName,
				PerStream: map[streamcontrol.StreamIDFullyQualified]streamcontrol.StreamProfile{},
			}
			curProfilesMap[profName] = prof
		}
	}

	curProfiles := make([]Profile, 0, len(curProfilesMap))
	for idx, profile := range curProfilesMap {
		curProfiles = append(curProfiles, *profile)
		logger.Tracef(ctx, "rearrangeProfiles(): curProfiles[%s] = %#+v", idx, *profile)
	}

	sort.SliceStable(curProfiles, func(i, j int) bool {
		aa := curProfiles[i]
		ab := curProfiles[j]
		if aa.MaxOrder != ab.MaxOrder {
			return aa.MaxOrder < ab.MaxOrder
		}
		if aa.Name != ab.Name {
			return aa.Name < ab.Name
		}
		return false
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

	return nil
}

func (p *Panel) refilterProfiles(ctx context.Context) {
	if p.configCache == nil {
		return
	}
	if cap(p.profilesOrderFiltered) < len(p.profilesOrder) {
		p.profilesOrderFiltered = make([]streamcontrol.ProfileName, 0, len(p.profilesOrder)*2)
	} else {
		p.profilesOrderFiltered = p.profilesOrderFiltered[:0]
	}
	if p.filterValue == "" {
		p.profilesOrderFiltered = p.profilesOrderFiltered[:len(p.profilesOrder)]
		copy(p.profilesOrderFiltered, p.profilesOrder)
		logger.Tracef(
			ctx,
			"refilterProfiles(): profilesOrderFiltered <- p.profilesOrder: %#+v",
			p.profilesOrder,
		)
		logger.Tracef(ctx, "refilterProfiles(): p.profilesListWidget.Refresh()")
		if p.profilesListWidget != nil {
			p.app.Driver().DoFromGoroutine(func() {
				p.profilesListWidget.Refresh()
			}, false)
		}
		return
	}

	filterValue := strings.ToLower(p.filterValue)
	for _, profileName := range p.profilesOrder {
		titleMatch := strings.Contains(strings.ToLower(string(profileName)), filterValue)
		subValueMatch := false
		p.configCacheLocker.Do(ctx, func() {
			for platID, platCfg := range p.configCache.Backends {
				if platCfg == nil {
					continue
				}
				for _, accRaw := range platCfg.Accounts {
					for _, sProfs := range accRaw.GetStreamProfiles() {
						profRaw, ok := sProfs[profileName]
						if !ok {
							continue
						}

						switch platID {
						case twitch.ID:
							prof, err := streamcontrol.GetStreamProfile[twitch.StreamProfile](ctx, profRaw)
							if err != nil {
								continue
							}
							if containTagSubstringCI(prof.Tags[:], filterValue) {
								subValueMatch = true
							}
							if ptrStringMatchCI(prof.Language, filterValue) {
								subValueMatch = true
							}
						case kick.ID:
						case youtube.ID:
							prof, err := streamcontrol.GetStreamProfile[youtube.StreamProfile](ctx, profRaw)
							if err != nil {
								continue
							}
							if containTagSubstringCI(prof.Tags, filterValue) {
								subValueMatch = true
							}
						}
					}
				}
			}
		})

		if titleMatch || subValueMatch {
			logger.Tracef(
				ctx,
				"refilterProfiles(): profilesOrderFiltered[%3d] = %s",
				len(p.profilesOrderFiltered),
				profileName,
			)
			p.profilesOrderFiltered = append(p.profilesOrderFiltered, profileName)
		}
	}

	logger.Tracef(ctx, "refilterProfiles(): p.profilesListWidget.Refresh()")
	if p.profilesListWidget != nil {
		p.app.Driver().DoFromGoroutine(func() {
			p.profilesListWidget.Refresh()
		}, false)
	}
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
	ctx := context.TODO()
	w := obj.(*widget.Label)

	profileName := streamcontrol.ProfileName(p.profilesOrderFiltered[itemID])
	var profile Profile
	p.configCacheLocker.Do(ctx, func() {
		profile = getProfile(p.configCache, profileName)
	})

	w.SetText(string(profile.Name))
}

func ptrCopy[T any](v T) *T {
	return &v
}

func (p *Panel) onProfilesListSelect(
	id widget.ListItemID,
) {
	if id < 0 || id >= len(p.profilesOrderFiltered) {
		return
	}
	ctx := context.TODO()
	p.setupStreamButton.Enable()

	profileName := p.profilesOrderFiltered[id]
	var profile Profile
	p.configCacheLocker.Do(ctx, func() {
		profile = getProfile(p.configCache, profileName)
	})
	p.selectedProfileName = ptrCopy(profileName)
	p.streamTitleField.SetText(profile.DefaultStreamTitle)
	p.streamTitleLabel.SetText(profile.DefaultStreamTitle)
	p.streamDescriptionField.SetText(profile.DefaultStreamDescription)
	p.streamDescriptionLabel.SetText(profile.DefaultStreamDescription)

	p.selectedStreams = make(map[streamcontrol.StreamIDFullyQualified]bool)
	for streamID := range profile.PerStream {
		p.selectedStreams[streamID] = true
	}
	p.updateStreamsSelectButtonLabel()
	p.syncSelectedStreamsToDaemon(ctx)
}

func (p *Panel) onProfilesListUnselect(
	_ widget.ListItemID,
) {
	p.selectedProfileName = nil
	p.setupStreamButton.Disable()
	p.streamTitleField.SetText("")
	p.streamDescriptionField.SetText("")
}

func (p *Panel) setFilter(ctx context.Context, filter string) {
	p.filterValue = filter
	p.refilterProfiles(ctx)
}

func (p *Panel) getSelectedProfile() Profile {
	if p.selectedProfileName == nil {
		return Profile{}
	}
	ctx := context.TODO()
	return xsync.DoA2R1(ctx, &p.configCacheLocker, getProfile, p.configCache, *p.selectedProfileName)
}

func (p *Panel) newGlobalStreamsSelectUI(
	ctx context.Context,
	w fyne.Window,
	backendEnabled map[streamcontrol.PlatformID]bool,
	backendData map[streamcontrol.PlatformID]any,
	perStream map[streamcontrol.StreamIDFullyQualified]streamcontrol.StreamProfile,
) (fyne.CanvasObject, func() (map[streamcontrol.StreamIDFullyQualified]streamcontrol.StreamProfile, error)) {
	platforms := []streamcontrol.PlatformID{
		obs.ID,
		twitch.ID,
		kick.ID,
		youtube.ID,
	}

	// Collect all available streams across all platforms
	allAvailableStreams := make(map[streamcontrol.StreamIDFullyQualified]string)
	for _, platID := range platforms {
		if !backendEnabled[platID] {
			continue
		}
		accounts, err := p.StreamD.GetAccounts(ctx, platID)
		if err != nil {
			continue
		}
		for _, accID := range accounts {
			streams, err := p.StreamD.GetStreams(ctx, accID)
			if err != nil {
				continue
			}
			if len(streams) == 0 {
				streams = []streamcontrol.StreamInfo{{ID: streamcontrol.DefaultStreamID}}
			}
			for _, stream := range streams {
				sID := streamcontrol.StreamIDFullyQualified{
					AccountIDFullyQualified: accID,
					StreamID:                stream.ID,
				}
				label := fmt.Sprintf("[%s] %s", strings.ToUpper(string(platID)), accID.AccountID)
				if stream.Name != "" {
					label += " - " + stream.Name
				} else if stream.ID != streamcontrol.DefaultStreamID {
					label += " - " + string(stream.ID)
				}
				allAvailableStreams[sID] = label
			}
		}
	}

	if len(perStream) == 0 {
		for sID := range allAvailableStreams {
			var prof streamcontrol.StreamProfile
			switch sID.PlatformID {
			case obs.ID:
				prof = &obs.StreamProfile{}
			case twitch.ID:
				prof = &twitch.StreamProfile{}
			case kick.ID:
				prof = &kick.StreamProfile{}
			case youtube.ID:
				prof = &youtube.StreamProfile{}
			default:
				prof = streamcontrol.RawMessage("{}")
			}
			perStream[sID] = prof
		}
	}

	streamsList := container.NewVBox()
	var refreshStreams func()

	addStreamSelect := widget.NewSelect(nil, nil)
	addStreamSelect.PlaceHolder = "Add a stream for any platform..."

	updateAddSelect := func() {
		var options []string
		for sID, label := range allAvailableStreams {
			if _, ok := perStream[sID]; ok {
				continue
			}
			options = append(options, label)
		}
		sort.Strings(options)
		addStreamSelect.Options = options
		addStreamSelect.Refresh()
	}

	addStreamSelect.OnChanged = func(label string) {
		if label == "" {
			return
		}
		for sID, l := range allAvailableStreams {
			if l == label {
				var prof streamcontrol.StreamProfile
				switch sID.PlatformID {
				case obs.ID:
					prof = &obs.StreamProfile{}
				case twitch.ID:
					prof = &twitch.StreamProfile{}
				case kick.ID:
					prof = &kick.StreamProfile{}
				case youtube.ID:
					prof = &youtube.StreamProfile{}
				default:
					prof = streamcontrol.RawMessage("{}")
				}
				perStream[sID] = prof
				break
			}
		}
		addStreamSelect.SetSelected("")
		refreshStreams()
	}

	type streamSaveResult struct {
		sID  streamcontrol.StreamIDFullyQualified
		prof streamcontrol.StreamProfile
	}
	var saveFuncs []func() (streamSaveResult, error)

	refreshStreams = func() {
		streamsList.RemoveAll()
		saveFuncs = nil

		var sortedIDs []streamcontrol.StreamIDFullyQualified
		for sID := range perStream {
			sortedIDs = append(sortedIDs, sID)
		}

		sort.Slice(sortedIDs, func(i, j int) bool {
			return allAvailableStreams[sortedIDs[i]] < allAvailableStreams[sortedIDs[j]]
		})

		for _, sID := range sortedIDs {
			sID := sID
			label := allAvailableStreams[sID]
			if label == "" {
				label = fmt.Sprintf("[%s] %v/%v", sID.PlatformID, sID.AccountID, sID.StreamID)
			}

			ui, ok := platformUIs[sID.PlatformID]
			if !ok {
				logger.Errorf(ctx, "no platform UI for platform ID '%s'", sID.PlatformID)
				continue
			}

			prof := perStream[sID]

			// Parent select logic
			var availableProfiles []string
			availableProfiles = append(availableProfiles, "<default>")
			p.configCacheLocker.Do(ctx, func() {
				if p.configCache == nil {
					return
				}
				platCfg := p.configCache.Backends[sID.PlatformID]
				if platCfg != nil {
					for _, acc := range platCfg.Accounts {
						for _, sProfs := range acc.GetStreamProfiles() {
							for profName := range sProfs {
								availableProfiles = append(availableProfiles, string(profName))
							}
						}
					}
				}
			})
			sort.Strings(availableProfiles[1:])
			// unique available profiles
			uniqueProfiles := make(map[string]struct{})
			var uniqueOptions []string
			for _, opt := range availableProfiles {
				if _, ok := uniqueProfiles[opt]; !ok {
					uniqueProfiles[opt] = struct{}{}
					uniqueOptions = append(uniqueOptions, opt)
				}
			}

			profileSelect := widget.NewSelect(uniqueOptions, nil)
			currentParent, hasParent := prof.GetParent()
			if !hasParent {
				profileSelect.SetSelected("<default>")
			} else {
				profileSelect.SetSelected(string(currentParent))
			}
			profileSelect.OnChanged = func(s string) {
				if s == "<default>" {
					s = ""
				}
				p.setStreamProfileParent(prof, streamcontrol.ProfileName(s))
			}

			removeButton := widget.NewButtonWithIcon("", theme.ContentRemoveIcon(), func() {
				delete(perStream, sID)
				refreshStreams()
			})

			streamUI, saveFunc := ui.RenderStream(
				ctx,
				p,
				w,
				sID.PlatformID,
				sID,
				backendData[sID.PlatformID],
				prof,
			)

			saveStreamConfig := func() (streamSaveResult, error) {
				config, err := saveFunc()
				if err != nil {
					return streamSaveResult{sID: sID}, err
				}
				return streamSaveResult{sID: sID, prof: config.(streamcontrol.StreamProfile)}, nil
			}
			saveFuncs = append(saveFuncs, saveStreamConfig)

			streamsList.Add(widget.NewRichTextFromMarkdown(fmt.Sprintf("## %s", label)))
			streamsList.Add(container.NewVBox(
				container.NewHBox(
					widget.NewLabel("Parent profile:"),
					profileSelect,
					removeButton,
				),
				streamUI,
			))
			streamsList.Add(widget.NewSeparator())
		}
		updateAddSelect()
		streamsList.Refresh()
	}

	refreshStreams()

	return container.NewVBox(
			addStreamSelect,
			streamsList,
		), func() (map[streamcontrol.StreamIDFullyQualified]streamcontrol.StreamProfile, error) {
			result := make(map[streamcontrol.StreamIDFullyQualified]streamcontrol.StreamProfile)
			for _, f := range saveFuncs {
				res, err := f()
				if err != nil {
					return nil, err
				}
				result[res.sID] = res.prof
			}
			return result, nil
		}
}

func (p *Panel) setStreamProfileParent(prof streamcontrol.StreamProfile, parent streamcontrol.ProfileName) {
	// This is a helper because StreamProfile doesn't have a SetParent method.
	switch v := prof.(type) {
	case *obs.StreamProfile:
		v.Parent = parent
	case *twitch.StreamProfile:
		v.Parent = parent
	case *kick.StreamProfile:
		v.Parent = parent
	case *youtube.StreamProfile:
		v.Parent = parent
	}
}
