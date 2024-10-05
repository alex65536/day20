package userauth

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

type TokenCheckerOptions struct {
	CacheExpiryInterval time.Duration `toml:"cache-expiry-interval"`
}

func (o TokenCheckerOptions) Clone() TokenCheckerOptions {
	return o
}

func (o *TokenCheckerOptions) FillDefaults() {
	if o.CacheExpiryInterval == 0 {
		o.CacheExpiryInterval = 3 * time.Minute
	}
}

type TokenChecker struct {
	o      TokenCheckerOptions
	db     DB
	cache  sync.Map
	group  singleflight.Group
	ctx    context.Context
	cancel func()
	done   chan struct{}
}

func NewTokenChecker(o TokenCheckerOptions, db DB) *TokenChecker {
	ctx, cancel := context.WithCancel(context.Background())
	t := &TokenChecker{
		o:      o,
		db:     db,
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go t.loop()
	return t
}

func (t *TokenChecker) Check(srcToken string) error {
	now := time.Now()
	hash := HashRoomToken(srcToken)
	v, ok := t.cache.Load(hash)
	if ok {
		val := v.(*tokenCacheVal)
		if now.After(val.deadline) {
			t.cache.CompareAndDelete(hash, v)
			ok = false
		}
	}
	if ok {
		return nil
	}
	_, err, _ := t.group.Do(hash, func() (any, error) {
		tok, err := t.db.GetRoomToken(t.ctx, hash)
		if err != nil {
			return nil, fmt.Errorf("get room token: %w", err)
		}
		if tok.Hash != hash {
			return nil, fmt.Errorf("hash mismatch")
		}
		return nil, nil
	})
	if err != nil {
		return err
	}
	t.cache.Store(hash, &tokenCacheVal{
		deadline: time.Now().Add(t.o.CacheExpiryInterval),
	})
	return nil
}

func (t *TokenChecker) Close() {
	t.cancel()
	<-t.done
}

func (t *TokenChecker) loop() {
	defer close(t.done)
	ticker := time.NewTicker(t.o.CacheExpiryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-t.ctx.Done():
			return
		default:
			now := time.Now()
			t.cache.Range(func(k, v any) bool {
				val := v.(*tokenCacheVal)
				if now.After(val.deadline) {
					t.cache.CompareAndDelete(k, v)
				}
				return true
			})
			select {
			case <-t.ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}
}

type tokenCacheVal struct {
	deadline time.Time
}
