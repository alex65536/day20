package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"sync"

	"github.com/alex65536/day20/internal/util/slogx"
	"golang.org/x/crypto/acme/autocert"
)

type servers struct {
	insecure *http.Server
	secure   *http.Server
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   func()
	log      *slog.Logger
}

func newServers(parentCtx context.Context, log *slog.Logger, o *Options, mux *http.ServeMux) (*servers, error) {
	if o.HTTPS != nil {
		if o.HTTPS.CachePath == "" {
			return nil, fmt.Errorf("certificate cache path not specified")
		}
	}
	ctx, cancel := context.WithCancel(parentCtx)
	s := &servers{
		ctx:    ctx,
		cancel: cancel,
		log:    log,
	}
	if o.HTTPS == nil || o.HTTPS.ExposeInsecure {
		s.insecure = &http.Server{
			Addr:        o.AddrWithPort(),
			Handler:     mux,
			BaseContext: func(net.Listener) context.Context { return ctx },
		}
	}
	if o.HTTPS != nil {
		m := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(slices.Clone(o.HTTPS.AllowedSecureDomains)...),
			Cache:      autocert.DirCache(o.HTTPS.CachePath),
		}
		s.secure = &http.Server{
			Addr:        o.SecureAddrWithPort(),
			TLSConfig:   m.TLSConfig(),
			Handler:     mux,
			BaseContext: func(net.Listener) context.Context { return ctx },
		}
	}
	return s, nil
}

func (s *servers) iterServers(f func(name string, serv *http.Server)) {
	if s.insecure != nil {
		f("insecure", s.insecure)
	}
	if s.secure != nil {
		f("secure", s.secure)
	}
}

func (s *servers) Go() {
	s.iterServers(func(name string, serv *http.Server) {
		if serv == nil {
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			log := s.log.With(slog.String("name", name))
			log.Info("starting http server")
			var err error
			if name == "secure" {
				err = serv.ListenAndServeTLS("", "")
			} else {
				err = serv.ListenAndServe()
			}
			if err != nil {
				if !errors.Is(err, http.ErrServerClosed) {
					select {
					case <-s.ctx.Done():
					default:
						log.Error("listen http server failed", slogx.Err(err))
					}
				}
			}
		}()
	})
}

func (s *servers) Shutdown() {
	s.iterServers(func(name string, serv *http.Server) {
		log := s.log.With(slog.String("name", name))
		log.Info("stopping http server")
		if err := serv.Shutdown(context.Background()); err != nil {
			log.Warn("could not shut down server", slogx.Err(err))
		}
	})
	s.cancel()
	s.wg.Wait()
}
