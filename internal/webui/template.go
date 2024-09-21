package webui

import (
	"bytes"
	"fmt"
	"html/template"
)

type templator struct {
	cfg  *Config
	tmpl map[string]*template.Template
}

func newTemplator(cfg *Config) *templator {
	return &templator{
		cfg:  cfg,
		tmpl: make(map[string]*template.Template),
	}
}

func (t *templator) makeFuncs() template.FuncMap {
	return template.FuncMap{
		"asURL": func(s string) string {
			return t.cfg.prefix + s
		},
		"asStaticURL": func(s string) string {
			return t.cfg.prefix + s + "?" + t.cfg.opts.ServerID
		},
	}
}

func (t *templator) Has(key string) bool {
	_, ok := t.tmpl[key]
	return ok
}

func (t *templator) Add(key string, names ...string) error {
	files := make([]string, 0, len(names)+1)
	files = append(files, "template/base.html")
	for _, n := range names {
		files = append(files, fmt.Sprintf("template/%v.html", n))
	}
	if _, ok := t.tmpl[key]; ok {
		return fmt.Errorf("template %v already exists", key)
	}
	tmpl, err := template.New(key).Funcs(t.makeFuncs()).ParseFS(templates, files...)
	if err != nil {
		return fmt.Errorf("template %v parse: %w", key, err)
	}
	t.tmpl[key] = tmpl
	return nil
}

func (t *templator) RenderFragment(key, fragment string, data any) ([]byte, error) {
	tmpl, ok := t.tmpl[key]
	if !ok {
		return nil, fmt.Errorf("template %v not found", key)
	}
	var b bytes.Buffer
	if err := tmpl.ExecuteTemplate(&b, fragment, data); err != nil {
		return nil, fmt.Errorf("render %v: %w", key, err)
	}
	return b.Bytes(), nil
}

func (t *templator) Render(key string, data any) ([]byte, error) {
	return t.RenderFragment(key, "base", data)
}
