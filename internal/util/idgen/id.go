package idgen

import (
	crand "crypto/rand"
	"fmt"
	"math/big"
	"math/rand/v2"
	"strings"
	"time"
)

const (
	idAlphabet    = "0123456789abcdefghjkmnpqrstvwxyz"
	tokenAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

func init() {
	if len(idAlphabet) != 32 {
		panic("must not happen")
	}
	for i := 1; i < len(idAlphabet); i++ {
		if idAlphabet[i-1] >= idAlphabet[i] {
			panic("must not happen")
		}
	}
	if len(tokenAlphabet) != 62 {
		panic("must not happen")
	}
	for i := 1; i < len(tokenAlphabet); i++ {
		if tokenAlphabet[i-1] >= tokenAlphabet[i] {
			panic("must not happen")
		}
	}
}

func ID() string {
	// This ID generator follows https://github.com/ulid/spec, but is lowercase and not monotonic.
	var b strings.Builder
	ts := uint64(time.Now().UnixMilli()) & ((1 << 48) - 1)
	for i := 45; i >= 0; i -= 5 {
		_ = b.WriteByte(idAlphabet[(ts>>i)&31])
	}
	for range 2 {
		r := rand.Uint64()
		for range 8 {
			_ = b.WriteByte(idAlphabet[r&31])
			r >>= 5
		}
	}
	return b.String()
}

func SecureLinkValue() (string, error) {
	var b strings.Builder
	var bigLen = big.NewInt(int64(len(idAlphabet)))
	for range 26 {
		idx, err := crand.Int(crand.Reader, bigLen)
		if err != nil {
			return "", fmt.Errorf("crypto rand: %w", err)
		}
		_ = b.WriteByte(idAlphabet[int(idx.Int64())])
	}
	return b.String(), nil
}

func SecureToken() (string, error) {
	var b strings.Builder
	_, _ = b.WriteString("CqrD2_")
	var bigLen = big.NewInt(int64(len(tokenAlphabet)))
	for range 32 {
		idx, err := crand.Int(crand.Reader, bigLen)
		if err != nil {
			return "", fmt.Errorf("crypto rand: %w", err)
		}
		_ = b.WriteByte(tokenAlphabet[int(idx.Int64())])
	}
	return b.String(), nil
}
