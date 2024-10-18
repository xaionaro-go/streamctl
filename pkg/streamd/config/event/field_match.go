package event

import "strings"

func fieldMatch[T comparable](f1 *T, f2 *T) bool {
	if f1 == nil || f2 == nil {
		return true
	}

	return *f1 == *f2
}

func partialFieldMatch(full *string, partial *string) bool {
	if full == nil || partial == nil {
		return true
	}

	return strings.Contains(*full, *partial)
}
