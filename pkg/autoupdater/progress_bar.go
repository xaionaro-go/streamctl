package autoupdater

type ProgressBar interface {
	SetProgress(progress float64)
}
