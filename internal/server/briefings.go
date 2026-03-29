package server

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"
)

//go:embed briefings/*.md
var briefingFS embed.FS

// BriefingContext holds the template variables available to all briefing templates.
type BriefingContext struct {
	ProjectName string
	ProjectID   string
	Goal        string
	Description string
	CaptainName string
}

// renderBriefing loads a markdown template from the embedded briefings directory
// and executes it with the given context. Template files use Go text/template
// syntax (e.g., {{.ProjectName}}).
func renderBriefing(filename string, ctx BriefingContext) (string, error) {
	data, err := briefingFS.ReadFile("briefings/" + filename)
	if err != nil {
		return "", fmt.Errorf("reading briefing template %s: %w", filename, err)
	}

	tmpl, err := template.New(filename).Parse(string(data))
	if err != nil {
		return "", fmt.Errorf("parsing briefing template %s: %w", filename, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("executing briefing template %s: %w", filename, err)
	}

	return buf.String(), nil
}
