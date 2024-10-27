package autoupdater

type ErrNoUpdates struct{}

func (ErrNoUpdates) Error() string {
	return "no updates"
}

type ErrNoAsset struct{}

func (ErrNoAsset) Error() string {
	return "the asset is not found"
}
