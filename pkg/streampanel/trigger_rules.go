package streampanel

import (
	"context"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/serializable"
	"github.com/xaionaro-go/serializable/registry"
	"github.com/xaionaro-go/streamctl/pkg/consts"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config/action"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config/event/eventquery"
)

type triggerRulesUI struct {
	CanvasObject fyne.CanvasObject
	panel        *Panel
}

func NewTriggerRulesUI(
	ctx context.Context,
	panel *Panel,
) *triggerRulesUI {
	ui := &triggerRulesUI{
		panel: panel,
	}

	button := widget.NewButtonWithIcon(
		"Setup trigger rules",
		theme.SettingsIcon(), func() {
			ui.openSetupWindow(ctx)
		},
	)
	ui.CanvasObject = container.NewVBox(
		button,
	)
	return ui
}

func (ui *triggerRulesUI) openSetupWindow(ctx context.Context) {
	w := ui.panel.app.NewWindow(consts.AppName + ": Setup trigger rules")
	resizeWindow(w, fyne.NewSize(1000, 1000))

	var refreshContent func() bool

	refreshContent = func() bool {
		triggerRules, err := ui.panel.StreamD.ListTriggerRules(ctx)
		if err != nil {
			ui.panel.DisplayError(err)
			return false
		}

		var objs []fyne.CanvasObject
		for idx, triggerRule := range triggerRules {
			objs = append(objs, container.NewHBox(
				widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
					ui.openAddOrEditSceneRuleWindow(
						ctx,
						"Edit trigger rule",
						*triggerRule,
						func(
							ctx context.Context,
							triggerRule *config.TriggerRule,
						) error {
							err := ui.panel.StreamD.UpdateTriggerRule(ctx, api.TriggerRuleID(idx), triggerRule)
							if err != nil {
								return err
							}
							observability.Go(ctx, func(ctx context.Context) { refreshContent() })
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
							err := ui.panel.StreamD.RemoveTriggerRule(ctx, api.TriggerRuleID(idx))
							if err != nil {
								ui.panel.DisplayError(err)
								return
							}
							observability.Go(ctx, func(ctx context.Context) { refreshContent() })
						},
						ui.panel.mainWindow,
					)
					cw.Show()
				}),
				widget.NewLabel(fmt.Sprintf("%v", triggerRule)),
			))
		}

		w.SetContent(container.NewBorder(
			nil,
			widget.NewButtonWithIcon("Add rule", theme.ContentAddIcon(), func() {
				ui.openAddOrEditSceneRuleWindow(
					ctx,
					"Add scene rule",
					config.TriggerRule{},
					func(
						ctx context.Context,
						triggerRule *config.TriggerRule,
					) error {
						_, err := ui.panel.StreamD.AddTriggerRule(ctx, triggerRule)
						if err != nil {
							return err
						}
						observability.Go(ctx, func(ctx context.Context) { refreshContent() })
						return nil
					},
				)
			}),
			nil,
			nil,
			container.NewVBox(
				objs...,
			),
		))
		return true
	}
	if !refreshContent() {
		w.Close()
		return
	}
	w.Show()
}

func (ui *triggerRulesUI) openAddOrEditSceneRuleWindow(
	ctx context.Context,
	title string,
	triggerRule config.TriggerRule,
	commitFn func(context.Context, *config.TriggerRule) error,
) {
	w := ui.panel.app.NewWindow(consts.AppName + ": " + title)
	resizeWindow(w, fyne.NewSize(1000, 1000))

	var triggerQueryTypeList []string
	_triggerQueryTypeList := serializable.ListTypeNames[eventquery.EventQuery]()
	triggerQueryValues := map[string]eventquery.EventQuery{}
	for _, typeName := range _triggerQueryTypeList {
		value, _ := serializable.NewByTypeName[eventquery.EventQuery](typeName)
		if isUIDisabled(value) {
			continue
		}
		triggerQueryValues[typeName] = value
		triggerQueryTypeList = append(triggerQueryTypeList, typeName)
	}
	if triggerRule.EventQuery == nil {
		triggerRule.EventQuery = triggerQueryValues[triggerQueryTypeList[0]]
	}
	triggerQueryValues[registry.ToTypeName(triggerRule.EventQuery)] = triggerRule.EventQuery

	var actionTypeList []string
	_actionTypeList := serializable.ListTypeNames[action.Action]()
	actionValues := map[string]action.Action{}
	for _, typeName := range _actionTypeList {
		value, _ := serializable.NewByTypeName[action.Action](typeName)
		if isUIDisabled(value) {
			continue
		}
		actionValues[typeName] = value
		actionTypeList = append(actionTypeList, typeName)
	}
	if triggerRule.Action == nil {
		triggerRule.Action = actionValues[actionTypeList[0]]
	}
	actionValues[registry.ToTypeName(triggerRule.Action)] = triggerRule.Action

	var refreshContent func()
	refreshContent = func() {
		triggerSelector := widget.NewSelect(triggerQueryTypeList, func(s string) {
			if s == registry.ToTypeName(triggerRule.EventQuery) {
				return
			}
			triggerRule.EventQuery = triggerQueryValues[s]
			refreshContent()
		})
		curEventQueryType := registry.ToTypeName(triggerRule.EventQuery)
		triggerSelector.SetSelected(curEventQueryType)
		logger.Debugf(ctx, "trigger: selector %v: cur: %v (%T)", triggerQueryTypeList, curEventQueryType, triggerRule.EventQuery)
		triggerFields := makeFieldsFor(triggerRule.EventQuery)

		actionSelector := widget.NewSelect(actionTypeList, func(s string) {
			if s == registry.ToTypeName(triggerRule.Action) {
				return
			}
			triggerRule.Action = actionValues[s]
			refreshContent()
		})
		curActionType := registry.ToTypeName(triggerRule.Action)
		actionSelector.SetSelected(curActionType)
		logger.Debugf(ctx, "action: selector %v: cur: %v (%T)", actionTypeList, curActionType, triggerRule.Action)
		actionFields := makeFieldsFor(triggerRule.Action)

		w.SetContent(container.NewBorder(
			nil,
			widget.NewButton("Save", func() {
				logger.Debugf(ctx, "triggerRule == %v", triggerRule)
				err := commitFn(ctx, &triggerRule)
				if err != nil {
					ui.panel.DisplayError(err)
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
