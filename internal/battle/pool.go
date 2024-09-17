package battle

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/alex65536/day20/internal/util/idgen"
	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/alex65536/go-chess/uci"
	"github.com/alex65536/go-chess/util/maybe"
)

type logAdapter struct {
	log   *slog.Logger
	level slog.Level
}

func (l *logAdapter) Printf(s string, args ...any) {
	l.log.Log(context.Background(), l.level, fmt.Sprintf(s, args...))
}

type EnginePool interface {
	AcquireEngine(ctx context.Context) (*uci.Engine, error)
	ReleaseEngine(e *uci.Engine)
	Name() string
	Close()
}

type EnginePoolOptions struct {
	ShortName     string
	ExeName       string
	Args          []string
	Options       map[string]uci.OptValue
	EngineOptions uci.EngineOptions
	CreateTimeout maybe.Maybe[time.Duration]
}

func (o *EnginePoolOptions) FillDefaults() {
	o.CreateTimeout = maybe.Some(o.CreateTimeout.GetOr(5 * time.Second))
}

func (o EnginePoolOptions) Clone() EnginePoolOptions {
	o.Args = slices.Clone(o.Args)
	o.Options = maps.Clone(o.Options)
	o.EngineOptions = o.EngineOptions.Clone()
	return o
}

func NewEnginePool(ctx context.Context, log *slog.Logger, o EnginePoolOptions) (EnginePool, error) {
	o = o.Clone()
	o.FillDefaults()

	if !slogx.IsDiscard(log) {
		log = log.With(slog.String("pool_id", idgen.ID()))
	}

	poolCtx, cancel := context.WithCancel(context.Background())
	pool := &enginePool{
		o:      o,
		ctx:    poolCtx,
		cancel: cancel,
		es:     nil,
		log:    log,
	}

	e, err := pool.AcquireEngine(ctx)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("create first engine: %w", err)
	}
	info, ok := e.Info()
	if !ok {
		panic("must not happen")
	}
	name := o.ShortName
	if name == "" {
		name = o.ExeName
	}
	pool.name = fmt.Sprintf("%v at %v", info.Name, name)
	pool.ReleaseEngine(e)

	return pool, err

}

type enginePool struct {
	o      EnginePoolOptions
	ctx    context.Context
	cancel func()
	mu     sync.Mutex
	es     []*uci.Engine
	name   string
	log    *slog.Logger
}

func (p *enginePool) AcquireEngine(ctx context.Context) (*uci.Engine, error) {
	p.mu.Lock()
	if len(p.es) != 0 {
		e := p.es[len(p.es)-1]
		p.es = p.es[:len(p.es)-1]
		p.mu.Unlock()
		return e, nil
	}
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, p.o.CreateTimeout.Get())
	defer cancel()

	logger := uci.NewNullLogger()
	if !slogx.IsDiscard(p.log) {
		logger = &logAdapter{
			log:   p.log.With(slog.String("engine_id", idgen.ID())),
			level: slog.LevelInfo,
		}
	}

	e, err := uci.NewEasyEngine(p.ctx, uci.EasyEngineOptions{
		Name:            p.o.ExeName,
		Args:            p.o.Args,
		SysProcAttr:     engineSysProcAttr(),
		Options:         p.o.EngineOptions,
		WaitInitialized: false,
		Logger:          logger,
	})
	if err != nil {
		return nil, fmt.Errorf("create: %w", err)
	}
	if err := e.WaitInitialized(ctx); err != nil {
		e.Close()
		return nil, fmt.Errorf("wait init: %w", err)
	}
	for k, v := range p.o.Options {
		if err := e.SetOption(ctx, k, v); err != nil {
			e.Close()
			return nil, fmt.Errorf("set option %q: %w", k, err)
		}
	}

	return e, nil
}

func (p *enginePool) ReleaseEngine(e *uci.Engine) {
	if e.Terminated() {
		return
	}
	if e.Terminating() || e.CurSearch() != nil {
		e.Close()
		return
	}
	p.mu.Lock()
	p.es = append(p.es, e)
	p.mu.Unlock()
}

func (p *enginePool) Name() string {
	return p.name
}

func (p *enginePool) Close() {
	p.cancel()
	p.mu.Lock()
	es := p.es
	p.es = nil
	p.mu.Unlock()
	for _, e := range es {
		<-e.Done()
	}
}
