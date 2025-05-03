package streampanel

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	separatorTags = ','
)

type tagsEditor struct {
	fyne.CanvasObject
	TagsString string
	TagsLimit  uint
}

func newTagsEditor(
	tags []string,
	tagsLimit uint,
) *tagsEditor {
	textField := widget.NewMultiLineEntry()
	r := &tagsEditor{
		TagsString: strings.Join(tags, string(separatorTags)+" "),
		TagsLimit:  tagsLimit,
	}
	textField.SetText(r.TagsString)
	countLabel := widget.NewLabel("")
	deduplicateButton := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
		tags, _ := r.getTags()
		textField.SetText(strings.Join(tags, string(separatorTags)+" ") + string(separatorTags) + " ")
	})
	deduplicateButton.OnTapped()
	deduplicateButton.Hide()
	r.CanvasObject = container.NewVBox(
		textField,
		container.NewHBox(countLabel, deduplicateButton),
	)
	textField.OnChanged = func(s string) {
		r.TagsString = s
		tags, dups := r.getTags()
		tagsCount := uint(len(tags))
		var appendString string
		if len(dups) > 0 {
			appendString = fmt.Sprintf("; duplicates: %d", len(dups))
			deduplicateButton.Show()
		} else {
			deduplicateButton.Hide()
		}
		switch {
		case len(dups) > 0 || (r.TagsLimit > 0 && tagsCount > r.TagsLimit):
			countLabel.Importance = widget.DangerImportance
		case r.TagsLimit > 0 && tagsCount == r.TagsLimit:
			countLabel.Importance = widget.WarningImportance
		default:
			countLabel.Importance = widget.MediumImportance
		}
		if r.TagsLimit == 0 {
			countLabel.SetText(fmt.Sprintf("tags count: %d%s", tagsCount, appendString))
			return
		}
		countLabel.SetText(fmt.Sprintf("tags count: %d/%d%s", tagsCount, r.TagsLimit, appendString))
	}
	textField.OnSubmitted = textField.OnChanged
	textField.OnSubmitted(textField.Text)
	return r
}

func (t *tagsEditor) getTags() ([]string, []string) {
	var tags, dups []string
	isSet := map[string]struct{}{}
	for _, tag := range strings.Split(t.TagsString, string(separatorTags)) {
		tag = strings.Trim(tag, " ")
		if tag == "" {
			continue
		}
		if _, ok := isSet[tag]; ok {
			dups = append(dups, tag)
			continue
		}
		isSet[tag] = struct{}{}
		tags = append(tags, tag)
	}
	return tags, dups

}

func (t *tagsEditor) GetTags() []string {
	tags, _ := t.getTags()
	return tags
}
