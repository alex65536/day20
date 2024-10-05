package main

import (
	crand "crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/alex65536/day20/internal/database"
	"github.com/alex65536/day20/internal/roomkeeper"
	"github.com/alex65536/day20/internal/scheduler"
	"github.com/alex65536/day20/internal/userauth"
	"github.com/alex65536/day20/internal/webui"
)

type HTTPSOptions struct {
	Port                 uint16   `toml:"port"`
	ExposeInsecure       bool     `toml:"expose-insecure"`
	AllowedSecureDomains []string `toml:"allowed-secure-domains"`
	CachePath            string   `toml:"cache-path"`
}

func (o *HTTPSOptions) FillDefaults() {}

type Options struct {
	Addr         string                       `toml:"addr"`
	Port         uint16                       `toml:"port"`
	Host         string                       `toml:"host"`
	DB           database.Options             `toml:"db"`
	WebUI        webui.Options                `toml:"webui"`
	RoomKeeper   roomkeeper.Options           `toml:"roomkeeper"`
	Users        userauth.ManagerOptions      `toml:"users"`
	Scheduler    scheduler.Options            `toml:"scheduler"`
	TokenChecker userauth.TokenCheckerOptions `toml:"token-checker"`
	SecretsPath  string                       `toml:"secrets-path"`
	HTTPS        *HTTPSOptions                `toml:"https"`
}

func (o *Options) urlRoot() string {
	schema := "http"
	if o.HTTPS != nil {
		schema = "https"
	}
	return fmt.Sprintf("%v://%v", schema, o.Host)
}

func (o *Options) SecureAddrWithPort() string {
	if o.HTTPS == nil {
		panic("no https")
	}
	port := o.HTTPS.Port
	if port == 0 {
		port = 443
	}
	return fmt.Sprintf("%v:%v", o.Addr, port)
}

func (o *Options) AddrWithPort() string {
	port := o.Port
	if port == 0 {
		port = 80
	}
	return fmt.Sprintf("%v:%v", o.Addr, port)
}

func (o *Options) FillDefaults() {
	if o.Addr == "" {
		o.Addr = "localhost"
		if o.Port == 0 {
			o.Port = 8080
		}
	}
	if o.Host == "" {
		o.Host = o.AddrWithPort()
	}
	o.DB.FillDefaults()
	o.WebUI.FillDefaults()
	o.RoomKeeper.FillDefaults()
	o.Users.FillDefaults()
	o.Scheduler.FillDefaults()
	if o.Users.LinkPrefix == "" {
		o.Users.LinkPrefix = o.urlRoot() + "/invite/"
	}
	o.TokenChecker.FillDefaults()
	if o.HTTPS != nil {
		o.HTTPS.FillDefaults()
	}
}

func (o *Options) MixSecretsFromFile() error {
	rawSecrets, err := os.ReadFile(o.SecretsPath)
	if err != nil {
		rawSecrets = nil
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read secrets: %w", err)
		}
	}
	var secrets Secrets
	if rawSecrets != nil {
		if err := toml.Unmarshal(rawSecrets, &secrets); err != nil {
			return fmt.Errorf("unmarshal secrets")
		}
	}
	secretsChanged, err := secrets.GenerateMissing()
	if err != nil {
		return fmt.Errorf("generate secrets: %w", err)
	}
	if secretsChanged {
		newRawSecrets, err := toml.Marshal(&secrets)
		if err != nil {
			return fmt.Errorf("marshal secrets")
		}
		if err := os.WriteFile(o.SecretsPath, newRawSecrets, 0600); err != nil {
			return fmt.Errorf("write secrets: %w", err)
		}
	}
	if err := o.MixSecrets(&secrets); err != nil {
		return fmt.Errorf("mix secrets: %w", err)
	}
	return nil
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
