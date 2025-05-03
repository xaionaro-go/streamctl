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
	TagsString      string
	TagsLimit       uint
	CharactersLimit uint
}

func newTagsEditor(
	tags []string,
	tagsLimit uint,
	charactersLimit uint,
) *tagsEditor {
	textField := widget.NewMultiLineEntry()
	r := &tagsEditor{
		TagsString:      strings.Join(tags, string(separatorTags)+" "),
		TagsLimit:       tagsLimit,
		CharactersLimit: charactersLimit,
	}
	textField.SetText(r.TagsString)
	statusLabel := widget.NewLabel("")
	deduplicateButton := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
		tags, _ := r.getTags()
		s := strings.Join(tags, string(separatorTags)+" ")
		if len(tags) > 0 {
			s += string(separatorTags) + " "
		}
		textField.SetText(s)
	})
	deduplicateButton.OnTapped()
	deduplicateButton.Hide()
	r.CanvasObject = container.NewVBox(
		textField,
		container.NewHBox(statusLabel, deduplicateButton),
	)
	textField.OnChanged = func(s string) {
		r.TagsString = s
		tags, dups := r.getTags()
		tagsCount := uint(len(tags))
		charLen := uint(0)
		for _, tag := range tags {
			charLen += uint(len(tag))
		}
		var parts []string
		switch {
		case false ||
			len(dups) > 0 ||
			(r.TagsLimit > 0 && tagsCount > r.TagsLimit) ||
			(r.CharactersLimit > 0 && charLen > r.CharactersLimit):
			statusLabel.Importance = widget.DangerImportance
		case false ||
			(r.TagsLimit > 0 && tagsCount == r.TagsLimit) ||
			(r.CharactersLimit > 0 && charLen == r.CharactersLimit):
			statusLabel.Importance = widget.WarningImportance
		default:
			statusLabel.Importance = widget.MediumImportance
		}
		if r.TagsLimit == 0 {
			parts = append(parts, fmt.Sprintf("tags count: %d", tagsCount))
		} else {
			parts = append(parts, fmt.Sprintf("tags count: %d/%d", tagsCount, r.TagsLimit))
		}
		if r.CharactersLimit > 0 {
			parts = append(parts, fmt.Sprintf("char count: %d/%d", charLen, r.CharactersLimit))
		}
		if len(dups) > 0 {
			parts = append(parts, fmt.Sprintf("duplicates: %d", len(dups)))
			deduplicateButton.Show()
		} else {
			deduplicateButton.Hide()
		}
		statusLabel.SetText(strings.Join(parts, string(separatorTags)+" "))
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
