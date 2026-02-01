package streampanel

import (
	"context"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func cleanString(in string) string {
	return strings.ToLower(strings.TrimSpace(in))
}

func newClientIDField(text string) *widget.Entry {
	e := widget.NewEntry()
	e.SetPlaceHolder("client ID")
	e.SetText(text)
	return e
}

func newClientSecretField(text string) *widget.Entry {
	e := widget.NewEntry()
	e.SetPlaceHolder("client secret")
	e.SetText(text)
	return e
}

type searchSelectParams struct {
	ctx            context.Context
	p              *Panel
	placeholder    string
	onSearch       func(text string) []searchResult
	onSelected     func(id string, name string)
	initialName    *string
	onClear        func()
	observabilityG func(context.Context, func(context.Context))
}

type searchResult struct {
	ID   string
	Name string
}

func newSearchSelect(
	params searchSelectParams,
) fyne.CanvasObject {
	searchEntry := widget.NewEntry()
	searchEntry.SetPlaceHolder(params.placeholder)

	resultsBox := container.NewHBox()
	selectedBox := container.NewHBox()

	setSelected := func(id string, name string) {
		selectedBox.RemoveAll()
		selectedContainer := container.NewHBox()
		tagContainerRemoveButton := widget.NewButtonWithIcon(
			name,
			theme.ContentClearIcon(),
			func() {
				selectedBox.Remove(selectedContainer)
				if params.onClear != nil {
					params.onClear()
				}
			},
		)
		selectedContainer.Add(tagContainerRemoveButton)
		selectedBox.Add(selectedContainer)
		selectedBox.Refresh()
		params.onSelected(id, name)
	}

	if params.initialName != nil {
		setSelected("", *params.initialName)
	}

	searchEntry.OnChanged = func(text string) {
		resultsBox.RemoveAll()
		if text == "" {
			return
		}
		text = cleanString(text)
		results := params.onSearch(text)
		for _, res := range results {
			res := res
			tagContainerAddButton := widget.NewButtonWithIcon(
				res.Name,
				theme.ContentAddIcon(),
				func() {
					searchEntry.OnSubmitted(res.Name)
				},
			)
			resultsBox.Add(tagContainerAddButton)
		}
		resultsBox.Refresh()
	}

	searchEntry.OnSubmitted = func(text string) {
		if text == "" {
			return
		}
		text = cleanString(text)
		results := params.onSearch(text)
		for _, res := range results {
			if cleanString(res.Name) == text {
				setSelected(res.ID, res.Name)
				params.observabilityG(params.ctx, func(ctx context.Context) {
					time.Sleep(100 * time.Millisecond)
					searchEntry.SetText("")
				})
				return
			}
		}
	}

	return container.NewVBox(
		resultsBox,
		selectedBox,
		searchEntry,
	)
}
