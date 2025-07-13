package event

import (
	"encoding/json"
	"fmt"
)

type Event interface {
	fmt.Stringer
	Get() Event
	Match(Event) bool
}

func tryJSON(value any) []byte {
	b, _ := json.Marshal(value)
	return b
}
