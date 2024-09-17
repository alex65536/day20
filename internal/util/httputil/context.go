package httputil

import (
	"context"
	"net/http"

	"github.com/alex65536/day20/internal/util/idgen"
)

type reqIDKey struct{}

func WrapRequestContext(parent context.Context) context.Context {
	return context.WithValue(parent, reqIDKey{}, idgen.ID())
}

func WrapRequest(req *http.Request) *http.Request {
	return req.WithContext(WrapRequestContext(req.Context()))
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
