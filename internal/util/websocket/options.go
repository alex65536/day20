package websocket

import (
	"time"

	"github.com/gorilla/websocket"
)

type Options struct {
	ReadSize      int
	WriteSize     int
	WriteDeadline time.Duration
	PingInterval  time.Duration
	PingTimeout   time.Duration
	ReadMsgLimit  int64
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
