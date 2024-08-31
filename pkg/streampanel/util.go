package streampanel

func assert(b bool) {
	if !b {
		panic("assertion failed")
	}
}

func reverse[T any](s []T) []T {
	l := len(s)
	for i := 0; i < l/2; i++ {
		s[i], s[l-1-i] = s[l-1-i], s[i]
	}
	return s
}
