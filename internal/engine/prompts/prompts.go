package prompts

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"
)

//go:embed *.tmpl
var tmplFS embed.FS

var tmpls = template.Must(template.ParseFS(tmplFS, "*.tmpl"))

// Render executes the named template (e.g. "coder.tmpl") with data.
func Render(name string, data any) (string, error) {
	var buf bytes.Buffer
	t := tmpls.Lookup(name)
	if t == nil {
		return "", fmt.Errorf("no template named %q", name)
	}
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
