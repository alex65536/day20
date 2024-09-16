package util

import (
	"github.com/alex65536/go-chess/util/maybe"
)

type Clonable[T any] interface {
	Clone() T
}

func CloneMaybe[T Clonable[T]](m maybe.Maybe[T]) maybe.Maybe[T] {
	if m.IsNone() {
		return maybe.None[T]()
	}
	return maybe.Some(m.Get().Clone())
}

func ClonePtr[T Clonable[T]](a *T) *T {
	if a == nil {
		return nil
	}
	b := (*a).Clone()
	return &b
}

func CloneTrivialPtr[T any](a *T) *T {
	if a == nil {
		return nil
	}
	b := *a
	return &b
}
