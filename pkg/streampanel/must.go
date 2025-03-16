package streampanel

func must[T any](in T, err error) T {
	if err != nil {
		panic(err)
	}
	return in
}
