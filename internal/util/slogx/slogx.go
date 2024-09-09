package slogx

import (
	"context"
	"log/slog"
)

type discardHandler struct{}

func IsDiscard(l *slog.Logger) bool {
	_, ok := l.Handler().(discardHandler)
	return ok
}

func DiscardLogger() *slog.Logger {
	return slog.New(Discard())
}

// Discard() is adapted from https://go-review.googlesource.com/c/go/+/547956. Hopefully it will
// eventually land into stable and we'll be able to remove this.
func Discard() slog.Handler {
	return discardHandler{}
}

func (discardHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (d discardHandler) WithAttrs([]slog.Attr) slog.Handler      { return d }
func (d discardHandler) WithGroup(string) slog.Handler           { return d }

func Err(err error) slog.Attr {
	return slog.String("err", err.Error())
}
