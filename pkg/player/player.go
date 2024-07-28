package player

import "time"

type Player interface {
	ProcessTitle() string
	OpenURL(link string) error
	GetLink() string
	EndChan() <-chan struct{}
	IsEnded() bool
	GetPosition() time.Duration
	GetLength() time.Duration
	SetSpeed(speed float64) error
	SetPause(pause bool) error
	Stop() error
	Close() error
}

type PlayerCommon struct {
	Title string
}

func (p PlayerCommon) ProcessTitle() string {
	return p.Title
}
