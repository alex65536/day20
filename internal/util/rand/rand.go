package rand

import (
	"math/rand/v2"
	"sync"
)

type defaultSource struct{}

func (defaultSource) Uint64() uint64 {
	return rand.Uint64()
}

func (defaultSource) SourceIsConcurrent() {}

func DefaultSource() rand.Source {
	return defaultSource{}
}

type ConcurrentSource interface {
	rand.Source
	SourceIsConcurrent()
}

type concurrentSource struct {
	s  rand.Source
	mu sync.Mutex
}

func (s *concurrentSource) Uint64() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.s.Uint64()
}

func (s *concurrentSource) SourceIsConcurrent() {}

func NewConcurrentSource(s rand.Source) ConcurrentSource {
	if cs, ok := s.(ConcurrentSource); ok {
		return cs
	}
	return &concurrentSource{s: s}
}
