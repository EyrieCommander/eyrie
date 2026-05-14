package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type commandRoomResponse struct {
	GeneratedAt      string                  `json:"generated_at"`
	Mesh             meshStatusResponse      `json:"mesh"`
	Board            *commandRoomBoard       `json:"board,omitempty"`
	RuntimeRegistry  []commandRoomRuntime    `json:"runtime_registry"`
	DevelopmentMesh  *commandRoomDevelopment `json:"development_mesh,omitempty"`
	ZeroClawAgents   []commandRoomZeroClaw   `json:"zeroclaw_agents"`
	DataSources      []commandRoomDataSource `json:"data_sources"`
	ApprovalBoundary []string                `json:"approval_boundary"`
}

type commandRoomBoard struct {
	Path        string                 `json:"path"`
	GeneratedAt string                 `json:"generated_at,omitempty"`
	Captain     string                 `json:"captain,omitempty"`
	Domain      string                 `json:"domain,omitempty"`
	Items       []commandRoomBoardItem `json:"items"`
}

type commandRoomBoardItem struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	Status           string `json:"status"`
	Priority         string `json:"priority"`
	Lane             string `json:"lane"`
	Owner            string `json:"owner"`
	PrimaryAgent     string `json:"primary_agent"`
	Summary          string `json:"summary"`
	NextAction       string `json:"next_action"`
	CommanderVisible bool   `json:"commander_visible"`
	Source           string `json:"source,omitempty"`
	LinkedItemRef    string `json:"linked_item_ref,omitempty"`
}

type commandRoomRuntime struct {
	ID                string `json:"id"`
	DisplayName       string `json:"display_name"`
	Status            string `json:"status"`
	ParentAgent       string `json:"parent_agent"`
	OwningDomain      string `json:"owning_domain"`
	Role              string `json:"role"`
	Framework         string `json:"framework"`
	Transport         string `json:"transport"`
	Workspace         string `json:"workspace,omitempty"`
	CurrentAssignment string `json:"current_assignment,omitempty"`
	Path              string `json:"path"`
}

type commandRoomDataSource struct {
	Label  string `json:"label"`
	Path   string `json:"path,omitempty"`
	Status string `json:"status"`
}

func (s *Server) handleCommandRoom(w http.ResponseWriter, r *http.Request) {
	mesh, err := readMeshStatus()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var board *commandRoomBoard
	var runtimes []commandRoomRuntime
	var sources []commandRoomDataSource

	if mesh.Available {
		boardPath := filepath.Join(projectMeshOwnerRoot(mesh.Root), "status", "eyrie-command-board.json")
		board = readCommandRoomBoard(boardPath)
		sources = append(sources, commandRoomDataSource{
			Label:  "captain board",
			Path:   boardPath,
			Status: availabilityStatus(board != nil),
		})

		registryPath := mesh.Channels.RuntimeRegistry
		if registryPath == "" {
			registryPath = filepath.Join(projectMeshOwnerRoot(mesh.Root), "docs", "runtime-registry")
		}
		runtimes = readCommandRoomRuntimes(registryPath)
		sources = append(sources, commandRoomDataSource{
			Label:  "runtime registry",
			Path:   registryPath,
			Status: availabilityStatus(len(runtimes) > 0),
		})
	}

	sources = append(sources, commandRoomDataSource{
		Label:  "agent mesh",
		Path:   mesh.Root,
		Status: availabilityStatus(mesh.Available),
	})

	developmentRoot := locateCommandRoomDevelopmentMeshRoot()
	developmentMesh := readCommandRoomDevelopmentMesh(developmentRoot)
	sources = append(sources, commandRoomDataSource{
		Label:  "development mesh",
		Path:   developmentRoot,
		Status: availabilityStatus(developmentMesh != nil),
	})
	zeroClawAgents := readCommandRoomZeroClawAgents(s.instanceStore)

	writeJSON(w, http.StatusOK, commandRoomResponse{
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
		Mesh:            mesh,
		Board:           board,
		RuntimeRegistry: runtimes,
		DevelopmentMesh: developmentMesh,
		ZeroClawAgents:  zeroClawAgents,
		DataSources:     sources,
		ApprovalBoundary: []string{
			"read-only file-backed surface",
			"no runtime launch or session control",
			"no credential or runtime-home mutation",
			"no GitHub, public, push, deploy, email, or external mutation",
		},
	})
}

func projectMeshOwnerRoot(meshRoot string) string {
	if filepath.Base(meshRoot) == "agent-mesh" && filepath.Base(filepath.Dir(meshRoot)) == "docs" {
		return filepath.Dir(filepath.Dir(meshRoot))
	}
	return filepath.Dir(meshRoot)
}

func availabilityStatus(ok bool) string {
	if ok {
		return "available"
	}
	return "missing"
}

func readCommandRoomBoard(path string) *commandRoomBoard {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw struct {
		GeneratedAt string                 `json:"generated_at"`
		Captain     string                 `json:"captain"`
		Domain      string                 `json:"domain"`
		Items       []commandRoomBoardItem `json:"items"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	return &commandRoomBoard{
		Path:        path,
		GeneratedAt: raw.GeneratedAt,
		Captain:     raw.Captain,
		Domain:      raw.Domain,
		Items:       raw.Items,
	}
}

func readCommandRoomRuntimes(dir string) []commandRoomRuntime {
	if strings.TrimSpace(dir) == "" || !dirExists(dir) {
		return nil
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil
	}
	runtimes := make([]commandRoomRuntime, 0, len(matches))
	for _, match := range matches {
		var raw map[string]any
		if err := readYAMLFile(match, &raw); err != nil {
			continue
		}
		runtimes = append(runtimes, commandRoomRuntime{
			ID:                stringField(raw, "runtime_id"),
			DisplayName:       stringField(raw, "display_name"),
			Status:            stringField(raw, "status"),
			ParentAgent:       stringField(raw, "parent_agent"),
			OwningDomain:      stringField(raw, "owning_domain"),
			Role:              stringField(raw, "role"),
			Framework:         stringField(raw, "framework"),
			Transport:         nestedStringField(raw, "transport", "primary"),
			Workspace:         stringField(raw, "workspace"),
			CurrentAssignment: stringField(raw, "current_assignment"),
			Path:              match,
		})
	}
	sort.Slice(runtimes, func(i, j int) bool {
		return runtimes[i].ID < runtimes[j].ID
	})
	return runtimes
}

func stringField(raw map[string]any, key string) string {
	if value, ok := raw[key].(string); ok {
		return value
	}
	return ""
}

func nestedStringField(raw map[string]any, key string, nested string) string {
	value, ok := raw[key].(map[string]any)
	if !ok {
		return ""
	}
	if text, ok := value[nested].(string); ok {
		return text
	}
	return ""
}
