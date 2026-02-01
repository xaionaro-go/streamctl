package streampanel

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/clock"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
)

var youtubeCredentialsCreateLink, _ = url.Parse(
	"https://console.cloud.google.com/apis/credentials/oauthclient",
)

type youtubeProfileUI struct{}

func init() {
	registerPlatformUI(youtube.ID, &youtubeProfileUI{})
}

func (ui *youtubeProfileUI) GetUserInfoItems(
	ctx context.Context,
	p *Panel,
	platID streamcontrol.PlatformID,
	accountID streamcontrol.AccountID,
	accountRaw []byte,
) ([]fyne.CanvasObject, func() ([]byte, error), error) {
	var cfg youtube.AccountConfig
	if len(accountRaw) > 0 {
		if err := yaml.Unmarshal(accountRaw, &cfg); err != nil {
			return nil, nil, fmt.Errorf("unable to unmarshal account config: %w", err)
		}
	}

	clientIDField := newClientIDField(cfg.ClientID)
	clientSecretField := newClientSecretField(cfg.ClientSecret.Get())
	instructionText := widget.NewRichText(
		&widget.TextSegment{Text: "Go to\n", Style: widget.RichTextStyle{Inline: true}},
		&widget.HyperlinkSegment{
			Text: youtubeCredentialsCreateLink.String(),
			URL:  youtubeCredentialsCreateLink,
		},
		&widget.TextSegment{
			Text:  `,` + "\n" + `configure "consent screen" (note: you may add yourself into Test Users to avoid problems further on, and don't forget to add "YouTube Data API v3" scopes) and go back to` + "\n",
			Style: widget.RichTextStyle{Inline: true},
		},
		&widget.HyperlinkSegment{
			Text: youtubeCredentialsCreateLink.String(),
			URL:  youtubeCredentialsCreateLink,
		},
		&widget.TextSegment{
			Text:  `,` + "\n" + `choose "Desktop app", confirm and copy&paste client ID and client secret.`,
			Style: widget.RichTextStyle{Inline: true},
		},
	)
	instructionText.Wrapping = fyne.TextWrapWord

	var allowlistedStreamIDs []streamcontrol.StreamID
	activeStreamsContainer := p.NewStreamManagementUI(ctx, platID, accountID, cfg.AllowlistedStreamIDs, func(newList []streamcontrol.StreamID) {
		allowlistedStreamIDs = newList
	})

	items := []fyne.CanvasObject{
		widget.NewLabel("YouTube client ID:"),
		clientIDField,
		widget.NewLabel("YouTube client secret:"),
		clientSecretField,
		instructionText,
		activeStreamsContainer,
	}

	saveFunc := func() ([]byte, error) {
		cfg.ClientID = clientIDField.Text
		cfg.ClientSecret.Set(clientSecretField.Text)
		if allowlistedStreamIDs != nil {
			cfg.AllowlistedStreamIDs = allowlistedStreamIDs
		}
		return yaml.Marshal(cfg)
	}

	return items, saveFunc, nil
}

func (ui *youtubeProfileUI) Placement() platformProfilePlacement {
	return platformProfilePlacementRight
}

func (ui *youtubeProfileUI) IsReadyToStart(ctx context.Context, p *Panel) bool {
	platStreamStatus, err := p.StreamD.GetStreamStatus(ctx, streamcontrol.StreamIDFullyQualified{
		AccountIDFullyQualified: streamcontrol.AccountIDFullyQualified{
			PlatformID: youtube.ID,
		},
	})
	if err != nil {
		logger.Errorf(ctx, "unable to get stream status from %s: %v", youtube.ID, err)
		return true
	}
	if d, ok := platStreamStatus.CustomData.(youtube.StreamStatusCustomData); ok {
		if len(d.UpcomingBroadcasts) != 0 {
			return true
		}
	}
	return false
}

func (ui *youtubeProfileUI) AfterStartStream(ctx context.Context, p *Panel) error {
	// I don't know why, but if we don't open the livestream control page on YouTube
	// in the browser, then the stream does not want to start.
	//
	// And here we wait until the hack with opening the page will complete.
	observability.Go(ctx, func(ctx context.Context) {
		waitFor := 15 * time.Second
		deadline := clock.Get().Now().Add(waitFor)

		p.streamMutex.Do(ctx, func() {
			defer func() {
				p.startStopButton.SetText(startStreamString())
				p.startStopButton.Icon = theme.MediaRecordIcon()
				p.startStopButton.Importance = widget.SuccessImportance
				p.startStopButton.Enable()
			}()
			p.startStopButton.Disable()
			p.startStopButton.Icon = theme.ViewRefreshIcon()
			p.startStopButton.Importance = widget.DangerImportance

			t := clock.Get().Ticker(100 * time.Millisecond)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					timeDiff := deadline.Sub(clock.Get().Now()).Truncate(100 * time.Millisecond)
					if timeDiff < 0 {
						return
					}
					p.startStopButton.SetText(fmt.Sprintf("%.1fs", timeDiff.Seconds()))
				}
			}
		})
	})
	return nil
}

