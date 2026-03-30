package server

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed briefings/*.md
var briefingFS embed.FS

// briefingTemplates is parsed once at init from the embedded briefings/*.md
// files. Avoids re-parsing templates on every project chat message.
var briefingTemplates = template.Must(
	template.New("briefings").ParseFS(briefingFS, "briefings/*.md"),
)

// BriefingContext holds the template variables available to all briefing templates.
type BriefingContext struct {
	ProjectName string
	ProjectID   string
	Goal        string
	Description string
	CaptainName string
}

// renderBriefing executes a pre-parsed markdown template with the given context.
func renderBriefing(filename string, ctx BriefingContext) (string, error) {
	// WHY: ParseFS may register templates by base name only ("foo.md") or
	// with the directory prefix ("briefings/foo.md") depending on Go version
	// and embed path. Try the caller's name first, then strip the prefix.
	name := filename
	if strings.HasPrefix(name, "briefings/") {
		name = strings.TrimPrefix(name, "briefings/")
	}

	// Try base name first, then with prefix
	var buf bytes.Buffer
	t := briefingTemplates.Lookup(name)
	if t == nil {
		t = briefingTemplates.Lookup("briefings/" + name)
	}
	if t == nil {
		return "", fmt.Errorf("briefing template %q not found", filename)
	}
	if err := t.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("executing briefing template %s: %w", filename, err)
	}
	return buf.String(), nil
}
