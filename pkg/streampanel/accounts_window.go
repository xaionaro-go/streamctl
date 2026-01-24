package streampanel

import (
	"context"
	"fmt"
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
)

func (p *Panel) OpenAccountManagementWindow(ctx context.Context) {
	w := p.app.NewWindow("Account Management")
	w.Resize(fyne.NewSize(800, 600))

	cfg, err := p.GetStreamDConfig(ctx)
	if err != nil {
		p.DisplayError(fmt.Errorf("unable to get the config: %w", err))
		return
	}

	content := container.NewVBox()

	var updateContent func()
	updateContent = func() {
		content.Objects = nil

		platforms := []streamcontrol.PlatformID{obs.ID, twitch.ID, kick.ID, youtube.ID}
		for _, platID := range platforms {
			platID := platID
			platCfg := cfg.Backends[platID]

			header := container.NewHBox(
				widget.NewLabel(fmt.Sprintf("Platform: %s", platID)),
				layout.NewSpacer(),
				widget.NewButtonWithIcon("Add account", theme.ContentAddIcon(), func() {
					p.openAddAccountDialogForPlatform(ctx, cfg, platID, func() {
						updateContent()
					})
				}),
			)
			content.Add(header)

			if platCfg != nil {
				accounts := make([]streamcontrol.AccountID, 0, len(platCfg.Accounts))
				for accID := range platCfg.Accounts {
					accounts = append(accounts, accID)
				}
				slices.Sort(accounts)

				for _, accID := range accounts {
					accID := accID
					accRaw := platCfg.Accounts[accID]

					accountRow := container.NewHBox(
						widget.NewLabel(string(accID)),
						layout.NewSpacer(),
						widget.NewButtonWithIcon("Edit", theme.SettingsIcon(), func() {
							p.openEditAccountDialog(ctx, cfg, platID, accID, accRaw, func() {
								updateContent()
							})
						}),
						widget.NewButtonWithIcon("Delete", theme.DeleteIcon(), func() {
							delete(platCfg.Accounts, accID)
							updateContent()
						}),
					)
					content.Add(container.NewPadded(accountRow))
				}
			}
			content.Add(widget.NewSeparator())
		}
		content.Refresh()
	}

	updateContent()

	saveButton := widget.NewButtonWithIcon("Save and Close", theme.DocumentSaveIcon(), func() {
		if err := p.SetStreamDConfig(ctx, cfg); err != nil {
			p.DisplayError(fmt.Errorf("unable to update the remote config: %w", err))
		} else {
			if err := p.StreamD.SaveConfig(ctx); err != nil {
				p.DisplayError(fmt.Errorf("unable to save the remote config: %w", err))
			}
			if err := p.StreamD.EXPERIMENTAL_ReinitStreamControllers(ctx); err != nil {
				p.DisplayError(fmt.Errorf("unable to reinit the stream controllers: %w", err))
			}
		}
		w.Close()
	})

	w.SetContent(container.NewBorder(
		widget.NewLabelWithStyle("Manage your platform accounts", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		saveButton,
		nil,
		nil,
		container.NewVScroll(content),
	))
	w.Show()
}

func (p *Panel) openAddAccountDialogForPlatform(
	ctx context.Context,
	cfg *config.Config,
	platID streamcontrol.PlatformID,
	onUpdate func(),
) {
	w := p.app.NewWindow(fmt.Sprintf("Add %s account", platID))
	w.Resize(fyne.NewSize(500, 400))

	accountIDEntry := widget.NewEntry()
	accountIDEntry.SetPlaceHolder("Account ID (e.g. 'myaccount1')")

	ui, ok := platformUIs[platID]
	if !ok {
		p.DisplayError(fmt.Errorf("no UI for platform %s", platID))
		return
	}

	items, save, err := ui.GetUserInfoItems(ctx, p, platID, "", nil)
	if err != nil {
		p.DisplayError(fmt.Errorf("unable to get the UI items: %w", err))
		return
	}

	addButton := widget.NewButtonWithIcon("Add account", theme.ContentAddIcon(), func() {
		if accountIDEntry.Text == "" {
			p.DisplayError(fmt.Errorf("please enter account ID"))
			return
		}
		newRaw, err := save()
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to save the account: %w", err))
			return
		}
		platCfg := cfg.Backends[platID]
		if platCfg == nil {
			streamcontrol.InitializeConfig(cfg.Backends, platID)
			platCfg = cfg.Backends[platID]
		}
		if platCfg.Accounts == nil {
			platCfg.Accounts = make(map[streamcontrol.AccountID]streamcontrol.RawMessage)
		}
		platCfg.Accounts[streamcontrol.AccountID(accountIDEntry.Text)] = newRaw
		onUpdate()
		w.Close()
	})

	w.SetContent(container.NewBorder(
		container.NewVBox(
			widget.NewLabel("Account ID:"),
			accountIDEntry,
		),
		addButton,
		nil,
		nil,
		container.NewVScroll(container.NewVBox(items...)),
	))
	w.Show()
}

func (p *Panel) openEditAccountDialog(
	ctx context.Context,
	cfg *config.Config,
	platID streamcontrol.PlatformID,
	accID streamcontrol.AccountID,
	accRaw streamcontrol.RawMessage,
	onUpdate func(),
) {
	w := p.app.NewWindow(fmt.Sprintf("Edit %s account: %s", platID, accID))
	w.Resize(fyne.NewSize(500, 400))

	ui, ok := platformUIs[platID]
	if !ok {
		p.DisplayError(fmt.Errorf("no UI for platform %s", platID))
		return
	}

	items, save, err := ui.GetUserInfoItems(ctx, p, platID, accID, accRaw)
	if err != nil {
		p.DisplayError(fmt.Errorf("unable to get the UI items: %w", err))
		return
	}

	saveButton := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() {
		newRaw, err := save()
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to save the account: %w", err))
			return
		}
		platCfg := cfg.Backends[platID]
		platCfg.Accounts[accID] = newRaw
		onUpdate()
		w.Close()
	})

	w.SetContent(container.NewBorder(
		widget.NewLabel(fmt.Sprintf("Platform: %s, Account: %s", platID, accID)),
		saveButton,
		nil,
		nil,
		container.NewVScroll(container.NewVBox(items...)),
	))
	w.Show()
}
