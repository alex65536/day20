package roomapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/alex65536/day20/internal/util/httputil"
	"github.com/alex65536/day20/internal/util/slogx"
)

type TokenChecker func(token string) error

type ServerConfig struct {
	TokenChecker TokenChecker
}

func makeHandler[Req any, Rsp any](
	log *slog.Logger,
	cfg *ServerConfig,
	fn func(context.Context, *Req) (*Rsp, error),
) http.HandlerFunc {
	return func(w http.ResponseWriter, hReq *http.Request) {
		hReq = httputil.WrapRequest(hReq)
		ctx := hReq.Context()

		log := log.With(slog.String("rid", httputil.ExtractReqID(ctx)))

		if err := func() error {
			log.Info("handle roomapi request",
				slog.String("method", hReq.Method),
				slog.String("addr", hReq.RemoteAddr),
			)

			if hReq.Method != http.MethodPost {
				log.Warn("unsupported method")
				return httputil.MakeError(http.StatusMethodNotAllowed, "method not allowed")
			}

			if contentType := hReq.Header.Get("Content-Type"); contentType != "application/json" {
				log.Warn("bad request content type", slog.String("content_type", contentType))
				return httputil.MakeError(http.StatusUnsupportedMediaType, "bad request content type")
			}

			tokenChecked := false
			if token, authOk := func() (string, bool) {
				auth := hReq.Header.Get("Authorization")
				if auth == "" {
					log.Info("unauthorized request")
					return "", false
				}
				token, ok := strings.CutPrefix(auth, "Bearer ")
				if !ok {
					log.Warn("bad auth token format")
					return "", false
				}
				return token, true
			}(); authOk {
				if err := cfg.TokenChecker(token); err != nil {
					log.Warn("bad token", slogx.Err(err))
					return &Error{Code: ErrBadToken, Message: "bad token auth"}
				}
				tokenChecked = true
			} else {
				return httputil.MakeAuthError("bad auth", "Bearer")
			}
			if !tokenChecked {
				// Extra safeguard to protect against auth bypass.
				return httputil.MakeAuthError("bad auth", "Bearer")
			}

			reqBytes, err := io.ReadAll(hReq.Body)
			if err != nil {
				log.Info("error reading request", slogx.Err(err))
				return nil
			}
			var req *Req
			if err := json.Unmarshal(reqBytes, &req); err != nil {
				log.Warn("error unmarshalling json", slogx.Err(err))
				return httputil.MakeError(http.StatusBadRequest, "unmarshal json request")
			}

			rsp, err := fn(ctx, req)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					select {
					case <-ctx.Done():
						err = &Error{
							Code:    ErrTemporarilyUnavailable,
							Message: "context canceled or expired",
						}
					default:
					}
				}
				if apiErr := (*Error)(nil); errors.As(err, &apiErr) {
					return err
				}
				log.Warn("handler failed", slogx.Err(err))
				return httputil.MakeError(http.StatusInternalServerError, "internal server error")
			}

			rspBytes, err := json.Marshal(rsp)
			if err != nil {
				log.Warn("error marshalling json", slogx.Err(err))
				return httputil.MakeError(http.StatusInternalServerError, "marshal json response")
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write(rspBytes); err != nil {
				log.Info("error writing response", slogx.Err(err))
			}
			return nil
		}(); err != nil {
			var apiError *Error
			if errors.As(err, &apiError) {
				var code int
				switch apiError.Code {
				case ErrNeedsResync:
					code = http.StatusConflict
				case ErrNoJob:
					code = http.StatusNotFound
				case ErrNoSuchRoom:
					code = http.StatusGone
				case ErrBadToken:
					code = http.StatusForbidden
				case ErrNoJobRunning:
					code = http.StatusNotFound
				case ErrIncompatibleProto:
					code = http.StatusBadRequest
				case ErrBadRequest:
					code = http.StatusBadRequest
				case ErrLocked:
					code = http.StatusConflict
				case ErrTemporarilyUnavailable:
					code = http.StatusServiceUnavailable
				case ErrOutOfSequence:
					code = http.StatusBadRequest
				default:
					code = http.StatusBadRequest
				}
				data, err := json.Marshal(apiError)
				if err != nil {
					log.Warn("error marshalling error json", slogx.Err(err))
					if err := httputil.WriteErrorResponse(fmt.Errorf("marshal error json"), w); err != nil {
						log.Info("error writing error response", slogx.Err(err))
					}
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(code)
				if _, err := w.Write(data); err != nil {
					log.Info("error writing error response", slogx.Err(err))
				}
				return
			}
			if err := httputil.WriteErrorResponse(err, w); err != nil {
				log.Info("error writing error response", slogx.Err(err))
			}
		}
	}
}

func make404Handler(log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, hReq *http.Request) {
		log.Info("404 not found",
			slog.String("uri", hReq.RequestURI),
			slog.String("method", hReq.Method),
			slog.String("addr", hReq.RemoteAddr),
		)
		http.NotFound(w, hReq)
	}
}

func HandleServer(log *slog.Logger, mux *http.ServeMux, prefix string, a API, cfg ServerConfig) error {
	if cfg.TokenChecker == nil {
		return fmt.Errorf("no token checker")
	}
	mux.HandleFunc(prefix+"/update",
		makeHandler(log.With(slog.String("handler", "update")), &cfg, a.Update))
	mux.HandleFunc(prefix+"/job",
		makeHandler(log.With(slog.String("handler", "job")), &cfg, a.Job))
	mux.HandleFunc(prefix+"/hello",
		makeHandler(log.With(slog.String("handler", "hello")), &cfg, a.Hello))
	mux.HandleFunc(prefix+"/bye",
		makeHandler(log.With(slog.String("handler", "bye")), &cfg, a.Bye))
	mux.HandleFunc(prefix+"/", make404Handler(log))
	return nil
}
