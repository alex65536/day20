package randutil

import (
	"maps"
	"math/rand/v2"
	"testing"
)

func TestSetStress(t *testing.T) {
	var s Set[int64]
	actual := make(map[int64]struct{})
	const iters = 200_000
	const numbers = 20
	for range iters {
		v := rand.Int64N(numbers)
		switch rand.IntN(2) {
		case 0:
			_, ok := actual[v]
			actual[v] = struct{}{}
			ok = !ok
			ok2 := s.Add(v)
			if ok != ok2 {
				t.Fatalf("insert %v yields different results: expected = %v, got = %v", v, ok, ok2)
			}
		case 1:
			_, ok := actual[v]
			delete(actual, v)
			ok2 := s.Del(v)
			if ok != ok2 {
				t.Fatalf("delete %v yields different results: expected = %v, got = %v", v, ok, ok2)
			}
		default:
			panic("must not happen")
		}
		if len(actual) != s.Len() {
			t.Fatalf("length differs: expected = %v, got = %v", len(actual), s.Len())
		}
		gather := maps.Clone(actual)
		iters := 0
		for len(gather) != 0 {
			v := s.Get()
			if _, ok := actual[v]; !ok {
				t.Fatalf("unexpected get: %v", v)
			}
			delete(gather, v)
			iters++
			if iters > len(actual) * numbers * 1_000 {
				t.Fatalf("cannot collect all the numbers for too long")
			}
		}
	}
}
