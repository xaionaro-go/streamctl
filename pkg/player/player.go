package player

type Player interface {
	OpenURL(link string) error
	EndChan() <-chan struct{}
	IsEnded() bool
	SetSpeed(speed float64) error
	SetPause(pause bool) error
	Stop() error
	Close() error
}
