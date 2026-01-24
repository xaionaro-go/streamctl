package streampanel

type BackendStatusCode uint

const (
	UndefinedBackendStatusCode = BackendStatusCode(iota)
	BackendStatusCodeReady
	BackendStatusCodeNotNow
	BackendStatusCodeDisable
)
