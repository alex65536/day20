package webui

import (
	"time"

	"github.com/alex65536/day20/internal/util/timeutil"
)

type humanTimePartData struct {
	Full  string
	Human string
}

func buildHumanTimePartData(now, t time.Time) *humanTimePartData {
	return &humanTimePartData{
		Full:  t.Local().Format(time.RFC1123),
		Human: timeutil.HumanTimeFromBase(now, t.Local()),
	}
}
