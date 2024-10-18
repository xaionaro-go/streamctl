package event

func fieldMatch[T comparable](f1 *T, f2 *T) bool {
	if f1 == nil || f2 == nil {
		return true
	}

	return *f1 == *f2
}
