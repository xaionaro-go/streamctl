package streamcontrol

import (
	"time"
)

type Event struct {
	ID            EventID
	CreatedAt     time.Time
	ExpiresAt     *time.Time
	Type          EventType
	User          User
	TargetUser    *User
	TargetChannel *User
	Message       *Message
	Paid          *Money
	Tier          *Tier
}
