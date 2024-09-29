package streamforward

func ptr[T any](in T) *T {
	return &in
}
