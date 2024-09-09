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

	httputil "github.com/alex65536/day20/internal/util/http"
	randutil "github.com/alex65536/day20/internal/util/rand"
	"github.com/alex65536/day20/internal/util/slogx"
)

type Server interface {
	Update(ctx context.Context, log *slog.Logger, req *UpdateRequest) (*UpdateResponse, error)
	Job(ctx context.Context, log *slog.Logger, req *JobRequest) (*JobResponse, error)
	Hello(ctx context.Context, log *slog.Logger, req *HelloRequest) (*HelloResponse, error)
	Bye(ctx context.Context, log *slog.Logger, req *ByeRequest) (*ByeResponse, error)
}

type TokenChecker func(token string) error

type ServerOptions struct {
	TokenChecker TokenChecker
}

func makeHandler[Req any, Rsp any](
	log *slog.Logger,
	o *ServerOptions,
	fn func(context.Context, *slog.Logger, *Req) (*Rsp, error),
) http.HandlerFunc {
	return func(w http.ResponseWriter, hReq *http.Request) {
		log := log.With(
			slog.String("addr", hReq.RemoteAddr),
			slog.String("method", hReq.Method),
			slog.String("rid", randutil.InsecureID()),
		)

		if err := func() error {
			log.Info("handle roomapi request")

			if hReq.Method != http.MethodPost {
				log.Warn("unsupported method")
				return httputil.MakeHTTPError(http.StatusMethodNotAllowed, "method not allowed")
			}

			// TODO test it!
			if strings.ToLower(hReq.Header.Get("Expect")) == "spanish inquisition" {
				log.Info("nobody expects the spanish inquisition")
				return httputil.MakeHTTPError(http.StatusExpectationFailed, "NOBODY EXPECTS THE SPANISH INQUISITION!")
			}

			if err := o.TokenChecker(hReq.Header.Get("X-Token")); err != nil {
				log.Warn("token auth failed", slogx.Err(err))
				return &Error{Code: ErrBadToken, Message: "bad token auth"}
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			reqBytes, err := io.ReadAll(hReq.Body)
			if err != nil {
				log.Info("error reading request", slogx.Err(err))
				return nil
			}
			var req *Req
			if err := json.Unmarshal(reqBytes, req); err != nil {
				log.Warn("error unmarshalling json", slogx.Err(err))
				return httputil.MakeHTTPError(http.StatusBadRequest, "unmarshal json request")
			}

			rsp, err := fn(ctx, log, req)
			if err != nil {
				if apiErr := (*Error)(nil); errors.As(err, &apiErr) {
					return err
				}
				log.Warn("handler failed", slogx.Err(err))
				return httputil.MakeHTTPError(http.StatusInternalServerError, "internal server error")
			}

			rspBytes, err := json.Marshal(rsp)
			if err != nil {
				log.Warn("error marshalling json", slogx.Err(err))
				return httputil.MakeHTTPError(http.StatusInternalServerError, "marshal json response")
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
				case ErrJobCanceled:
					code = http.StatusNotFound
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

func RegisterServer(s Server, mux *http.ServeMux, o ServerOptions, prefix string, log *slog.Logger) error {
	if o.TokenChecker == nil {
		return fmt.Errorf("no token checker")
	}
	mux.HandleFunc(prefix+"/update",
		makeHandler(log.With(slog.String("method", "update")), &o, s.Update))
	mux.HandleFunc(prefix+"/job",
		makeHandler(log.With(slog.String("method", "job")), &o, s.Job))
	mux.HandleFunc(prefix+"/hello",
		makeHandler(log.With(slog.String("method", "hello")), &o, s.Hello))
	mux.HandleFunc(prefix+"/bye",
		makeHandler(log.With(slog.String("method", "bye")), &o, s.Bye))
	return nil
}
