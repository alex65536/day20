package webui

import (
	"log/slog"
	"net/http"

	httputil "github.com/alex65536/day20/internal/util/http"
	"github.com/alex65536/day20/internal/util/slogx"
)

func writeHTTPErr(log *slog.Logger, w http.ResponseWriter, err error) {
	if err = httputil.WriteErrorResponse(err, w); err != nil {
		log.Info("error writing error response", slogx.Err(err))
	}
}
