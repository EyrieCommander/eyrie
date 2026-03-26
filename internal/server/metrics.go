package server

import (
	"net/http"

	"github.com/Audacity88/eyrie/internal/discovery"
	"github.com/Audacity88/eyrie/internal/instance"
	"github.com/Audacity88/eyrie/internal/project"
)

// DashboardMetrics holds aggregate stats for the mission control dashboard.
type DashboardMetrics struct {
	ActiveProjects int `json:"active_projects"`
	PausedProjects int `json:"paused_projects"`
	RunningAgents  int `json:"running_agents"`
	BusyAgents     int `json:"busy_agents"`
	StoppedAgents  int `json:"stopped_agents"`
	TotalInstances int `json:"total_instances"`
}

// handleMetrics returns aggregate dashboard metrics.
// GET /api/metrics
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	var m DashboardMetrics

	// Count projects by status
	if ps, err := project.NewStore(); err == nil {
		if projects, err := ps.List(); err == nil {
			for _, p := range projects {
				switch p.Status {
				case project.PStatusActive:
					m.ActiveProjects++
				case project.PStatusPaused:
					m.PausedProjects++
				}
			}
		}
	}

	// Count agents by state
	disc := s.runDiscovery(r.Context())
	for _, ar := range disc.Agents {
		if ar.Alive {
			m.RunningAgents++
		} else {
			m.StoppedAgents++
		}
	}

	// Count instances + busy agents
	if is, err := instance.NewStore(); err == nil {
		if instances, err := is.List(); err == nil {
			m.TotalInstances = len(instances)
		}
	}

	// Infer busy count from agents with recent LastTask
	for _, ar := range disc.Agents {
		if !ar.Alive {
			continue
		}
		agent := discovery.NewAgent(ar.Agent)
		if st, err := agent.Status(r.Context()); err == nil {
			st.InferBusyState()
			if st.BusyState == "busy" {
				m.BusyAgents++
			}
		}
	}

	writeJSON(w, http.StatusOK, m)
}
