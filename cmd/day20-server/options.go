package main

import (
	crand "crypto/rand"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/alex65536/day20/internal/database"
	"github.com/alex65536/day20/internal/roomkeeper"
	"github.com/alex65536/day20/internal/userauth"
	"github.com/alex65536/day20/internal/webui"
)

type Options struct {
	Addr        string                  `toml:"addr"`
	TimeControl string                  `toml:"time-control"`
	DB          database.Options        `toml:"db"`
	WebUI       webui.Options           `toml:"webui"`
	RoomKeeper  roomkeeper.Options      `toml:"roomkeeper"`
	Users       userauth.ManagerOptions `toml:"users"`
}

func (o *Options) FillDefaults() {
	if o.Addr == "" {
		o.Addr = "localhost:8080"
	}
	if o.TimeControl == "" {
		o.TimeControl = "40/60"
	}
	o.DB.FillDefaults()
	o.WebUI.FillDefaults()
	o.Users.FillDefaults()
	if o.Users.LinkPrefix == "" {
		o.Users.LinkPrefix = fmt.Sprintf("http://%v/invite/", o.Addr)
	}
}

func (o *Options) MixSecrets(s *Secrets) error {
	var err error
	o.WebUI.Session.Key, err = base64.StdEncoding.DecodeString(s.SessionKey)
	if err != nil {
		return fmt.Errorf("decode session key")
	}
	o.WebUI.CSRFKey, err = base64.StdEncoding.DecodeString(s.CSRFKey)
	if err != nil {
		return fmt.Errorf("decode csrf key")
	}
	return nil
}

type Secrets struct {
	SessionKey string `toml:"session-key"`
	CSRFKey    string `toml:"csrf-key"`
}

func (s *Secrets) GenerateMissing() (changed bool, err error) {
	changed = false
	if s.SessionKey == "" {
		skey := make([]byte, 32)
		_, err = io.ReadFull(crand.Reader, skey)
		if err != nil {
			return changed, fmt.Errorf("generate session key: %w", err)
		}
		changed = true
		s.SessionKey = base64.StdEncoding.EncodeToString(skey)
	}
	if s.CSRFKey == "" {
		ckey := make([]byte, 32)
		_, err = io.ReadFull(crand.Reader, ckey)
		if err != nil {
			return changed, fmt.Errorf("generate csrf key: %w", err)
		}
		changed = true
		s.CSRFKey = base64.StdEncoding.EncodeToString(ckey)
	}
	return changed, nil
}
