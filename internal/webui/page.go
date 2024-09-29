package webui

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"

	"github.com/alex65536/day20/internal/userauth"
	"github.com/alex65536/day20/internal/util/clone"
	"github.com/alex65536/day20/internal/util/httputil"
	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/alex65536/go-chess/util/maybe"
)

const sessionName = "day20_session"

type userInfo struct {
	ID       string
	Username string
	Epoch    int
}

func makeUserInfo(user *userauth.User) *userInfo {
	if user == nil {
		return nil
	}
	return &userInfo{
		ID:       user.ID,
		Username: user.Username,
		Epoch:    user.Epoch,
	}
}

type dataBuilder interface {
	Build(ctx context.Context, bc builderCtx) (any, error)
}

type pageOptions struct {
	NoUserInfo     bool
	NoShowAuth     bool
	FullUser       bool
	GetUserOptions maybe.Maybe[userauth.GetUserOptions]
}

type page struct {
	name     string
	cfg      *Config
	pageOpts pageOptions
	log      *slog.Logger
	b        dataBuilder
	tmpl     *template.Template
	errTmpl  *template.Template
}

type pageData struct {
	Data     any
	User     *userInfo
	WithAuth bool
}

type builderCtx struct {
	Log      *slog.Logger
	Config   *Config
	UserInfo *userInfo
	FullUser *userauth.User
	Req      *http.Request
	writer   http.ResponseWriter
}

func (bc *builderCtx) UpgradeSession(newUser *userInfo) {
	log := bc.Log
	session, _ := bc.Config.sessionStore.Get(bc.Req, sessionName)
	delete(session.Values, "user")
	if newUser != nil {
		session.Values["user"] = &newUser
	}
	if err := session.Save(bc.Req, bc.writer); err != nil {
		log.Error("apply new session", slogx.Err(err))
	}
}

func (bc *builderCtx) ResetSession(newUser *userInfo) {
	log := bc.Log
	session, _ := bc.Config.sessionStore.Get(bc.Req, sessionName)
	session.Options.MaxAge = -1
	for k := range session.Values {
		delete(session.Values, k)
	}
	if err := session.Save(bc.Req, bc.writer); err != nil {
		log.Error("expire current session", slogx.Err(err))
	}
	session, _ = bc.Config.sessionStore.New(bc.Req, sessionName)
	if newUser != nil {
		session.Values["user"] = &newUser
	}
	if err := session.Save(bc.Req, bc.writer); err != nil {
		log.Error("apply new session", slogx.Err(err))
	}
	bc.UserInfo = clone.TrivialPtr(newUser)
	bc.FullUser = nil
}

func (p *page) renderError(log *slog.Logger, w http.ResponseWriter, httpErr *httputil.Error) {
	if 300 <= httpErr.Code() && httpErr.Code() <= 399 {
		log.Info("send http redirect",
			slog.Int("code", httpErr.Code()),
			slog.String("msg", httpErr.Message()),
		)
		httpErr.ApplyHeaders(w)
		w.WriteHeader(httpErr.Code())
		return
	}

	log.Info("send http status error",
		slog.Int("code", httpErr.Code()),
		slog.String("msg", httpErr.Message()),
	)
	var b bytes.Buffer
	if err := p.errTmpl.Execute(&b, pageData{
		Data: struct {
			Code    int
			Message string
		}{
			Code:    httpErr.Code(),
			Message: httpErr.Message(),
		},
	}); err != nil {
		log.Error("error rendering page", slogx.Err(err))
		writeHTTPErr(log, w, fmt.Errorf("render page"))
		return
	}
	w.Header().Set("Context-Type", "text/html")
	httpErr.ApplyHeaders(w)
	w.WriteHeader(httpErr.Code())
	if _, err := w.Write(b.Bytes()); err != nil {
		log.Error("error writing page data", slogx.Err(err))
		return
	}
}

func (p *page) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	log := p.log.With(slog.String("rid", httputil.ExtractReqID(ctx)))
	log.Info("handle page request",
		slog.String("method", req.Method),
		slog.String("addr", req.RemoteAddr),
	)

	if req.Method != http.MethodGet && req.Method != http.MethodPost {
		log.Warn("method not allowed")
		writeHTTPErr(log, w, httputil.MakeError(http.StatusMethodNotAllowed, "method not allowed"))
		return
	}

	var userInf *userInfo
	if !p.pageOpts.NoUserInfo {
		session, _ := p.cfg.sessionStore.Get(req, sessionName)
		userInfoAny := session.Values["user"]
		if userInfoAny != nil {
			rawUserInfo := userInfoAny.(userInfo)
			userInf = &rawUserInfo
		}
		if session.IsNew {
			p.cfg.opts.Session.SetupSession(session.Options)
			if err := p.cfg.sessionStore.Save(req, w, session); err != nil {
				log.Error("could not save session", slogx.Err(err))
			}
		}
	}

	var fullUser *userauth.User
	resetSession := false
	if p.pageOpts.FullUser && userInf != nil {
		var opts []userauth.GetUserOptions
		if o, ok := p.pageOpts.GetUserOptions.TryGet(); ok {
			opts = append(opts, o)
		}
		rawFullUser, err := p.cfg.UserManager.GetUser(ctx, userInf.ID, opts...)
		if err != nil {
			if errors.Is(err, userauth.ErrUserNotFound) {
				resetSession = true
			} else {
				log.Error("could not fetch full user", slogx.Err(err))
			}
			userInf = nil
		} else {
			fullUser = &rawFullUser
			if fullUser.Perms.IsBlocked || fullUser.Epoch != userInf.Epoch {
				resetSession = true
			}
		}
	}

	bc := builderCtx{
		Log:      log,
		Config:   p.cfg,
		UserInfo: userInf,
		FullUser: fullUser,
		Req:      req,
		writer:   w,
	}
	if resetSession {
		bc.ResetSession(nil)
	}

	data, err := p.b.Build(ctx, bc)
	if err != nil {
		if httpErr := (*httputil.Error)(nil); errors.As(err, &httpErr) {
			p.renderError(log, w, httpErr)
			return
		}
		log.Error("error building page data", slogx.Err(err))
		writeHTTPErr(log, w, fmt.Errorf("build page"))
		return
	}

	var b bytes.Buffer
	if err := p.tmpl.Execute(&b, pageData{
		Data:     data,
		User:     bc.UserInfo,
		WithAuth: !p.pageOpts.NoUserInfo && !p.pageOpts.NoShowAuth,
	}); err != nil {
		log.Error("error rendering page", slogx.Err(err))
		writeHTTPErr(log, w, fmt.Errorf("render page"))
		return
	}

	w.Header().Set("Context-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(b.Bytes()); err != nil {
		log.Error("error writing page data", slogx.Err(err))
		return
	}
}

func newPage(
	log *slog.Logger,
	cfg *Config,
	pageOpts pageOptions,
	templator *templator,
	builder dataBuilder,
	name string,
) (http.Handler, error) {
	tmpl, err := templator.Get(name)
	if err != nil {
		return nil, fmt.Errorf("template %q: %w", name, err)
	}
	errTempl, err := templator.Get("error")
	if err != nil {
		return nil, fmt.Errorf("template \"error\": %w", err)
	}
	return &page{
		name:     name,
		cfg:      cfg,
		pageOpts: pageOpts,
		log:      log.With(slog.String("page", name)),
		b:        builder,
		tmpl:     tmpl,
		errTmpl:  errTempl,
	}, nil
}

func init() {
	gob.Register(userInfo{})
}