func (ui *youtubeProfileUI) IsAlwaysChecked(ctx context.Context, p *Panel) bool {
	return false
}

func (ui *youtubeProfileUI) ShouldStopParallel() bool {
	return true
}

func (ui *youtubeProfileUI) AfterStopStream(ctx context.Context, p *Panel) error {
	return nil
}

func (ui *youtubeProfileUI) UpdateStatus(ctx context.Context, p *Panel) {
}

func (ui *youtubeProfileUI) GetColor() fyne.ThemeColorName {
	return theme.ColorNameHyperlink
}

func (ui *youtubeProfileUI) RenderStream(
	ctx context.Context,
	p *Panel,
	w fyne.Window,
	platID streamcontrol.PlatformID,
	sID streamcontrol.StreamIDFullyQualified,
	backendData any,
	streamConfig any,
) (fyne.CanvasObject, func() (any, error)) {
	dataYouTube := backendData.(api.BackendDataYouTube)

	var youtubeProfile youtube.StreamProfile
	if streamConfig != nil {
		_ = yaml.Unmarshal(streamcontrol.ToRawMessage(streamConfig), &youtubeProfile)
	}

	autoNumerateCheck := widget.NewCheck("Auto-numerate", func(b bool) {
		youtubeProfile.AutoNumerate = b
	})
	autoNumerateCheck.SetChecked(youtubeProfile.AutoNumerate)
	autoNumerateHint := NewHintWidget(
		w,
		"When enabled, it adds the number of the stream to the stream's title.\n\nFor example 'Watching presidential debate' -> 'Watching presidential debate [#52]'.",
	)

	var initialBroadcastName *string
	for _, bcID := range youtubeProfile.TemplateBroadcastIDs {
		for _, bc := range dataYouTube.Cache.Broadcasts {
			if bc.Id != bcID {
				continue
			}
			initialBroadcastName = &bc.Snippet.Title
		}
	}

	searchParams := searchSelectParams{
		ctx:         ctx,
		p:           p,
		placeholder: "youtube live recording template",
		onSearch: func(text string) []searchResult {
			var results []searchResult
			count := 0
			for _, bc := range dataYouTube.Cache.Broadcasts {
				if strings.Contains(cleanString(bc.Snippet.Title), text) {
					results = append(results, searchResult{
						ID:   bc.Id,
						Name: bc.Snippet.Title,
					})
					count++
					if count > 10 {
						break
					}
				}
			}
			return results
		},
		onSelected: func(id string, name string) {
			youtubeProfile.TemplateBroadcastIDs = []string{id}
		},
		initialName: initialBroadcastName,
		onClear: func() {
			youtubeProfile.TemplateBroadcastIDs = youtubeProfile.TemplateBroadcastIDs[:0]
		},
		observabilityG: observability.Go,
	}

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
	case youtube.UndefinedTemplateTags, youtube.TemplateTagsIgnore:
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

	templateTagsHint := NewHintWidget(
		w,
		"'ignore' will ignore the tags set in the template; 'use as primary' will put the tags of the template first and then add the profile tags; 'use as additional' will put the tags of the profile first and then add the template tags",
	)

	youtubeTagsEditor := newTagsEditor(youtubeProfile.Tags, 0, youtube.LimitTagsLength)

	content := container.NewVBox(
		container.NewHBox(autoNumerateCheck, autoNumerateHint),
		newSearchSelect(searchParams),
		container.NewHBox(templateTagsLabel, templateTags, templateTagsHint),
		widget.NewLabel("Tags:"),
		youtubeTagsEditor.CanvasObject,
	)

	return content, func() (any, error) {
		youtubeProfile.Tags = youtubeTagsEditor.GetTags()
		return youtubeProfile, nil
	}
}

func (ui *youtubeProfileUI) FilterMatch(
	platProfile streamcontrol.RawMessage,
	filterValue string,
) bool {
	var p youtube.StreamProfile
	if err := yaml.Unmarshal(platProfile, &p); err == nil {
		if containTagSubstringCI(p.Tags, filterValue) {
			return true
		}
	}
	return false
}
