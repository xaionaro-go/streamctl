package streampanel

func reverse[T any](s []T) []T {
	l := len(s)
	for i := range l / 2 {
		s[i], s[l-1-i] = s[l-1-i], s[i]
	}
	return s
}
