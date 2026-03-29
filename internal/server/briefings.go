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
	// ParseFS registers templates with the full relative path (e.g., "briefings/foo.md").
	// Callers pass just the filename, so normalize here.
	if !strings.HasPrefix(filename, "briefings/") {
		filename = "briefings/" + filename
	}
	var buf bytes.Buffer
	if err := briefingTemplates.ExecuteTemplate(&buf, filename, ctx); err != nil {
		return "", fmt.Errorf("executing briefing template %s: %w", filename, err)
	}
	return buf.String(), nil
}
