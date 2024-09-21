package websockutil

import (
	"time"

	"github.com/gorilla/websocket"
)

type Options struct {
	ReadSize      int           `toml:"read-size"`
	WriteSize     int           `toml:"write-size"`
	WriteDeadline time.Duration `toml:"write-deadline"`
	PingInterval  time.Duration `toml:"ping-interval"`
	PingTimeout   time.Duration `toml:"ping-timeout"`
	ReadMsgLimit  int64         `toml:"read-msg-limit"`
}

func (o *Options) FillDefaults() {
	if o.ReadSize == 0 {
		o.ReadSize = 2048
	}
	if o.WriteSize == 0 {
		o.WriteSize = 2048
	}
	if o.WriteDeadline == 0 {
		o.WriteDeadline = 30 * time.Second
	}
	if o.PingInterval == 0 {
		o.PingInterval = 30 * time.Second
	}
	if o.PingTimeout == 0 {
		o.PingTimeout = 1 * time.Minute
	}
	if o.ReadMsgLimit == 0 {
		o.ReadMsgLimit = 32768
	}
}

func (o *Options) Upgrader() websocket.Upgrader {
	return websocket.Upgrader{
		ReadBufferSize:  o.ReadSize,
		WriteBufferSize: o.WriteSize,
	}
}
