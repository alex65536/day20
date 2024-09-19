package database

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"time"

	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/mattn/go-colorable"
	"gorm.io/gorm/logger"
)

type ourLogger struct {
	log *slog.Logger
	o   *Options
}

func Logger(srcLog *slog.Logger, o Options) logger.Interface {
	if o.Debug {
		// In debug mode, use a fancier logger built into gorm itself.
		return logger.New(
			log.New(colorable.NewColorableStdout(), "", log.LstdFlags),
			logger.Config{
				LogLevel:                  logger.Info,
				IgnoreRecordNotFoundError: false,
				Colorful:                  true,
			},
		)
	}
	return &ourLogger{
		log: srcLog,
		o:   &o,
	}
}

func (l *ourLogger) LogMode(level logger.LogLevel) logger.Interface {
	// No-op.
	return l
}

func (l *ourLogger) Info(ctx context.Context, msg string, data ...any) {
	l.log.Info("gorm info", slog.String("msg", fmt.Sprintf(msg, data...)))
}

func (l *ourLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	l.log.Warn("gorm warn", slog.String("msg", fmt.Sprintf(msg, data...)))
}

func (l *ourLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	l.log.Error("gorm error", slog.String("msg", fmt.Sprintf(msg, data...)))
}

func (l *ourLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	elapsed := time.Since(begin)
	switch {
	case err != nil && !errors.Is(err, logger.ErrRecordNotFound):
		sql, _ := fc()
		l.log.Error("gorm sql error", slog.Duration("elapsed", elapsed), slogx.Err(err), slog.String("sql", sql))
	case elapsed > l.o.SlowThreshold:
		sql, _ := fc()
		l.log.Warn("slow sql", slog.Duration("elapsed", elapsed), slog.String("sql", sql))
	}
}
