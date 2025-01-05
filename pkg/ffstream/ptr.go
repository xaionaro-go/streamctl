package ffstream

func ptr[T any](in T) *T {
	return &in
}
