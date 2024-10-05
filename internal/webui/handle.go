package webui

import (
	"compress/gzip"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/NYTimes/gziphandler"
	"github.com/alex65536/day20/internal/roomkeeper"
	"github.com/alex65536/day20/internal/scheduler"
	"github.com/alex65536/day20/internal/userauth"
	"github.com/alex65536/day20/internal/util/idgen"
	"github.com/alex65536/day20/internal/util/websockutil"
	"github.com/gorilla/csrf"
	"github.com/gorilla/sessions"
)

type SessionStoreFactory interface {
	NewSessionStore(ctx context.Context, opts SessionOptions) sessions.Store
}

type Config struct {
	Keeper              *roomkeeper.Keeper
	UserManager         *userauth.Manager
	SessionStoreFactory SessionStoreFactory
	Scheduler           *scheduler.Scheduler
	sessionStore        sessions.Store
	prefix              string
	opts                *Options
}

type SessionOptions struct {
	Key             []byte        `toml:"-"`
	CleanupInterval time.Duration `toml:"cleanup-interval"`
	Insecure        bool          `toml:"insecure"`
	MaxAge          time.Duration `toml:"max-age"`
}

func (o *SessionOptions) FillDefaults() {
	if o.CleanupInterval == 0 {
		o.CleanupInterval = 1 * time.Hour
	}
	if o.MaxAge == 0 {
		o.MaxAge = 42 * 24 * time.Hour
	}
}

func (o *SessionOptions) AssignSessionOptions(s *sessions.Options) {
	s.SameSite = http.SameSiteLaxMode
	s.Secure = !o.Insecure
	s.HttpOnly = true
	s.MaxAge = int(o.MaxAge.Seconds())
}

func (o SessionOptions) Clone() SessionOptions {
	o.Key = slices.Clone(o.Key)
	return o
}

type Options struct {
	WebSocket         websockutil.Options `toml:"websocket"`
	ReadCursorTimeout time.Duration       `toml:"read-cursor-timeout"`
	RoomRPSLimit      float64             `toml:"room-rps-limit"`
	RoomRPSBurst      int                 `toml:"room-rps-burst"`
	ServerID          string              `toml:"server-id"`
	Session           SessionOptions      `toml:"session"`
	CSRFKey           []byte              `toml:"-"`
	Compression       string              `toml:"compression"`
}

func (o *Options) makeCompressor() (func(http.Handler) http.Handler, error) {
	switch o.Compression {
	case "none":
		return func(h http.Handler) http.Handler { return h }, nil
	case "gzip":
		h, err := gziphandler.NewGzipLevelHandler(gzip.DefaultCompression)
		if err != nil {
			return nil, fmt.Errorf("create gzip handler: %w", err)
		}
		return h, nil
	default:
		return nil, fmt.Errorf("unknown compression %q", o.Compression)
	}
}

func (o *Options) FillDefaults() {
	o.WebSocket.FillDefaults()
	if o.ReadCursorTimeout == 0 {
		o.ReadCursorTimeout = 30 * time.Second
	}
	if o.RoomRPSLimit == 0.0 {
		o.RoomRPSLimit = 3
	}
	if o.RoomRPSBurst == 0 {
		o.RoomRPSBurst = 5
	}
	o.Session.FillDefaults()
	if o.Compression == "" {
		o.Compression = "gzip"
	}
}

func (o Options) Clone() Options {
	o.Session = o.Session.Clone()
	o.CSRFKey = slices.Clone(o.CSRFKey)
	return o
}

func must[T any](t T, err error) T {
	if err != nil {
		panic(err)
	}
	return t
}

func Handle(ctx context.Context, log *slog.Logger, mux *http.ServeMux, prefix string, cfg Config, o Options) {
	o = o.Clone()
	o.FillDefaults()
	if len(o.Session.Key) == 0 {
		panic("no session key")
	}
	if o.ServerID == "" {
		o.ServerID = idgen.ID()
	}
	if len(o.CSRFKey) != 32 {
		panic("bad csrf key")
	}

	cfg.sessionStore = cfg.SessionStoreFactory.NewSessionStore(ctx, o.Session)
	cfg.prefix = prefix
	cfg.opts = &o
	b := middlewareBuilder{
		Log:         log,
		Prefix:      prefix,
		CSRFProtect: csrf.Protect(o.CSRFKey),
		Compress:    must(o.makeCompressor()),
	}
	templ := must(newTemplator(&cfg))

	// Static.
	mux.Handle(prefix+"/img/", b.WrapStatic(http.FileServerFS(staticData)))
	mux.Handle(prefix+"/css/", b.WrapStatic(http.FileServerFS(staticData)))
	mux.Handle(prefix+"/font/", b.WrapStatic(http.FileServerFS(staticData)))
	mux.Handle(prefix+"/js/", b.WrapStatic(http.FileServerFS(staticData)))
	mux.Handle(prefix+"/favicon.ico", b.WrapStatic(http.FileServerFS(staticData)))
	mux.Handle(prefix+"/favicon.png", b.WrapStatic(http.FileServerFS(staticData)))
	mux.Handle(prefix+"/favicon.svg", b.WrapStatic(http.FileServerFS(staticData)))

	// Pages, attaches & websockets.
	mux.Handle(prefix+"/{$}", b.WrapPage(must(mainPage(log, &cfg, templ))))
	mux.Handle(prefix+"/room/{roomID}", b.WrapPage(must(roomPage(log, &cfg, templ))))
	mux.Handle(prefix+"/room/{roomID}/ws", b.WrapWebSocket(must(roomWebSocket(log, &cfg, templ))))
	mux.Handle(prefix+"/room/{roomID}/pgn", b.WrapAttach(roomPGNAttach(log, &cfg)))
	mux.Handle(prefix+"/invite/{inviteVal}", b.WrapPage(must(invitePage(log, &cfg, templ))))
	mux.Handle(prefix+"/login", b.WrapPage(must(loginPage(log, &cfg, templ))))
	mux.Handle(prefix+"/logout", b.WrapPage(must(logoutPage(log, &cfg, templ))))
	mux.Handle(prefix+"/profile", b.WrapPage(must(profilePage(log, &cfg, templ))))
	mux.Handle(prefix+"/user/{username}", b.WrapPage(must(userPage(log, &cfg, templ))))
	mux.Handle(prefix+"/invites", b.WrapPage(must(invitesPage(log, &cfg, templ))))
	mux.Handle(prefix+"/users", b.WrapPage(must(usersPage(log, &cfg, templ))))
	mux.Handle(prefix+"/contests", b.WrapPage(must(contestsPage(log, &cfg, templ))))
	mux.Handle(prefix+"/contests/new", b.WrapPage(must(contestsNewPage(log, &cfg, templ))))
	mux.Handle(prefix+"/contest/{contestID}", b.WrapPage(must(contestPage(log, &cfg, templ))))
	mux.Handle(prefix+"/contest/{contestID}/pgn", b.WrapAttach(contestPGNAttach(log, &cfg)))
	mux.Handle(prefix+"/roomtokens", b.WrapPage(must(roomtokensPage(log, &cfg, templ))))
	mux.Handle(prefix+"/roomtokens/new", b.WrapPage(must(roomtokensNewPage(log, &cfg, templ))))

	// 404.
	mux.Handle(prefix+"/", b.WrapPage(must(e404Page(log, &cfg, templ))))
}
