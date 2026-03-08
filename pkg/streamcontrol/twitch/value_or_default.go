package twitch

func valueOrDefault[T comparable](main, fallback T) T {
	var zeroValue T
	if main != zeroValue {
		return main
	}
	return fallback
}
