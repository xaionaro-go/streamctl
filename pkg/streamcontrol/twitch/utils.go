package twitch

func ptr[T any](in T) *T {
	return &in
}

func valueOrDefault[T comparable](main, fallback T) T {
	var zeroValue T
	if main != zeroValue {
		return main
	}
	return fallback
}
