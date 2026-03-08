package streamcontrol

import (
	"encoding/json"
	"fmt"
)

type TextFormatType int

const (
	UndefinedTextFormatType = TextFormatType(iota)
	TextFormatTypePlain
	TextFormatTypeMarkdown
	TextFormatTypeHTML
	endOfTextFormatType
)

func (t TextFormatType) String() string {
	switch t {
	case UndefinedTextFormatType:
		return "undefined"
	case TextFormatTypePlain:
		return "plain"
	case TextFormatTypeMarkdown:
		return "markdown"
	case TextFormatTypeHTML:
		return "html"
	default:
		return fmt.Sprintf("unknown_%d", int(t))
	}
}

func (t TextFormatType) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.String())
}

func ParseTextFormatType(s string) TextFormatType {
	for c := range endOfTextFormatType {
		if c.String() == s {
			return c
		}
	}
	return UndefinedTextFormatType
}

func TextFormatTypeFromString(s string) (TextFormatType, error) {
	v := ParseTextFormatType(s)
	if v == UndefinedTextFormatType && s != "undefined" {
		return UndefinedTextFormatType, fmt.Errorf("unknown text format type: '%v'", s)
	}
	return v, nil
}

func (t *TextFormatType) UnmarshalJSON(data []byte) error {
	if len(data) < 2 || data[0] != '"' || data[len(data)-1] != '"' {
		return fmt.Errorf("invalid JSON string: %s", data)
	}
	var s string
	err := json.Unmarshal(data, &s)
	if err != nil {
		return fmt.Errorf("unable to unmarshal JSON string: %w", err)
	}
	v, err := TextFormatTypeFromString(s)
	if err != nil {
		return fmt.Errorf("unable to convert string '%s' to TextFormatType: %w", s, err)
	}
	*t = v
	return nil
}
