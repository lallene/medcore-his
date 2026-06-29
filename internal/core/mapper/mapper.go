package mapper

func MapSlice[T any, R any](items []T, fn func(T) R) []R {
	result := make([]R, 0, len(items))

	for _, item := range items {
		result = append(result, fn(item))
	}

	return result
}
