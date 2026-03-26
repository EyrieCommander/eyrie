package project

import (
	"fmt"
	"strings"
)

// TeamMember represents an agent participating in a project, used when
// rendering PROJECT.md context files for agent workspaces.
type TeamMember struct {
	Name        string
	DisplayName string
	Role        string // "captain" or "talon"
	Description string // persona description (optional)
	Framework   string
}

// RenderProjectMD generates the content of a PROJECT.md file that gives
// agents context about their project and team. This file is written to
// each project agent's workspace and refreshed when the team or project
// details change.
func RenderProjectMD(proj Project, members []TeamMember) string {
	var b strings.Builder

	b.WriteString("# PROJECT.md\n\n")
	b.WriteString(fmt.Sprintf("**Project:** %s\n", proj.Name))
	if proj.Goal != "" {
		b.WriteString(fmt.Sprintf("**Goal:** %s\n", proj.Goal))
	}
	if proj.Description != "" {
		b.WriteString(fmt.Sprintf("**Description:** %s\n", proj.Description))
	}
	b.WriteString(fmt.Sprintf("**Status:** %s\n", proj.Status))
	if proj.Progress > 0 {
		b.WriteString(fmt.Sprintf("**Progress:** %d%%\n", proj.Progress))
	}
	if proj.Deadline != nil {
		b.WriteString(fmt.Sprintf("**Deadline:** %s\n", proj.Deadline.Format("2006-01-02")))
	}
	b.WriteString(fmt.Sprintf("**Project ID:** %s\n", proj.ID))

	if len(members) > 0 {
		b.WriteString("\n## Team\n\n")
		for _, m := range members {
			line := fmt.Sprintf("- **%s** (%s, %s)", m.DisplayName, m.Role, m.Framework)
			if m.Description != "" {
				line += " — " + m.Description
			}
			b.WriteString(line + "\n")
		}
	}

	b.WriteString("\n---\n*This file is maintained by Eyrie and updated when the project team or goals change.*\n")

	return b.String()
}
