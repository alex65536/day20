package main

import (
	"github.com/alex65536/day20/internal/enginemap"
	"github.com/alex65536/day20/internal/util/clone"
)

type Options struct {
	Rooms     int                `toml:"rooms"`
	URL       string             `toml:"url"`
	TokenFile string             `toml:"token-file"`
	Engines   *enginemap.Options `toml:"engines"`
}

func (o Options) Clone() Options {
	o.Engines = clone.Ptr(o.Engines)
	return o
}

func (o *Options) FillDefaults() {
	if o.Rooms == 0 {
		o.Rooms = 1
	}
}
