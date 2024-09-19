package main

import (
	"github.com/alex65536/day20/internal/database"
)

type Options struct {
	Addr        string           `toml:"addr"`
	TimeControl string           `toml:"time-control"`
	DB          database.Options `toml:"db"`
}

func (o *Options) FillDefaults() {
	if o.Addr == "" {
		o.Addr = "127.0.0.1:8080"
	}
	if o.TimeControl == "" {
		o.TimeControl = "40/60"
	}
}
