package player

import "time"

type Player interface {
	OpenURL(link string) error
	EndChan() <-chan struct{}
	IsEnded() bool
	GetPosition() time.Duration
	GetLength() time.Duration
	SetSpeed(speed float64) error
	SetPause(pause bool) error
	Stop() error
	Close() error
}
