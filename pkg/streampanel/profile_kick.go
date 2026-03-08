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
	"github.com/xaionaro-go/kickcom"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/buildvars"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
)

var kickAppsCreateLink = must(url.Parse("https://kick.com/settings/developer?action=create"))

type kickProfileUI struct{}

func init() {
	registerPlatformUI(kick.ID, &kickProfileUI{})
}

func (ui *kickProfileUI) GetUserInfoItems(
	ctx context.Context,
	p *Panel,
	platID streamcontrol.PlatformID,
	accountID streamcontrol.AccountID,
	accountRaw []byte,
) ([]fyne.CanvasObject, func() ([]byte, error), error) {
	var cfg kick.AccountConfig
	if len(accountRaw) > 0 {
		if err := yaml.Unmarshal(accountRaw, &cfg); err != nil {
			return nil, nil, fmt.Errorf("unable to unmarshal account config: %w", err)
		}
	}

	clientSecretIsBuiltin := buildvars.KickClientID != "" && buildvars.KickClientSecret != ""

	channelField := widget.NewEntry()
	channelField.SetPlaceHolder(
		"channel ID (copy&paste it from the browser: https://www.kick.com/<the channel ID is here>)",
	)
	channelField.SetText(cfg.Channel)
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
		&widget.HyperlinkSegment{Text: kickAppsCreateLink.String(), URL: kickAppsCreateLink},
		&widget.TextSegment{
			Text:  `,` + "\n" + `click "Create new", enter app name and description, enter "http://localhost:8092/" as the RedirectURL, allow all permissions, click "Create App" and copy&paste client ID and client secret.`,
			Style: widget.RichTextStyle{Inline: true},
		},
	)
	instructionText.Wrapping = fyne.TextWrapWord

	items := []fyne.CanvasObject{
		widget.NewLabel("Kick channel:"),
		channelField,
		widget.NewLabel("Kick client ID:"),
		clientIDField,
		widget.NewLabel("Kick client secret:"),
		clientSecretField,
		instructionText,
	}

	saveFunc := func() ([]byte, error) {
		channelWords := strings.Split(channelField.Text, "/")
		cfg.Channel = channelWords[len(channelWords)-1]
		cfg.ClientID = clientIDField.Text
		cfg.ClientSecret.Set(clientSecretField.Text)
		return yaml.Marshal(cfg)
	}

	return items, saveFunc, nil
}

func (ui *kickProfileUI) Placement() platformProfilePlacement {
	return platformProfilePlacementRight
}

func (ui *kickProfileUI) RenderStream(
	ctx context.Context,
	p *Panel,
	w fyne.Window,
	platID streamcontrol.PlatformID,
	sID streamcontrol.StreamIDFullyQualified,
	backendData any,
	streamConfig any,
) (fyne.CanvasObject, func() (any, error)) {
	dataKick := backendData.(api.BackendDataKick)

	var kickProfile kick.StreamProfile
	if streamConfig != nil {
		_ = yaml.Unmarshal(streamcontrol.ToRawMessage(streamConfig), &kickProfile)
	}

	kickCategories := dataKick.Cache.GetCategories()
	catN := map[string]kickcom.CategoryV1Short{}
	catI := map[uint64]kickcom.CategoryV1Short{}
	for _, cat := range kickCategories {
		catN[cleanString(cat.Name)] = cat
		catI[cat.ID] = cat
	}

	var initialCategoryName *string
	if kickProfile.CategoryID != nil {
		if cat, ok := catI[*kickProfile.CategoryID]; ok {
			initialCategoryName = &cat.Name
		}
	}

	searchParams := searchSelectParams{
		ctx:         ctx,
		p:           p,
		placeholder: "kick category",
		onSearch: func(text string) []searchResult {
			var results []searchResult
			count := 0
			for _, cat := range kickCategories {
				if strings.Contains(cleanString(cat.Name), text) {
					results = append(results, searchResult{
						ID:   fmt.Sprintf("%d", cat.ID),
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
			if id == "" {
				return
			}
			var catID uint64
			fmt.Sscanf(id, "%d", &catID)
			kickProfile.CategoryID = &catID
		},
		initialName: initialCategoryName,
		onClear: func() {
			kickProfile.CategoryID = nil
		},
		observabilityG: observability.Go,
	}

	content := container.NewVBox(
		newSearchSelect(searchParams),
	)

	return content, func() (any, error) {
		return kickProfile, nil
	}
}

func (ui *kickProfileUI) IsReadyToStart(ctx context.Context, p *Panel) bool {
	return true
}

func (ui *kickProfileUI) AfterStartStream(ctx context.Context, p *Panel) error {
	return nil
}

func (ui *kickProfileUI) IsAlwaysChecked(ctx context.Context, p *Panel) bool {
	return false
}

func (ui *kickProfileUI) ShouldStopParallel() bool {
	return true
}

func (ui *kickProfileUI) AfterStopStream(ctx context.Context, p *Panel) error {
	return nil
}

func (ui *kickProfileUI) UpdateStatus(ctx context.Context, p *Panel) {
}

func (ui *kickProfileUI) GetColor() fyne.ThemeColorName {
	return theme.ColorNameSuccess
}

func (ui *kickProfileUI) FilterMatch(
	platProfile streamcontrol.RawMessage,
	filterValue string,
) bool {
	return false
}
