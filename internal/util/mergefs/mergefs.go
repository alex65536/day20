package mergefs

import (
	"errors"
	"io/fs"
)

type mergeFS struct{ subs []fs.FS }

func (f *mergeFS) Open(name string) (fs.File, error) {
	for _, sub := range f.subs {
		f, err := sub.Open(name)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, err
		}
		return f, err
	}
	return nil, fs.ErrNotExist
}

func New(subs ...fs.FS) fs.FS {
	return &mergeFS{subs: subs}
}
