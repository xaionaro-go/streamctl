package streampanel

import (
	"github.com/xaionaro-go/streamctl/pkg/clock"
)

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
	clientIDField := widget.NewEntry()
	clientIDField.SetPlaceHolder("client ID")
	clientIDField.SetText(cfg.ClientID)
	if clientSecretIsBuiltin {
		clientIDField.Hide()
	}
	clientSecretField := widget.NewEntry()
	clientSecretField.SetPlaceHolder("client secret")
	clientSecretField.SetText(cfg.ClientSecret.Get())
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
		catN[cleanKickCategoryName(cat.Name)] = cat
		catI[cat.ID] = cat
	}

	kickCategory := widget.NewEntry()
	kickCategory.SetPlaceHolder("kick category")

	selectKickCategoryBox := container.NewHBox()
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
		selectKickCategoryBox.Refresh()
	}

	selectedKickCategoryBox := container.NewHBox()

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
		selectedKickCategoryBox.Refresh()
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
		setSelectedKickCategory(uint64(cat.ID))
		observability.Go(ctx, func(ctx context.Context) {
			clock.Get().Sleep(100 * time.Millisecond)
			kickCategory.SetText("")
		})
	}

	content := container.NewVBox(
		selectKickCategoryBox,
		selectedKickCategoryBox,
		kickCategory,
	)

	return content, func() (any, error) {
		return kickProfile, nil
	}
}

func cleanKickCategoryName(in string) string {
	return strings.ToLower(strings.Trim(in, " "))
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
