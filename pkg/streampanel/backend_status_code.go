package streampanel

type BackendStatusCode uint

const (
	BackendStatusCodeUndefined = BackendStatusCode(iota)
	BackendStatusCodeReady
	BackendStatusCodeNotNow
	BackendStatusCodeDisable
)
