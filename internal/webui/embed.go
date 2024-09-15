package webui

import (
	"embed"
	"io/fs"

	"github.com/alex65536/day20/internal/util/mergefs"
)

//go:embed static_contrib
var contribStaticData embed.FS

//go:embed static
var ourStaticData embed.FS

//go:embed template
var templates embed.FS

var staticData fs.FS

func init() {
	contrib, err := fs.Sub(contribStaticData, "static_contrib")
	if err != nil {
		panic(err)
	}
	our, err := fs.Sub(ourStaticData, "static")
	if err != nil {
		panic(err)
	}
	staticData = mergefs.New(contrib, our)
}
