package randutil

import (
	"maps"
	"math/rand/v2"
	"slices"
)

type Set[T comparable] struct {
	mp map[T]int
	v  []T
}

func (s *Set[T]) makeMap() {
	if s.mp == nil {
		s.mp = make(map[T]int)
	}
}

func (s *Set[T]) Add(val T) bool {
	s.makeMap()
	if _, ok := s.mp[val]; ok {
		return false
	}
	s.mp[val] = len(s.v)
	s.v = append(s.v, val)
	return true
}

func (s Set[T]) Has(val T) bool {
	if s.mp == nil {
		return false
	}
	_, ok := s.mp[val]
	return ok
}

func (s Set[T]) Len() int {
	return len(s.v)
}

func (s *Set[T]) Del(val T) bool {
	s.makeMap()
	idx, ok := s.mp[val]
	if !ok {
		return false
	}
	tail := len(s.v)-1
	if idx != tail {
		s.v[idx], s.v[tail] = s.v[tail], s.v[idx]
		s.mp[s.v[idx]] = idx
	}
	s.v = s.v[:tail]
	delete(s.mp, val)
	return true
}

func (s Set[T]) Get() T {
	if len(s.v) == 0 {
		return *new(T)
	}
	return s.v[rand.IntN(len(s.v))]
}

func (s Set[T]) Clone() Set[T] {
	s.mp = maps.Clone(s.mp)
	s.v = slices.Clone(s.v)
	return s
}
