package server

import (
	"bufio"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Audacity88/eyrie/internal/config"
	"gopkg.in/yaml.v3"
)

type meshStatusResponse struct {
	Available       bool                `json:"available"`
	Root            string              `json:"root,omitempty"`
	ManifestPath    string              `json:"manifest_path,omitempty"`
	Updated         string              `json:"updated,omitempty"`
	Status          string              `json:"status,omitempty"`
	Project         string              `json:"project,omitempty"`
	ProjectID       string              `json:"project_id,omitempty"`
	Owner           string              `json:"owner,omitempty"`
	ParentAgent     meshAgentSummary    `json:"parent_agent"`
	Subordinates    []meshAgentSummary  `json:"subordinates"`
	Channels        meshChannelSummary  `json:"channels"`
	Inboxes         []meshInboxSummary  `json:"inboxes"`
	LatestOutbox    *meshNoticeSummary  `json:"latest_outbox,omitempty"`
	Reports         []meshReportSummary `json:"reports"`
	CommanderRefs   []meshCommanderRef  `json:"commander_refs"`
	GeneratedAt     string              `json:"generated_at"`
	UnavailableText string              `json:"unavailable_text,omitempty"`
}

type meshAgentSummary struct {
	ID               string `json:"id" yaml:"id"`
	DisplayName      string `json:"display_name" yaml:"display_name"`
	PlannedFramework string `json:"planned_framework" yaml:"planned_framework"`
	Role             string `json:"role" yaml:"role"`
	Inbox            string `json:"inbox,omitempty" yaml:"inbox"`
}

type meshChannelSummary struct {
	Broadcasts  string `json:"broadcasts,omitempty" yaml:"broadcasts"`
	ParentInbox string `json:"parent_inbox,omitempty" yaml:"parent_inbox"`
	Outbox      string `json:"outbox,omitempty" yaml:"outbox"`
	Reports     string `json:"reports,omitempty" yaml:"reports"`
	DocsInbox   string `json:"docs_inbox,omitempty"`
	DanyaInbox  string `json:"danya_inbox,omitempty"`
	MagnusInbox string `json:"magnus_inbox,omitempty"`
}

type meshInboxSummary struct {
	Recipient string              `json:"recipient"`
	Path      string              `json:"path"`
	Updated   string              `json:"updated,omitempty"`
	Total     int                 `json:"total"`
	Open      int                 `json:"open"`
	Pending   int                 `json:"pending_acknowledgements"`
	Notices   []meshNoticeSummary `json:"notices"`
}

