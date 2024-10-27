package buildvars

func ptr[T any](v T) *T {
	return &v
}
