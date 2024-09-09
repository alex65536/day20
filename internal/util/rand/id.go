package rand

import (
	crypto "crypto/rand"
	"fmt"
	"math/big"
	"math/rand/v2"
	"strings"
)

const (
	idAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"
	idLen      = 22
)

func InsecureID() string {
	var b strings.Builder
	for range idLen {
		_ = b.WriteByte(idAlphabet[rand.IntN(len(idAlphabet))])
	}
	return b.String()
}

func SecureID() (string, error) {
	var b strings.Builder
	for range idLen {
		pos, err := crypto.Int(crypto.Reader, big.NewInt(int64(len(idAlphabet))))
		if err != nil {
			return "", fmt.Errorf("crypto rand: %w", err)
		}
		_ = b.WriteByte(idAlphabet[pos.Int64()])
	}
	return b.String(), nil
}

func MustSecureID() string {
	s, err := SecureID()
	if err != nil {
		panic(err)
	}
	return s
}
