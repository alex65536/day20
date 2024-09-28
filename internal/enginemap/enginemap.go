package enginemap

import (
	"fmt"
	"maps"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/alex65536/day20/internal/battle"
	"github.com/alex65536/day20/internal/roomapi"
	"github.com/alex65536/go-chess/uci"
	"github.com/alex65536/go-chess/util/maybe"
)

type Map interface {
	GetOptions(engine roomapi.JobEngine) (battle.EnginePoolOptions, error)
}

type EngineOptions struct {
	Name                        string         `toml:"name"`
	Args                        []string       `toml:"args"`
	Options                     map[string]any `toml:"options,omitempty"`
	LogEngineString             bool           `toml:"log-engine-string"`
	AllowBadSubstringsInOptions bool           `toml:"allow-bad-substrings-in-options"`
	InitTimeout                 *time.Duration `toml:"init-timeout,omitempty"`
	WaitOnCancelTimeout         *time.Duration `toml:"wait-on-cancel-timeout,omitempty"`
	CreateTimeout               *time.Duration `toml:"create-timeout,omitempty"`
}

func cloneTrivial[T any](a *T) *T {
	if a == nil {
		return nil
	}
	b := *a
	return &b
}

func (o EngineOptions) Clone() EngineOptions {
	o.Args = slices.Clone(o.Args)
	o.Options = maps.Clone(o.Options) // Only primitives and strings are allowed, so OK to shallow copy.
	o.InitTimeout = cloneTrivial(o.InitTimeout)
	o.WaitOnCancelTimeout = cloneTrivial(o.WaitOnCancelTimeout)
	o.CreateTimeout = cloneTrivial(o.CreateTimeout)
	return o
}

func (o EngineOptions) PoolOptions(shortName string) (battle.EnginePoolOptions, error) {
	initTimeout := time.Duration(0)
	if o.InitTimeout != nil {
		initTimeout = *o.InitTimeout
	}

	noWaitOnCancel := false
	waitOnCancelTimeout := time.Duration(0)
	if o.WaitOnCancelTimeout != nil {
		if *o.WaitOnCancelTimeout <= 0 {
			noWaitOnCancel = true
		} else {
			waitOnCancelTimeout = *o.WaitOnCancelTimeout
		}
	}

	createTimeout := maybe.None[time.Duration]()
	if o.CreateTimeout != nil {
		createTimeout = maybe.Some(*o.CreateTimeout)
	}

	var opts map[string]uci.OptValue
	if o.Options != nil {
		opts = make(map[string]uci.OptValue, len(opts))
		for name, opt := range o.Options {
			var newOpt uci.OptValue
			switch v := opt.(type) {
			case bool:
				newOpt = uci.OptValueBool(v)
			case int64:
				newOpt = uci.OptValueInt(v)
			case float64:
				intVal := int64(v)
				if float64(intVal) != v {
					return battle.EnginePoolOptions{}, fmt.Errorf("option %q is number but not int", name)
				}
				newOpt = uci.OptValueInt(intVal)
			case string:
				newOpt = uci.OptValueString(v)
			default:
				return battle.EnginePoolOptions{}, fmt.Errorf("option %q has bad type %T", name, opt)
			}
			opts[name] = newOpt
		}
	}

	return battle.EnginePoolOptions{
		ShortName: shortName,
		ExeName:   o.Name,
		Args:      slices.Clone(o.Args),
		Options:   opts,
		EngineOptions: uci.EngineOptions{
			SanitizeUTF8:                false,
			LogEngineString:             o.LogEngineString,
			AllowBadSubstringsInOptions: o.AllowBadSubstringsInOptions,
			NoWaitOnCancel:              noWaitOnCancel,
			InitTimeout:                 initTimeout,
			WaitOnCancelTimeout:         waitOnCancelTimeout,
		},
		CreateTimeout: createTimeout,
	}, nil
}

type Options struct {
	// Allows all the executables found in PATH to be run as chess engines as fallback.
	// SECURITY: May lead to remote code execution. Use only if you COMPLETELY TRUST THE SERVER AND
	// ALL ITS USERS WHO ARE ALLOWED TO RUN ENGINE CONTESTS.
	AllowPathDangerous bool `toml:"allow-path-dangerous"`

	// Allows all the executable from the given DIRs to be run as chess engines.
	// SECURITY: The server can execute ANY FILE from the provided dirs. Use with EXTREME CARE.
	AllowDirs []string `toml:"allow-dirs"`

	// Default options for engines found with AllowPathDangerous or AllowDirs.
	Default EngineOptions `toml:"default"`

	// Maps engine names to engine options.
	Engines map[string]EngineOptions `toml:"engines"`
}

func (o Options) Clone() Options {
	o.AllowDirs = slices.Clone(o.AllowDirs)
	o.Default = o.Default.Clone()
	if o.Engines != nil {
		o.Engines = maps.Clone(o.Engines)
		for k, v := range o.Engines {
			o.Engines[k] = v.Clone()
		}
	}
	return o
}

func New(o Options) Map {
	return &theMap{o: o.Clone()}
}

type theMap struct {
	o Options
}

func sanitizeEngineName(name string) bool {
	if name == "" || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") {
		return false
	}
	for i := range len(name) {
		if b := name[i]; ('a' <= b && b <= 'z') ||
			('A' <= b && b <= 'Z') ||
			('0' <= b && b <= '9') ||
			b == '_' || b == '-' || b == '.' {
			continue
		}
		return false
	}
	return true
}

func (m *theMap) GetOptions(engine roomapi.JobEngine) (battle.EnginePoolOptions, error) {
	if !sanitizeEngineName(engine.Name) {
		return battle.EnginePoolOptions{}, fmt.Errorf("bad engine name: %q", engine.Name)
	}

	if m.o.Engines != nil {
		if e, ok := m.o.Engines[engine.Name]; ok {
			res, err := e.PoolOptions(engine.Name)
			if err != nil {
				return battle.EnginePoolOptions{}, fmt.Errorf("create pool options: %w", err)
			}
			return res, nil
		}
	}

	for _, dir := range m.o.AllowDirs {
		if dir == "" {
			dir = "."
		}
		fname, err := exec.LookPath(filepath.Join(dir, engine.Name))
		if err != nil {
			continue
		}
		res, err := m.o.Default.PoolOptions(engine.Name)
		if err != nil {
			return battle.EnginePoolOptions{}, fmt.Errorf("create pool options: %w", err)
		}
		res.ExeName = fname
		return res, nil
	}

	if m.o.AllowPathDangerous {
		fname, err := exec.LookPath(engine.Name)
		if err != nil {
			return battle.EnginePoolOptions{}, fmt.Errorf("engine not found: %q", engine.Name)
		}
		res, err := m.o.Default.PoolOptions(engine.Name)
		if err != nil {
			return battle.EnginePoolOptions{}, fmt.Errorf("create pool options: %w", err)
		}
		res.ExeName = fname
		return res, nil
	}

	return battle.EnginePoolOptions{}, fmt.Errorf("engine not found: %q", engine.Name)
}
