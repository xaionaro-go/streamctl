package types

type Backend string

const (
	BackendUndefined = ""
	BackendLibVLC    = "libvlc"
	BackendMPV       = "mpv"
	BackendBuiltin   = "builtin"
)
