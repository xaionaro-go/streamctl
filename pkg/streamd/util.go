package streamd

func ptr[T any](in T) *T {
	return &in
}

func assert(b bool) {
	if !b {
		panic("assertion failed")
	}
}
