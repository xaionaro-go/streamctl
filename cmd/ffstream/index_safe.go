package main

func indexSafe[T any](s []T, index int) T {
	if index >= len(s) {
		var zeroValue T
		return zeroValue
	}
	return s[index]
}
