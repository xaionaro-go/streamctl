package twitch

func ptr[T any](in T) *T {
	return &in
}
