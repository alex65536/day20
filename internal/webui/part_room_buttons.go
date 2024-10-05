package webui

import (
	"html/template"
)

type roomButtonsPartData struct {
	RoomID    string
	Active    bool
	AJAXAttrs template.HTMLAttr
}
