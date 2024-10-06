package webui

import (
	"fmt"
	"html/template"
	"io/fs"
	"math"
	"strconv"
	"strings"

	"github.com/alex65536/day20/internal/util/human"
	"github.com/lucasb-eyer/go-colorful"
)

type templator struct {
	tmpl   map[string]*template.Template
	common *template.Template
}

func parseTemplate(t *template.Template, fileName string) error {
	data, err := templates.ReadFile(fileName)
	if err != nil {
		return fmt.Errorf("read file %q: %w", fileName, err)
	}
	if _, err := t.Parse(string(data)); err != nil {
		return fmt.Errorf("parse file %q: %w", fileName, err)
	}
	return nil
}

func parseCommonTemplate(cfg *Config) (*template.Template, error) {
	t := template.New("base").Funcs(template.FuncMap{
		"asURL": func(s string) string {
			return cfg.prefix + s
		},
		"asStaticURL": func(s string) string {
			return cfg.prefix + s + "?" + cfg.opts.ServerID
		},
		"mixColors": func(ha, hb string, ratio float64) (string, error) {
			a, err := colorful.Hex(ha)
			if err != nil {
				return "", fmt.Errorf("parse first: %w", err)
			}
			b, err := colorful.Hex(hb)
			if err != nil {
				return "", fmt.Errorf("parse second: %w", err)
			}
			return a.BlendHcl(b, ratio).Clamped().Hex(), nil
		},
		"fmtFloatWithInf": func(prec int, f float64) string {
			if math.IsInf(f, +1) {
				return "+∞"
			}
			if math.IsInf(f, -1) {
				return "-∞"
			}
			return strconv.FormatFloat(f, 'f', prec, 64)
		},
		"humanInt64": func(prec int, v int64) string {
			return human.Int(v, prec)
		},
	})
	if err := parseTemplate(t, "template/layout/base.html"); err != nil {
		return nil, err
	}
	if err := fs.WalkDir(templates, "template/part", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() && !strings.HasSuffix(name, ".html") {
			return nil
		}
		subT := t.New(strings.TrimPrefix(strings.TrimSuffix(name, ".html"), "template/"))
		if err := parseTemplate(subT, name); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("walk: %w", err)
	}
	return t, nil
}

func newTemplator(cfg *Config) (*templator, error) {
	common, err := parseCommonTemplate(cfg)
	if err != nil {
		return nil, fmt.Errorf("parse common template: %w", err)
	}
	return &templator{
		tmpl:   make(map[string]*template.Template),
		common: common,
	}, nil
}

func (t *templator) Get(name string) (*template.Template, error) {
	if tmpl, ok := t.tmpl[name]; ok {
		return tmpl, nil
	}
	tmpl, err := t.common.Clone()
	if err != nil {
		return nil, fmt.Errorf("clone: %w", err)
	}
	if name != "" {
		subT := tmpl.New(name)
		if err := parseTemplate(subT, "template/"+name+".html"); err != nil {
			return nil, fmt.Errorf("parse: %w", err)
		}
	}
	t.tmpl[name] = tmpl
	return tmpl, nil
}
