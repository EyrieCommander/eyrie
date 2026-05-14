package server

import (
	"sort"
	"time"

	"github.com/Audacity88/eyrie/internal/adapter"
	"github.com/Audacity88/eyrie/internal/instance"
)

type commandRoomZeroClaw struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	DisplayName   string `json:"display_name"`
	Status        string `json:"status"`
	HierarchyRole string `json:"hierarchy_role,omitempty"`
	ProjectID     string `json:"project_id,omitempty"`
	ParentID      string `json:"parent_id,omitempty"`
	Port          int    `json:"port"`
	ConfigPath    string `json:"config_path,omitempty"`
	WorkspacePath string `json:"workspace_path,omitempty"`
	CreatedBy     string `json:"created_by,omitempty"`
	HealthStatus  string `json:"health_status,omitempty"`
	LastSeen      string `json:"last_seen,omitempty"`
	Provenance    string `json:"provenance"`
}

func readCommandRoomZeroClawAgents(store *instance.Store) []commandRoomZeroClaw {
	if store == nil {
		return nil
	}
	instances, err := store.List()
	if err != nil {
		return nil
	}
	agents := make([]commandRoomZeroClaw, 0, len(instances))
	for _, inst := range instances {
		if inst.Framework != adapter.FrameworkZeroClaw {
			continue
		}
		agents = append(agents, commandRoomZeroClaw{
			ID:            inst.ID,
			Name:          inst.Name,
			DisplayName:   inst.DisplayName,
			Status:        string(inst.Status),
			HierarchyRole: string(inst.HierarchyRole),
			ProjectID:     inst.ProjectID,
			ParentID:      inst.ParentID,
			Port:          inst.Port,
			ConfigPath:    inst.ConfigPath,
			WorkspacePath: inst.WorkspacePath,
			CreatedBy:     inst.CreatedBy,
			HealthStatus:  inst.HealthStatus,
			LastSeen:      commandRoomTimeString(inst.LastSeen),
			Provenance:    "Eyrie instance metadata",
		})
	}
	sort.Slice(agents, func(i, j int) bool {
		if left, right := commandRoomZeroClawStatusRank(agents[i].Status), commandRoomZeroClawStatusRank(agents[j].Status); left != right {
			return left < right
		}
		if agents[i].HierarchyRole != agents[j].HierarchyRole {
			return agents[i].HierarchyRole < agents[j].HierarchyRole
		}
		return agents[i].Name < agents[j].Name
	})
	return agents
}

func commandRoomZeroClawStatusRank(status string) int {
	switch status {
	case string(instance.StatusRunning):
		return 0
	case string(instance.StatusStarting):
		return 1
	case string(instance.StatusCreated):
		return 2
	case string(instance.StatusStopped):
		return 3
	case string(instance.StatusError):
		return 4
	default:
		return 5
	}
}

func commandRoomTimeString(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
