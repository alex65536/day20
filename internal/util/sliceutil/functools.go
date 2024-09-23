package sliceutil

func Map[T any, U any, F ~func(T) U](items []T, f F) []U {
	res := make([]U, len(items))
	for i, item := range items {
		res[i] = f(item)
	}
	return res
}

func FilterMap[T any, U any, F ~func(T) (U, bool)](items []T, f F) []U {
	res := make([]U, 0, len(items))
	for _, item := range items {
		if mapped, ok := f(item); ok {
			res = append(res, mapped)
		}
	}
	return res
}
