package streamcontrol

import "time"

type StreamStatus struct {
	IsActive     bool
	ViewersCount *uint      `json:",omitempty"`
	StartedAt    *time.Time `json:",omitempty"`
	CustomData   any        `json:",omitempty"`
}
