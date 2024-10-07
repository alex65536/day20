package webui

import (
	"log/slog"
	"net/http"

	"github.com/alex65536/day20/internal/util/httputil"
	"github.com/alex65536/day20/internal/util/slogx"
)

func writeHTTPErr(log *slog.Logger, w http.ResponseWriter, err error) {
	if err = httputil.WriteErrorResponse(err, w); err != nil {
		log.Info("error writing error response", slogx.Err(err))
	}
}

func tagLogWithReq(log *slog.Logger, req *http.Request) *slog.Logger {
	return log.With(
		slog.String("uri", req.RequestURI),
		slog.String("method", req.Method),
		slog.String("addr", req.RemoteAddr),
		slog.String("user_agent", req.UserAgent()),
	)
}
