package streampanel

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/goccy/go-yaml"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/buildvars"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
)

var twitchAppsCreateLink = must(url.Parse("https://dev.twitch.tv/console/apps/create"))

type twitchProfileUI struct{}

func init() {
	registerPlatformUI(twitch.ID, &twitchProfileUI{})
}

func (ui *twitchProfileUI) GetUserInfoItems(
	ctx context.Context,
	p *Panel,
	platID streamcontrol.PlatformID,
	accountID streamcontrol.AccountID,
	accountRaw []byte,
) ([]fyne.CanvasObject, func() ([]byte, error), error) {
	var cfg twitch.AccountConfig
	if len(accountRaw) > 0 {
		if err := yaml.Unmarshal(accountRaw, &cfg); err != nil {
			return nil, nil, fmt.Errorf("unable to unmarshal account config: %w", err)
		}
	}

	clientSecretIsBuiltin := buildvars.TwitchClientID != "" && buildvars.TwitchClientSecret != ""

	channelField := widget.NewEntry()
	channelField.SetText(cfg.Channel)
	channelField.SetPlaceHolder(
		"channel ID (copy&paste it from the browser: https://www.twitch.tv/<the channel ID is here>)",
	)
	clientIDField := newClientIDField(cfg.ClientID)
	if clientSecretIsBuiltin {
		clientIDField.Hide()
	}
	clientSecretField := newClientSecretField(cfg.ClientSecret.Get())
	if clientSecretIsBuiltin {
		clientSecretField.Hide()
	}
	instructionText := widget.NewRichText(
		&widget.TextSegment{Text: "Go to\n", Style: widget.RichTextStyle{Inline: true}},
		&widget.HyperlinkSegment{Text: twitchAppsCreateLink.String(), URL: twitchAppsCreateLink},
		&widget.TextSegment{
			Text:  `,` + "\n" + `create an application (enter "http://localhost:8091/" as the "OAuth Redirect URLs" value), then click "Manage" then "New Secret", and copy&paste client ID and client secret.`,
			Style: widget.RichTextStyle{Inline: true},
		},
	)
	instructionText.Wrapping = fyne.TextWrapWord

	items := []fyne.CanvasObject{
		widget.NewLabel("Channel:"),
		channelField,
		widget.NewLabel("Client ID:"),
		clientIDField,
		widget.NewLabel("Client Secret:"),
		clientSecretField,
		instructionText,
	}

	saveFunc := func() ([]byte, error) {
		cfg.AuthType = "user"
		channelWords := strings.Split(strings.TrimRight(channelField.Text, "/"), "/")
		cfg.Channel = channelWords[len(channelWords)-1]
		cfg.ClientID = clientIDField.Text
		cfg.ClientSecret.Set(clientSecretField.Text)
		return yaml.Marshal(cfg)
	}

	return items, saveFunc, nil
}

func (ui *twitchProfileUI) Placement() platformProfilePlacement {
	return platformProfilePlacementRight
}

func (ui *twitchProfileUI) RenderStream(
	ctx context.Context,
	p *Panel,
	w fyne.Window,
	platID streamcontrol.PlatformID,
	sID streamcontrol.StreamIDFullyQualified,
	backendData any,
	streamConfig any,
) (fyne.CanvasObject, func() (any, error)) {
	dataTwitch := backendData.(api.BackendDataTwitch)

	var twitchProfile twitch.StreamProfile
	if streamConfig != nil {
		_ = yaml.Unmarshal(streamcontrol.ToRawMessage(streamConfig), &twitchProfile)
	}

	var initialCategoryName *string
	if twitchProfile.CategoryName != nil {
		initialCategoryName = twitchProfile.CategoryName
	}
	if twitchProfile.CategoryID != nil {
		catID := *twitchProfile.CategoryID
		for _, cat := range dataTwitch.Cache.Categories {
			if cat.ID == catID {
				initialCategoryName = &cat.Name
				break
			}
		}
	}

	params := searchSelectParams{
		ctx:         ctx,
		p:           p,
		placeholder: "twitch category",
		onSearch: func(text string) []searchResult {
			var results []searchResult
			count := 0
			for _, cat := range dataTwitch.Cache.Categories {
				if strings.Contains(cleanString(cat.Name), text) {
					results = append(results, searchResult{
						ID:   cat.ID,
						Name: cat.Name,
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
			catName := name
			twitchProfile.CategoryName = &catName
			if id != "" {
				twitchProfile.CategoryID = &id
			}
		},
		initialName: initialCategoryName,
		onClear: func() {
			twitchProfile.CategoryName = nil
			twitchProfile.CategoryID = nil
		},
		observabilityG: observability.Go,
	}

	twitchTagsEditor := newTagsEditor(twitchProfile.Tags[:], 10, 0)

	content := container.NewVBox(
		newSearchSelect(params),
		widget.NewLabel("Tags:"),
		twitchTagsEditor.CanvasObject,
	)

	return content, func() (any, error) {
		twitchTags := twitchTagsEditor.GetTags()
		for i := 0; i < len(twitchProfile.Tags); i++ {
			var v string
			if i < len(twitchTags) {
				v = twitchTags[i]
			} else {
				v = ""
			}
			twitchProfile.Tags[i] = v
		}
		return twitchProfile, nil
	}
}

func (ui *twitchProfileUI) IsReadyToStart(ctx context.Context, p *Panel) bool {
	return true
}

func (ui *twitchProfileUI) AfterStartStream(ctx context.Context, p *Panel) error {
	return nil
}

func (ui *twitchProfileUI) IsAlwaysChecked(ctx context.Context, p *Panel) bool {
	return false
}

func (ui *twitchProfileUI) ShouldStopParallel() bool {
	return true
}

func (ui *twitchProfileUI) AfterStopStream(ctx context.Context, p *Panel) error {
	return nil
}

func (ui *twitchProfileUI) UpdateStatus(ctx context.Context, p *Panel) {
}

func (ui *twitchProfileUI) GetColor() fyne.ThemeColorName {
	return theme.ColorNameHyperlink
}

func (ui *twitchProfileUI) FilterMatch(
	platProfile streamcontrol.RawMessage,
	filterValue string,
) bool {
	var p twitch.StreamProfile
	if err := yaml.Unmarshal(platProfile, &p); err == nil {
		if containTagSubstringCI(p.Tags[:], filterValue) {
			return true
		}
		if ptrStringMatchCI(p.Language, filterValue) {
			return true
		}
	}
	return false
}
