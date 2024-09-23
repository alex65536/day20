package clone

import (
	"github.com/alex65536/go-chess/util/maybe"
)

type Cloner[T any] interface {
	Clone() T
}

func Maybe[T Cloner[T]](m maybe.Maybe[T]) maybe.Maybe[T] {
	if m.IsNone() {
		return maybe.None[T]()
	}
	return maybe.Some(m.Get().Clone())
}

func Ptr[T Cloner[T]](a *T) *T {
	if a == nil {
		return nil
	}
	b := (*a).Clone()
	return &b
}

func TrivialPtr[T any](a *T) *T {
	if a == nil {
		return nil
	}
	b := *a
	return &b
}

func DeepSlice[T Cloner[T]](a []T) []T {
	res := make([]T, len(a))
	for i, v := range a {
		res[i] = v.Clone()
	}
	return res
}
