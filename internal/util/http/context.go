package http

import (
	"context"

	randutil "github.com/alex65536/day20/internal/util/rand"
)

type reqIDKey struct{}

func NewRequestContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	ctx = context.WithValue(ctx, reqIDKey{}, randutil.InsecureID())
	return ctx, cancel
}

func ExtractReqID(ctx context.Context) string {
	val := ctx.Value(reqIDKey{})
	if val == nil {
		return ""
	}
	if s, ok := val.(string); ok {
		return s
	}
	return ""
}
