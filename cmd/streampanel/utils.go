package main

func ptr[T any](in T) *T {
	return &in
}

func assertNoError(err error) {
	if err != nil {
		panic(err)
	}
}
