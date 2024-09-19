package sliceutil

func Map[T any, U any, F ~func(T) U](items []T, f F) []U {
	res := make([]U, len(items))
	for i, item := range items {
		res[i] = f(item)
	}
	return res
}