type meshNoticeSummary struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind,omitempty"`
	Title       string   `json:"title,omitempty"`
	Created     string   `json:"created,omitempty"`
	From        string   `json:"from,omitempty"`
	To          []string `json:"to,omitempty"`
	Parent      string   `json:"parent,omitempty"`
	Status      string   `json:"status,omitempty"`
	Priority    string   `json:"priority,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	Request     string   `json:"request,omitempty"`
	Deliverable string   `json:"deliverable,omitempty"`
	Response    string   `json:"response,omitempty"`
	SourcePath  string   `json:"source_path,omitempty"`
}

type meshReportSummary struct {
	Path       string `json:"path"`
	Title      string `json:"title"`
	ModifiedAt string `json:"modified_at"`
}

type meshCommanderRef struct {
	Path   string `json:"path"`
	Notice string `json:"notice,omitempty"`
	Source string `json:"source,omitempty"`
}

type meshManifestFile struct {
	Updated      string             `yaml:"updated"`
	Status       string             `yaml:"status"`
	Project      string             `yaml:"project"`
	ProjectID    string             `yaml:"project_id"`
	Owner        string             `yaml:"owner"`
	ParentAgent  meshAgentSummary   `yaml:"parent_agent"`
	Subordinates []meshAgentSummary `yaml:"subordinates"`
	Channels     meshChannelSummary `yaml:"channels"`
}

type meshInboxFile struct {
	Updated   string             `yaml:"updated"`
	Recipient string             `yaml:"recipient"`
	Notices   []meshNoticeRecord `yaml:"notices"`
}

type meshOutboxFile struct {
	Updated string             `yaml:"updated"`
	Owner   string             `yaml:"owner"`
	Entries []meshNoticeRecord `yaml:"entries"`
}

type meshNoticeRecord struct {
	ID               string                    `yaml:"id"`
	Kind             string                    `yaml:"kind"`
	Title            string                    `yaml:"title"`
	Created          string                    `yaml:"created"`
	From             string                    `yaml:"from"`
	To               []string                  `yaml:"to"`
	Parent           string                    `yaml:"parent"`
	Status           string                    `yaml:"status"`
	Priority         string                    `yaml:"priority"`
	Summary          string                    `yaml:"summary"`
	Request          string                    `yaml:"request"`
	Deliverable      string                    `yaml:"deliverable"`
	Response         string                    `yaml:"response"`
	ContextRefs      []string                  `yaml:"context_refs"`
	Acknowledgements []meshAcknowledgementInfo `yaml:"acknowledgements"`
}

type meshAcknowledgementInfo struct {
	Agent  string `yaml:"agent"`
	Status string `yaml:"status"`
}

func (s *Server) handleMeshStatus(w http.ResponseWriter, r *http.Request) {
	status, err := readMeshStatus()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func readMeshStatus() (meshStatusResponse, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	root, err := locateMeshRoot()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return meshStatusResponse{
				Available:       false,
				GeneratedAt:     now,
				UnavailableText: "No local agent mesh is configured. Set EYRIE_AGENT_MESH_DIR or [mesh].agent_mesh_dir in ~/.eyrie/config.toml to a private mesh directory.",
			}, nil
		}
		return meshStatusResponse{}, err
	}

	manifestPath := filepath.Join(root, "manifest.yaml")
	var manifest meshManifestFile
	if err := readYAMLFile(manifestPath, &manifest); err != nil {
		return meshStatusResponse{}, fmt.Errorf("read mesh manifest: %w", err)
	}

	channels := manifest.Channels
	channels.MagnusInbox = channels.ParentInbox
	for _, agent := range manifest.Subordinates {
		switch agent.ID {
		case "danya.eyrie":
			channels.DanyaInbox = agent.Inbox
		case "docs.eyrie":
			channels.DocsInbox = agent.Inbox
		}
	}

	inboxPaths := []string{channels.ParentInbox}
	for _, agent := range manifest.Subordinates {
		inboxPaths = append(inboxPaths, agent.Inbox)
	}
	inboxPaths = uniqueNonEmpty(inboxPaths)
	inboxes := make([]meshInboxSummary, 0, len(inboxPaths))
	for _, path := range inboxPaths {
		inbox, err := readMeshInbox(path)
		if err != nil {
			return meshStatusResponse{}, err
		}
		inboxes = append(inboxes, inbox)
	}

	latestOutbox, err := readLatestOutboxEntry(channels.Outbox)
	if err != nil {
		return meshStatusResponse{}, err
	}

	reports, err := readMeshReports(channels.Reports)
	if err != nil {
		return meshStatusResponse{}, err
	}

	commanderRefs, err := readCommanderRefs(root)
	if err != nil {
		return meshStatusResponse{}, err
	}

	return meshStatusResponse{
		Available:     true,
		Root:          root,
		ManifestPath:  manifestPath,
		Updated:       manifest.Updated,
		Status:        manifest.Status,
		Project:       manifest.Project,
		ProjectID:     manifest.ProjectID,
		Owner:         manifest.Owner,
		ParentAgent:   manifest.ParentAgent,
		Subordinates:  manifest.Subordinates,
		Channels:      channels,
		Inboxes:       inboxes,
		LatestOutbox:  latestOutbox,
		Reports:       reports,
		CommanderRefs: commanderRefs,
		GeneratedAt:   now,
	}, nil
}

func locateMeshRoot() (string, error) {
	if override := strings.TrimSpace(os.Getenv("EYRIE_AGENT_MESH_DIR")); override != "" {
		if fileExists(filepath.Join(override, "manifest.yaml")) {
			return filepath.Clean(override), nil
		}
		return "", os.ErrNotExist
	}

	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	if configured := strings.TrimSpace(cfg.Mesh.AgentMeshDir); configured != "" {
		configured = config.ExpandHome(configured)
		if fileExists(filepath.Join(configured, "manifest.yaml")) {
			return filepath.Clean(configured), nil
		}
		return "", os.ErrNotExist
	}

	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(wd, "docs", "agent-mesh")
		if fileExists(filepath.Join(candidate, "manifest.yaml")) {
			return candidate, nil
		}
		if filepath.Base(wd) == "agent-mesh" && fileExists(filepath.Join(wd, "manifest.yaml")) {
			return wd, nil
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			break
		}
		wd = parent
	}
	return "", os.ErrNotExist
}

func readMeshInbox(path string) (meshInboxSummary, error) {
	var file meshInboxFile
	if err := readYAMLFile(path, &file); err != nil {
		return meshInboxSummary{}, fmt.Errorf("read mesh inbox %s: %w", path, err)
	}
	notices := make([]meshNoticeSummary, 0, len(file.Notices))
	open := 0
	pending := 0
	for _, notice := range file.Notices {
		summary := notice.toSummary(path)
		notices = append(notices, summary)
		if isOpenMeshStatus(summary.Status) {
			open++
		}
		for _, ack := range notice.Acknowledgements {
			if strings.EqualFold(ack.Status, "pending") {
				pending++
			}
		}
	}
	return meshInboxSummary{
		Recipient: file.Recipient,
		Path:      path,
		Updated:   file.Updated,
		Total:     len(file.Notices),
		Open:      open,
		Pending:   pending,
		Notices:   notices,
	}, nil
}

func readLatestOutboxEntry(path string) (*meshNoticeSummary, error) {
	if strings.TrimSpace(path) == "" || !fileExists(path) {
		return nil, nil
	}
	var file meshOutboxFile
	if err := readYAMLFile(path, &file); err != nil {
		return nil, fmt.Errorf("read mesh outbox %s: %w", path, err)
	}
	if len(file.Entries) == 0 {
		return nil, nil
	}
	latest := file.Entries[len(file.Entries)-1].toSummary(path)
	return &latest, nil
}

func readMeshReports(path string) ([]meshReportSummary, error) {
	if strings.TrimSpace(path) == "" || !dirExists(path) {
		return nil, nil
	}
	matches, err := filepath.Glob(filepath.Join(path, "*.md"))
	if err != nil {
		return nil, err
	}
	reports := make([]meshReportSummary, 0, len(matches))
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			return nil, err
		}
		title, err := readMarkdownTitle(match)
		if err != nil {
			return nil, err
		}
		reports = append(reports, meshReportSummary{
			Path:       match,
			Title:      title,
			ModifiedAt: info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	sort.Slice(reports, func(i, j int) bool {
		return reports[i].ModifiedAt > reports[j].ModifiedAt
	})
	return reports, nil
}

func readCommanderRefs(root string) ([]meshCommanderRef, error) {
	pattern := regexp.MustCompile("`?(/Users/dan/Documents/Personal/Commander/Shared/notices/[A-Za-z0-9_.\\-/]+\\.yaml)`?(?::\\s*`([A-Za-z0-9_.-]+)`)?")
	refsByKey := map[string]meshCommanderRef{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".md" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range pattern.FindAllStringSubmatch(string(data), -1) {
			ref := meshCommanderRef{Path: match[1], Source: path}
			if len(match) > 2 {
				ref.Notice = match[2]
			}
			key := ref.Path + "#" + ref.Notice
			refsByKey[key] = ref
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	refs := make([]meshCommanderRef, 0, len(refsByKey))
	for _, ref := range refsByKey {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Path == refs[j].Path {
			return refs[i].Notice < refs[j].Notice
		}
		return refs[i].Path < refs[j].Path
	})
	return refs, nil
}

func (n meshNoticeRecord) toSummary(sourcePath string) meshNoticeSummary {
	return meshNoticeSummary{
		ID:          n.ID,
		Kind:        n.Kind,
		Title:       n.Title,
		Created:     n.Created,
		From:        n.From,
		To:          n.To,
		Parent:      n.Parent,
		Status:      n.Status,
		Priority:    n.Priority,
		Summary:     n.Summary,
		Request:     n.Request,
		Deliverable: n.Deliverable,
		Response:    n.Response,
		SourcePath:  sourcePath,
	}
}

func isOpenMeshStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "open", "pending", "must_handle":
		return true
	case "answered", "acknowledged", "routed", "sent", "closed", "done", "complete", "completed":
		return false
	default:
		return true
	}
}

func readYAMLFile(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, target)
}

func readMarkdownTitle(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# ")), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return filepath.Base(path), nil
}

func uniqueNonEmpty(values []string) []string {
	seen := map[string]struct{}{}
	var result []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
