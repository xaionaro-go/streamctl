package streampanel

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/go-ng/xmath"
	"github.com/xaionaro-go/kickcom"
	"github.com/xaionaro-go/observability"
	gconsts "github.com/xaionaro-go/streamctl/pkg/consts"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	streamdconfig "github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/xsync"
)

type Profile struct {
	streamdconfig.ProfileMetadata
	Name        streamcontrol.ProfileName
	PerPlatform map[streamcontrol.PlatformName]streamcontrol.AbstractStreamProfile
}

func (p *Panel) editProfileWindow(ctx context.Context) fyne.Window {
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
					_, ok := platCfg.GetStreamProfile(profile.Name)
					if ok {
						found = true
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
	var (
		obsProfile     *obs.StreamProfile
		twitchProfile  *twitch.StreamProfile
		kickProfile    *kick.StreamProfile
		youtubeProfile *youtube.StreamProfile
	)

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

	commonTagsEditor := newTagsEditor(values.TopicTags, 0, 0)

	backendEnabled := map[streamcontrol.PlatformName]bool{}
	backendData := map[streamcontrol.PlatformName]any{}
	for _, backendID := range []streamcontrol.PlatformName{
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
	_ = backendData[obs.ID].(api.BackendDataOBS)
	dataTwitch := backendData[twitch.ID].(api.BackendDataTwitch)
	dataKick := backendData[kick.ID].(api.BackendDataKick)
	_ = dataKick // TODO: delete me!
	dataYouTube := backendData[youtube.ID].(api.BackendDataYouTube)

	var bottomContentLeft, bottomContentRight, bottomContentCommon []fyne.CanvasObject

	bottomContentLeft = append(bottomContentLeft, widget.NewLabel("Tags for LLM (to re-generate the title):"))
	bottomContentLeft = append(bottomContentLeft, commonTagsEditor.CanvasObject)
	bottomContentLeft = append(bottomContentLeft, widget.NewSeparator())
	bottomContentLeft = append(bottomContentLeft, widget.NewRichTextFromMarkdown("# OBS:"))
	if backendEnabled[obs.ID] {
		if platProfile := values.PerPlatform[obs.ID]; platProfile != nil {
			var err error
			obsProfile, err = streamcontrol.GetStreamProfile[obs.StreamProfile](ctx, platProfile)
			if err != nil {
				p.DisplayError(fmt.Errorf("unable to convert the stream profile: %w", err))
			}
		} else {
			obsProfile = &obs.StreamProfile{}
		}

		enableRecordingCheck := widget.NewCheck("Enable recording", func(b bool) {
			obsProfile.EnableRecording = b
		})
		enableRecordingCheck.SetChecked(obsProfile.EnableRecording)
		bottomContentLeft = append(bottomContentLeft, enableRecordingCheck)
	}

	var getTwitchTags func() []string
	bottomContentRight = append(bottomContentRight, widget.NewRichTextFromMarkdown("# Twitch:"))
	if backendEnabled[twitch.ID] {
		if platProfile := values.PerPlatform[twitch.ID]; platProfile != nil {
			var err error
			twitchProfile, err = streamcontrol.GetStreamProfile[twitch.StreamProfile](ctx, platProfile)
			if err != nil {
				p.DisplayError(fmt.Errorf("unable to convert the stream profile: %w", err))
			}
		} else {
			twitchProfile = &twitch.StreamProfile{}
		}

		twitchCategory := widget.NewEntry()
		twitchCategory.SetPlaceHolder("twitch category")

		selectTwitchCategoryBox := container.NewHBox()
		bottomContentRight = append(bottomContentRight, selectTwitchCategoryBox)
		twitchCategory.OnChanged = func(text string) {
			selectTwitchCategoryBox.RemoveAll()
			if text == "" {
				return
			}
			text = cleanTwitchCategoryName(text)
			count := 0
			for _, cat := range dataTwitch.Cache.Categories {
				if strings.Contains(cleanTwitchCategoryName(cat.Name), text) {
					selectedTwitchCategoryContainer := container.NewHBox()
					catName := cat.Name
					tagContainerRemoveButton := widget.NewButtonWithIcon(
						catName,
						theme.ContentAddIcon(),
						func() {
							twitchCategory.OnSubmitted(catName)
						},
					)
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
		bottomContentRight = append(bottomContentRight, selectedTwitchCategoryBox)

		setSelectedTwitchCategory := func(catName string) {
			selectedTwitchCategoryBox.RemoveAll()
			selectedTwitchCategoryContainer := container.NewHBox()
			tagContainerRemoveButton := widget.NewButtonWithIcon(
				catName,
				theme.ContentClearIcon(),
				func() {
					selectedTwitchCategoryBox.Remove(selectedTwitchCategoryContainer)
					twitchProfile.CategoryName = nil
				},
			)
			selectedTwitchCategoryContainer.Add(tagContainerRemoveButton)
			selectedTwitchCategoryBox.Add(selectedTwitchCategoryContainer)
			twitchProfile.CategoryName = &catName
		}

		if twitchProfile.CategoryName != nil {
			setSelectedTwitchCategory(*twitchProfile.CategoryName)
		}
		if twitchProfile.CategoryID != nil {
			catID := *twitchProfile.CategoryID
			for _, cat := range dataTwitch.Cache.Categories {
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
			for _, cat := range dataTwitch.Cache.Categories {
				if cleanTwitchCategoryName(cat.Name) == text {
					setSelectedTwitchCategory(cat.Name)
					observability.Go(ctx, func(ctx context.Context) {
						time.Sleep(100 * time.Millisecond)
						twitchCategory.SetText("")
					})
					return
				}
			}
		}
		bottomContentRight = append(bottomContentRight, twitchCategory)

		twitchTagsEditor := newTagsEditor(twitchProfile.Tags[:], 10, 0)
		bottomContentRight = append(bottomContentRight, widget.NewLabel("Tags:"))
		bottomContentRight = append(bottomContentRight, twitchTagsEditor.CanvasObject)
		getTwitchTags = twitchTagsEditor.GetTags
	} else {
		bottomContentRight = append(bottomContentRight, widget.NewLabel("Twitch is disabled"))
	}

	bottomContentRight = append(bottomContentRight, widget.NewSeparator())
	bottomContentRight = append(bottomContentRight, widget.NewRichTextFromMarkdown("# Kick:"))
	if backendEnabled[kick.ID] {
		if platProfile := values.PerPlatform[kick.ID]; platProfile != nil {
			var err error
			kickProfile, err = streamcontrol.GetStreamProfile[kick.StreamProfile](ctx, platProfile)
			if err != nil {
				p.DisplayError(fmt.Errorf("unable to convert the stream profile: %w", err))
			}
		} else {
			kickProfile = &kick.StreamProfile{}
		}

		kickCategories := dataKick.Cache.GetCategories()
		catN := map[string]kickcom.CategoryV1Short{}
		catI := map[uint64]kickcom.CategoryV1Short{}
		for _, cat := range kickCategories {
			catN[cleanKickCategoryName(cat.Name)] = cat
			catI[cat.ID] = cat
		}

		kickCategory := widget.NewEntry()
		kickCategory.SetPlaceHolder("kick category")

		selectKickCategoryBox := container.NewHBox()
		bottomContentRight = append(bottomContentRight, selectKickCategoryBox)
		kickCategory.OnChanged = func(text string) {
			selectKickCategoryBox.RemoveAll()
			if text == "" {
				return
			}
			text = cleanKickCategoryName(text)
			count := 0
			for _, cat := range kickCategories {
				if strings.Contains(cleanKickCategoryName(cat.Name), text) {
					selectedKickCategoryContainer := container.NewHBox()
					catName := cat.Name
					tagContainerRemoveButton := widget.NewButtonWithIcon(
						catName,
						theme.ContentAddIcon(),
						func() {
							kickCategory.OnSubmitted(catName)
						},
					)
					selectedKickCategoryContainer.Add(tagContainerRemoveButton)
					selectKickCategoryBox.Add(selectedKickCategoryContainer)
					count++
					if count > 10 {
						break
					}
				}
			}
		}

		selectedKickCategoryBox := container.NewHBox()
		bottomContentRight = append(bottomContentRight, selectedKickCategoryBox)

		setSelectedKickCategory := func(catID uint64) {
			selectedKickCategoryBox.RemoveAll()
			selectedKickCategoryContainer := container.NewHBox()
			tagContainerRemoveButton := widget.NewButtonWithIcon(
				catI[catID].Name,
				theme.ContentClearIcon(),
				func() {
					selectedKickCategoryBox.Remove(selectedKickCategoryContainer)
					kickProfile.CategoryID = nil
				},
			)
			selectedKickCategoryContainer.Add(tagContainerRemoveButton)
			selectedKickCategoryBox.Add(selectedKickCategoryContainer)
			kickProfile.CategoryID = &catID
		}

		if kickProfile.CategoryID != nil {
			setSelectedKickCategory(*kickProfile.CategoryID)
		}

		kickCategory.OnSubmitted = func(text string) {
			if text == "" {
				return
			}
			text = cleanKickCategoryName(text)
			cat := catN[text]
			setSelectedKickCategory(cat.ID)
			observability.Go(ctx, func(ctx context.Context) {
				time.Sleep(100 * time.Millisecond)
				kickCategory.SetText("")
			})
		}
		bottomContentRight = append(bottomContentRight, kickCategory)
	} else {
		bottomContentRight = append(bottomContentRight, widget.NewLabel("Kick is disabled"))
	}

	var getYoutubeTags func() []string
	bottomContentRight = append(bottomContentRight, widget.NewSeparator())
	bottomContentRight = append(bottomContentRight, widget.NewRichTextFromMarkdown("# YouTube:"))
	if backendEnabled[youtube.ID] {
		if platProfile := values.PerPlatform[youtube.ID]; platProfile != nil {
			var err error
			youtubeProfile, err = streamcontrol.GetStreamProfile[youtube.StreamProfile](ctx, platProfile)
			if err != nil {
				p.DisplayError(fmt.Errorf("unable to convert the stream profile: %w", err))
			}
		} else {
			youtubeProfile = &youtube.StreamProfile{}
		}

		autoNumerateCheck := widget.NewCheck("Auto-numerate", func(b bool) {
			youtubeProfile.AutoNumerate = b
		})
		autoNumerateCheck.SetChecked(youtubeProfile.AutoNumerate)
		autoNumerateHint := NewHintWidget(
			w,
			"When enabled, it adds the number of the stream to the stream's title.\n\nFor example 'Watching presidential debate' -> 'Watching presidential debate [#52]'.",
		)
		bottomContentRight = append(
			bottomContentRight,
			container.NewHBox(autoNumerateCheck, autoNumerateHint),
		)

		youtubeTemplate := widget.NewEntry()
		youtubeTemplate.SetPlaceHolder("youtube live recording template")

		selectYoutubeTemplateBox := container.NewHBox()
		bottomContentRight = append(bottomContentRight, selectYoutubeTemplateBox)
		youtubeTemplate.OnChanged = func(text string) {
			selectYoutubeTemplateBox.RemoveAll()
			if text == "" {
				return
			}
			text = cleanYoutubeRecordingName(text)
			count := 0
			for _, bc := range dataYouTube.Cache.Broadcasts {
				if strings.Contains(cleanYoutubeRecordingName(bc.Snippet.Title), text) {
					selectedYoutubeRecordingsContainer := container.NewHBox()
					recName := bc.Snippet.Title
					tagContainerRemoveButton := widget.NewButtonWithIcon(
						recName,
						theme.ContentAddIcon(),
						func() {
							youtubeTemplate.OnSubmitted(recName)
						},
					)
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
		bottomContentRight = append(bottomContentRight, selectedYoutubeBroadcastBox)

		setSelectedYoutubeBroadcast := func(bc *youtube.LiveBroadcast) {
			selectedYoutubeBroadcastBox.RemoveAll()
			selectedYoutubeBroadcastContainer := container.NewHBox()
			recName := bc.Snippet.Title
			tagContainerRemoveButton := widget.NewButtonWithIcon(
				recName,
				theme.ContentClearIcon(),
				func() {
					selectedYoutubeBroadcastBox.Remove(selectedYoutubeBroadcastContainer)
					youtubeProfile.TemplateBroadcastIDs = youtubeProfile.TemplateBroadcastIDs[:0]
				},
			)
			selectedYoutubeBroadcastContainer.Add(tagContainerRemoveButton)
			selectedYoutubeBroadcastBox.Add(selectedYoutubeBroadcastContainer)
			youtubeProfile.TemplateBroadcastIDs = []string{bc.Id}
		}

		for _, bcID := range youtubeProfile.TemplateBroadcastIDs {
			for _, bc := range dataYouTube.Cache.Broadcasts {
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
			for _, bc := range dataYouTube.Cache.Broadcasts {
				if cleanYoutubeRecordingName(bc.Snippet.Title) == text {
					setSelectedYoutubeBroadcast(bc)
					observability.Go(ctx, func(ctx context.Context) {
						time.Sleep(100 * time.Millisecond)
						youtubeTemplate.SetText("")
					})
					return
				}
			}
		}
		bottomContentRight = append(bottomContentRight, youtubeTemplate)

		templateTagsLabel := widget.NewLabel("Template tags:")
		templateTags := widget.NewSelect(
			[]string{"ignore", "use as primary", "use as additional"},
			func(s string) {
				switch s {
				case "ignore":
					youtubeProfile.TemplateTags = youtube.TemplateTagsIgnore
				case "use as primary":
					youtubeProfile.TemplateTags = youtube.TemplateTagsUseAsPrimary
				case "use as additional":
					youtubeProfile.TemplateTags = youtube.TemplateTagsUseAsAdditional
				default:
					p.DisplayError(fmt.Errorf("unexpected new value of 'template tags': '%s'", s))
				}
			},
		)
		switch youtubeProfile.TemplateTags {
		case youtube.TemplateTagsUndefined, youtube.TemplateTagsIgnore:
			templateTags.SetSelected("ignore")
		case youtube.TemplateTagsUseAsPrimary:
			templateTags.SetSelected("use as primary")
		case youtube.TemplateTagsUseAsAdditional:
			templateTags.SetSelected("use as additional")
		default:
			p.DisplayError(
				fmt.Errorf(
					"unexpected current value of 'template tags': '%s'",
					youtubeProfile.TemplateTags,
				),
			)
		}
		templateTags.SetSelected(youtubeProfile.TemplateTags.String())
		templateTagsHint := NewHintWidget(
			w,
			"'ignore' will ignore the tags set in the template; 'use as primary' will put the tags of the template first and then add the profile tags; 'use as additional' will put the tags of the profile first and then add the template tags",
		)
		bottomContentRight = append(
			bottomContentRight,
			container.NewHBox(templateTagsLabel, templateTags, templateTagsHint),
		)

		youtubeTagsEditor := newTagsEditor(youtubeProfile.Tags, 0, youtube.LimitTagsLength)
		bottomContentRight = append(bottomContentRight, widget.NewLabel("Tags:"))
		bottomContentRight = append(bottomContentRight, youtubeTagsEditor.CanvasObject)
		getYoutubeTags = youtubeTagsEditor.GetTags
	} else {
		bottomContentRight = append(bottomContentRight, widget.NewLabel("YouTube is disabled"))
	}

	bottomContentCommon = append(bottomContentCommon,
		widget.NewButton("Save", func() {
			profile := Profile{
				Name:        streamcontrol.ProfileName(profileName.Text),
				PerPlatform: map[streamcontrol.PlatformName]streamcontrol.AbstractStreamProfile{},
				ProfileMetadata: streamdconfig.ProfileMetadata{
					DefaultStreamTitle:       defaultStreamTitle.Text,
					DefaultStreamDescription: defaultStreamDescription.Text,
					MaxOrder:                 0,
				},
			}
			if obsProfile != nil {
				profile.PerPlatform[obs.ID] = obsProfile
			}

			sanitizeTags := func(in []string) []string {
				out := make([]string, 0, len(in))
				for _, k := range in {
					if k == "" {
						continue
					}
					out = append(out, k)
				}
				return out
			}
			profile.TopicTags = sanitizeTags(commonTagsEditor.GetTags())
			if twitchProfile != nil {
				if getTwitchTags != nil {
					twitchTags := sanitizeTags(getTwitchTags())
					for i := 0; i < len(twitchProfile.Tags); i++ {
						var v string
						if i < len(twitchTags) {
							v = twitchTags[i]
						} else {
							v = ""
						}
						twitchProfile.Tags[i] = v
					}
				}
				profile.PerPlatform[twitch.ID] = twitchProfile
			}
			if kickProfile != nil {
				profile.PerPlatform[kick.ID] = kickProfile
			}
			if youtubeProfile != nil {
				if getYoutubeTags != nil {
					youtubeProfile.Tags = sanitizeTags(getYoutubeTags())
				}
				profile.PerPlatform[youtube.ID] = youtubeProfile
				if len(youtubeProfile.TemplateBroadcastIDs) == 0 {
					p.DisplayError(fmt.Errorf("no youtube template stream is selected"))
					return
				}
			}
			err := commitFn(ctx, profile)
			if err != nil {
				p.DisplayError(err)
				return
			}
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
				container.NewBorder(
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
				),
				container.NewVBox(
					bottomContentRight...,
				),
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
		return fmt.Errorf("unable to get config: %w", err)
	}

	logger.Debugf(ctx, "profileCreateOrUpdate(%s)", profile.Name)
	for platformName, platformProfile := range profile.PerPlatform {
		if platformProfile == nil {
			continue
		}
		cfg.Backends[platformName].StreamProfiles[profile.Name] = platformProfile
		logger.Debugf(
			ctx,
			"profileCreateOrUpdate(%s): cfg.Backends[%s].StreamProfiles[%s] = %#+v",
			profile.Name,
			platformName,
			profile.Name,
			platformProfile,
		)
	}
	cfg.ProfileMetadata[profile.Name] = profile.ProfileMetadata

	logger.Debugf(
		ctx,
		"profileCreateOrUpdate(%s): cfg.Backends == %#+v",
		profile.Name,
		cfg.Backends,
	)

	err = p.SetStreamDConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("unable to set config: %w", err)
	}

	if err := p.rearrangeProfiles(ctx); err != nil {
		return fmt.Errorf("unable to re-arrange profiles: %w", err)
	}

	if err := p.StreamD.SaveConfig(ctx); err != nil {
		return fmt.Errorf("unable to save the profile: %w", err)
	}
	return nil
}

func (p *Panel) profileDelete(ctx context.Context, profileName streamcontrol.ProfileName) error {
	cfg, err := p.GetStreamDConfig(ctx)
	if err != nil {
		return fmt.Errorf("unable to get config: %w", err)
	}

	logger.Debugf(ctx, "onProfileDeleted(%s)", profileName)
	for platformName := range cfg.Backends {
		delete(cfg.Backends[platformName].StreamProfiles, profileName)
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

func getProfile(cfg *streamdconfig.Config, profileName streamcontrol.ProfileName) Profile {
	prof := Profile{
		ProfileMetadata: cfg.ProfileMetadata[profileName],
		Name:            profileName,
		PerPlatform:     map[streamcontrol.PlatformName]streamcontrol.AbstractStreamProfile{},
	}
	for platName, platCfg := range cfg.Backends {
		platProf, ok := platCfg.GetStreamProfile(profileName)
		if !ok {
			continue
		}
		prof.PerPlatform[platName] = platProf
	}
	return prof
}

func (p *Panel) rearrangeProfiles(ctx context.Context) error {
	var cfg *streamdconfig.Config
	p.configCacheLocker.Do(ctx, func() {
		cfg = p.configCache
	})

	curProfilesMap := map[streamcontrol.ProfileName]*Profile{}
	for platName, platCfg := range cfg.Backends {
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
		p.profilesListWidget.Refresh()
		return
	}

	filterValue := strings.ToLower(p.filterValue)
	for _, profileName := range p.profilesOrder {
		titleMatch := strings.Contains(strings.ToLower(string(profileName)), filterValue)
		subValueMatch := false
		p.configCacheLocker.Do(ctx, func() {
			for _, platCfg := range p.configCache.Backends {
				prof, ok := platCfg.GetStreamProfile(profileName)
				if !ok {
					continue
				}

				switch prof := prof.(type) {
				case twitch.StreamProfile:
					if containTagSubstringCI(prof.Tags[:], filterValue) {
						subValueMatch = true
						break
					}
					if ptrStringMatchCI(prof.Language, filterValue) {
						subValueMatch = true
						break
					}
				case kick.StreamProfile:
				case youtube.StreamProfile:
					if containTagSubstringCI(prof.Tags, filterValue) {
						subValueMatch = true
						break
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
	p.profilesListWidget.Refresh()
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
	ctx := context.TODO()
	p.setupStreamButton.Enable()

	profileName := p.profilesOrder[id]
	var profile Profile
	p.configCacheLocker.Do(ctx, func() {
		profile = getProfile(p.configCache, profileName)
	})
	p.selectedProfileName = ptrCopy(profileName)
	p.streamTitleField.SetText(profile.DefaultStreamTitle)
	p.streamTitleLabel.SetText(profile.DefaultStreamTitle)
	p.streamDescriptionField.SetText(profile.DefaultStreamDescription)
	p.streamDescriptionLabel.SetText(profile.DefaultStreamDescription)
}

func (p *Panel) onProfilesListUnselect(
	_ widget.ListItemID,
) {
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
