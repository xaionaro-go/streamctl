package streampanel

import (
	"context"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs/types/action"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs/types/registry"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs/types/trigger"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
)

func (p *Panel) setStreamDConfig(
	ctx context.Context,
	cfg *config.Config,
) error {
	if err := p.StreamD.SetConfig(ctx, cfg); err != nil {
		return fmt.Errorf("unable to set the config: %w", err)
	}
	if err := p.StreamD.SaveConfig(ctx); err != nil {
		return fmt.Errorf("unable to save the config: %w", err)
	}
	return nil
}

func (p *Panel) openSetupOBSSceneRulesWindow(
	ctx context.Context,
	sceneName obs.SceneName,
) {
	w := p.app.NewWindow(AppName + ": Setup scene rules")
	resizeWindow(w, fyne.NewSize(1000, 1000))

	var refreshContent func()

	refreshContent = func() {
		sceneRules, err := p.StreamD.ListOBSSceneRules(ctx, sceneName)
		if err != nil {
			p.DisplayError(err)
			return
		}

		var objs []fyne.CanvasObject
		for idx, sceneRule := range sceneRules {
			objs = append(objs, container.NewHBox(
				widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
					p.openAddOrEditSceneRuleWindow(
						ctx,
						"Edit scene rule",
						sceneRule,
						func(
							ctx context.Context,
							sceneRule obs.SceneRule,
						) error {
							p.StreamD.UpdateOBSSceneRule(ctx, sceneName, uint64(idx), sceneRule)
							observability.Go(ctx, refreshContent)
							return nil
						},
					)
				}),
				widget.NewButtonWithIcon("", theme.ContentRemoveIcon(), func() {
					cw := dialog.NewConfirm(
						"Delete scene rule",
						"Are you sure you want to delete the stream rule?",
						func(b bool) {
							if !b {
								return
							}
							p.StreamD.RemoveOBSSceneRule(ctx, sceneName, uint64(idx))
							observability.Go(ctx, refreshContent)
						},
						p.mainWindow,
					)
					cw.Show()
				}),
			))
		}

		w.SetContent(container.NewBorder(
			nil,
			widget.NewButtonWithIcon("Add rule", theme.ContentAddIcon(), func() {
				p.openAddOrEditSceneRuleWindow(
					ctx,
					"Add scene rule",
					obs.SceneRule{},
					func(
						ctx context.Context,
						sceneRule obs.SceneRule,
					) error {
						p.StreamD.AddOBSSceneRule(ctx, sceneName, sceneRule)
						observability.Go(ctx, refreshContent)
						return nil
					},
				)
			}),
			nil,
			nil,
			objs...,
		))
	}
	refreshContent()
	w.Show()
}

func (p *Panel) openAddOrEditSceneRuleWindow(
	ctx context.Context,
	title string,
	sceneRule obs.SceneRule,
	commitFn func(context.Context, obs.SceneRule) error,
) {
	w := p.app.NewWindow(AppName + ": " + title)
	resizeWindow(w, fyne.NewSize(1000, 1000))

	triggerQueryTypeList := trigger.ListQueryTypeNames()
	triggerQueryValues := map[string]trigger.Query{}
	for _, typeName := range triggerQueryTypeList {
		triggerQueryValues[typeName] = trigger.NewByTypeName(typeName)
	}
	if sceneRule.TriggerQuery == nil {
		sceneRule.TriggerQuery = triggerQueryValues[triggerQueryTypeList[0]]
	}
	triggerQueryValues[registry.ToTypeName(sceneRule.TriggerQuery)] = sceneRule.TriggerQuery

	actionTypeList := action.ListTypeNames()
	actionValues := map[string]action.Action{}
	for _, typeName := range actionTypeList {
		actionValues[typeName] = action.NewByTypeName(typeName)
	}
	if sceneRule.Action == nil {
		sceneRule.Action = actionValues[actionTypeList[0]]
	}
	actionValues[registry.ToTypeName(sceneRule.Action)] = sceneRule.Action

	var refreshContent func()
	refreshContent = func() {
		triggerSelector := widget.NewSelect(triggerQueryTypeList, func(s string) {
			if s == registry.ToTypeName(sceneRule.TriggerQuery) {
				return
			}
			sceneRule.TriggerQuery = triggerQueryValues[s]
			refreshContent()
		})
		triggerSelector.SetSelected(registry.ToTypeName(sceneRule.TriggerQuery))
		triggerFields := makeFieldsFor(sceneRule.TriggerQuery)

		actionSelector := widget.NewSelect(actionTypeList, func(s string) {
			if s == registry.ToTypeName(sceneRule.Action) {
				return
			}
			sceneRule.Action = actionValues[s]
			refreshContent()
		})
		actionSelector.SetSelected(registry.ToTypeName(sceneRule.Action))
		actionFields := makeFieldsFor(sceneRule.Action)

		w.SetContent(container.NewBorder(
			nil,
			widget.NewButton("Save", func() {
				err := commitFn(ctx, sceneRule)
				if err != nil {
					p.DisplayError(err)
					return
				}
				w.Close()
			}),
			nil,
			nil,
			container.NewVBox(
				widget.NewLabel("Trigger:"),
				triggerSelector,
				container.NewVBox(triggerFields...),
				widget.NewLabel("Action:"),
				actionSelector,
				container.NewVBox(actionFields...),
			),
		))
	}

	refreshContent()
	w.Show()
}
