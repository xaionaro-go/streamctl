package streampanel

import (
	"context"
	"fmt"
	"sort"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

func (p *Panel) openAddAccountDialog(
	ctx context.Context,
	cfg streamcontrol.Config,
	onUpdate func(streamcontrol.PlatformID, bool),
) {
	w := p.app.NewWindow("Add account")
	w.Resize(fyne.NewSize(600, 400))

	platNames := streamcontrol.GetPlatformIDs()
	sort.Slice(platNames, func(i, j int) bool {
		return platNames[i] < platNames[j]
	})
	var platNamesStr []string
	for _, name := range platNames {
		platNamesStr = append(platNamesStr, string(name))
	}

	platformSelect := widget.NewSelect(platNamesStr, nil)
	platformSelect.PlaceHolder = "Select platform"
	accountIDEntry := widget.NewEntry()
	accountIDEntry.SetPlaceHolder("Account ID (e.g. 'myaccount1')")

	fieldsContainer := container.NewVBox()

	var saveFunc func() error

	platformSelect.OnChanged = func(platIDStr string) {
		platID := streamcontrol.PlatformID(platIDStr)
		fieldsContainer.Objects = nil
		saveFunc = nil

		ui, ok := platformUIs[platID]
		if !ok {
			return
		}

		items, save, err := ui.GetUserInfoItems(ctx, p, platID, streamcontrol.AccountID(accountIDEntry.Text), nil)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to get the UI items: %w", err))
			return
		}
		fieldsContainer.Objects = items
		saveFunc = func() error {
			newRaw, err := save()
			if err != nil {
				return err
			}
			platCfg := cfg[platID]
			if platCfg == nil {
				streamcontrol.InitializeConfig(cfg, platID)
				platCfg = cfg[platID]
			}
			accID := streamcontrol.AccountID(accountIDEntry.Text)
			if platCfg.Accounts == nil {
				platCfg.Accounts = make(map[streamcontrol.AccountID]streamcontrol.RawMessage)
			}
			platCfg.Accounts[accID] = newRaw
			return nil
		}
		fieldsContainer.Refresh()
	}

	addButton := widget.NewButtonWithIcon("Add account", theme.ContentAddIcon(), func() {
		fmt.Printf("DEBUG: Add account button clicked, saveFunc is nil: %v, accountID: %q\n", saveFunc == nil, accountIDEntry.Text)
		if saveFunc == nil {
			p.DisplayError(fmt.Errorf("please select a platform first"))
			return
		}
		if accountIDEntry.Text == "" {
			p.DisplayError(fmt.Errorf("please enter account ID"))
			return
		}
		if err := saveFunc(); err != nil {
			fmt.Printf("DEBUG: saveFunc error: %v\n", err)
			p.DisplayError(err)
			return
		}
		fmt.Printf("DEBUG: Closing Add account window\n")
		go onUpdate(streamcontrol.PlatformID(platformSelect.Selected), true)
		w.Close()
	})

	w.SetContent(container.NewBorder(
		container.NewVBox(
			widget.NewLabel("Platform:"),
			platformSelect,
			widget.NewLabel("Account ID:"),
			accountIDEntry,
		),
		addButton,
		nil,
		nil,
		container.NewVScroll(fieldsContainer),
	))
	w.Show()
}

func (p *Panel) initBackendConfig(
	cfg streamcontrol.Config,
	platID streamcontrol.PlatformID,
) error {
	streamcontrol.InitializeConfig(cfg, platID)
	return nil
}
