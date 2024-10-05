package webui

type errorsPartData struct {
	Errors []string
}

func (errorsPartData) Fragment() string { return "part/errors" }
