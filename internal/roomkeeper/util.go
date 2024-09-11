package roomkeeper

import (
	randutil "github.com/alex65536/day20/internal/util/rand"
)

func genUnusedKey[V any](mp map[string]V) string {
	for {
		s := randutil.InsecureID()
		if _, ok := mp[s]; !ok {
			return s
		}
	}
}
