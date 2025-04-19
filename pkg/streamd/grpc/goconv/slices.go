package goconv

func convertSlice[IN, OUT any](
	tracks []IN,
	convFunc func(IN) OUT,
) []OUT {
	result := make([]OUT, 0, len(tracks))
	for _, item := range tracks {
		result = append(result, convFunc(item))
	}
	return result
}
