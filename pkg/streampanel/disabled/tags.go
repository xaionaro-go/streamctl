package streampanel

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func newTagsEditor(
	initialTags []string,
	tagCountLimit uint,
	additionalButtons ...tagEditButton,
) *tagsEditorSection {
	t := &tagsEditorSection{}
	tagsEntryField := widget.NewEntry()
	tagsEntryField.SetPlaceHolder("add a tag")
	s := tagsEntryField.Size()
	s.Width = 200
	tagsMap := map[string]struct{}{}
	tagsEntryField.Resize(s)
	tagsControlsContainer := container.NewHBox()
	t.tagsContainer = container.NewGridWrap(fyne.NewSize(200, 30))
	selectedTags := map[string]struct{}{}
	selectedTagsOrdered := func() []tagInfo {
		var result []tagInfo
		for _, tag := range t.Tags {
			if _, ok := selectedTags[tag.Tag]; ok {
				result = append(result, tag)
			}
		}
		return result
	}

	tagContainerToFirstButton := widget.NewButtonWithIcon("", theme.MediaFastRewindIcon(), func() {
		for _, tag := range selectedTagsOrdered() {
			idx := t.getIdx(tag)
			if idx < 1 {
				return
			}
			t.move(idx, 0)
		}
	})
	tagsControlsContainer.Add(tagContainerToFirstButton)

	tagContainerToPrevButton := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
		for _, tag := range selectedTagsOrdered() {
			idx := t.getIdx(tag)
			if idx < 1 {
				return
			}
			t.move(idx, idx-1)
		}
	})
	tagsControlsContainer.Add(tagContainerToPrevButton)
	tagContainerToNextButton := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() {
		for _, tag := range reverse(selectedTagsOrdered()) {
			idx := t.getIdx(tag)
			if idx >= len(t.Tags)-1 {
				return
			}
			t.move(idx, idx+2)
		}
	})
	tagsControlsContainer.Add(tagContainerToNextButton)
	tagContainerToLastButton := widget.NewButtonWithIcon("", theme.MediaFastForwardIcon(), func() {
		for _, tag := range reverse(selectedTagsOrdered()) {
			idx := t.getIdx(tag)
			if idx >= len(t.Tags)-1 {
				return
			}
			t.move(idx, len(t.Tags))
		}
	})
	tagsControlsContainer.Add(tagContainerToLastButton)

	removeTag := func(tag string) {
		tagInfo := t.getTagInfo(tag)
		t.tagsContainer.Remove(tagInfo.Container)
		delete(tagsMap, tag)
		for idx, tagCmp := range t.Tags {
			if tagCmp.Tag == tag {
				t.Tags = append(t.Tags[:idx], t.Tags[idx+1:]...)
				break
			}
		}
	}

	tagsControlsContainer.Add(widget.NewSeparator())
	tagsControlsContainer.Add(widget.NewSeparator())
	tagsControlsContainer.Add(widget.NewSeparator())
	tagContainerRemoveButton := widget.NewButtonWithIcon("", theme.ContentClearIcon(), func() {
		for tag := range selectedTags {
			removeTag(tag)
		}
	})
	tagsControlsContainer.Add(tagContainerRemoveButton)

	tagsControlsContainer.Add(widget.NewSeparator())
	tagsControlsContainer.Add(widget.NewSeparator())
	tagsControlsContainer.Add(widget.NewSeparator())
	for _, additionalButtonInfo := range additionalButtons {
		button := widget.NewButtonWithIcon(
			additionalButtonInfo.Label,
			additionalButtonInfo.Icon,
			func() {
				additionalButtonInfo.Callback(t, selectedTagsOrdered())
			},
		)
		tagsControlsContainer.Add(button)
	}

	addTag := func(tagName string) {
		if tagCountLimit > 0 && len(t.Tags) >= int(tagCountLimit) {
			removeTag(t.Tags[tagCountLimit-1].Tag)
		}
		tagName = strings.Trim(tagName, " ")
		if tagName == "" {
			return
		}
		if _, ok := tagsMap[tagName]; ok {
			return
		}

		tagsMap[tagName] = struct{}{}
		tagContainer := container.NewHBox()
		t.Tags = append(t.Tags, tagInfo{
			Tag:       tagName,
			Container: tagContainer,
		})

		tagLabel := tagName
		overflown := false
		for {
			size := fyne.MeasureText(
				tagLabel,
				fyne.CurrentApp().Settings().Theme().Size("text"),
				fyne.TextStyle{},
			)
			if size.Width < 100 {
				break
			}
			tagLabel = tagLabel[:len(tagLabel)-1]
			overflown = true
		}
		if overflown {
			tagLabel += "…"
		}
		tagSelector := widget.NewCheck(tagLabel, func(b bool) {
			if b {
				selectedTags[tagName] = struct{}{}
			} else {
				delete(selectedTags, tagName)
			}
		})
		tagContainer.Add(tagSelector)
		t.tagsContainer.Add(tagContainer)
	}
	tagsEntryField.OnSubmitted = func(text string) {
		for _, tag := range strings.Split(text, ",") {
			addTag(tag)
		}
		tagsEntryField.SetText("")
	}

	for _, tag := range initialTags {
		addTag(tag)
	}
	t.CanvasObject = container.NewVBox(
		t.tagsContainer,
		tagsControlsContainer,
		tagsEntryField,
	)
	return t
}

type tagEditButton struct {
	Icon     fyne.Resource
	Label    string
	Callback func(*tagsEditorSection, []tagInfo)
}

type tagInfo struct {
	Tag       string
	Container fyne.CanvasObject
}

type tagsEditorSection struct {
	fyne.CanvasObject
	tagsContainer *fyne.Container
	Tags          []tagInfo
}

func (t *tagsEditorSection) getTagInfo(tag string) tagInfo {
	for _, tagCmp := range t.Tags {
		if tagCmp.Tag == tag {
			return tagCmp
		}
	}
	return tagInfo{}
}

func (t *tagsEditorSection) getIdx(tag tagInfo) int {
	for idx, tagCmp := range t.Tags {
		if tagCmp.Tag == tag.Tag {
			return idx
		}
	}

	return -1
}

func (t *tagsEditorSection) move(srcIdx, dstIdx int) {
	newTags := make([]tagInfo, 0, len(t.Tags))
	newObjs := make([]fyne.CanvasObject, 0, len(t.Tags))

	objs := t.tagsContainer.Objects
	for i := range t.Tags {
		if i == dstIdx {
			newTags = append(newTags, t.Tags[srcIdx])
			newObjs = append(newObjs, objs[srcIdx])
		}
		if i == srcIdx {
			continue
		}
		newTags = append(newTags, t.Tags[i])
		newObjs = append(newObjs, objs[i])
	}
	if dstIdx >= len(t.Tags) {
		newTags = append(newTags, t.Tags[srcIdx])
		newObjs = append(newObjs, objs[srcIdx])
	}

	t.Tags = newTags
	t.tagsContainer.Objects = newObjs
	t.tagsContainer.Refresh()
}

func (t *tagsEditorSection) GetTags() []string {
	result := make([]string, 0, len(t.Tags))
	for _, tag := range t.Tags {
		result = append(result, tag.Tag)
	}
	return result
}
