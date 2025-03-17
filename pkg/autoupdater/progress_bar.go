package autoupdater

type ProgressBar interface {
	SetProgress(progress float64)
}

type DummyProgressBar struct{}

var _ ProgressBar = (*DummyProgressBar)(nil)

func (DummyProgressBar) SetProgress(float64) {}
