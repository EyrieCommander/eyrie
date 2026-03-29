package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/Audacity88/eyrie/internal/config"
	"github.com/Audacity88/eyrie/internal/registry"
)

// installState tracks ongoing installations
type installState struct {
	mu          sync.RWMutex
	current     map[string]*installProgress
	stateFile   string
}

type installProgress struct {
	FrameworkID string    `json:"framework_id"`
	Phase       string    `json:"phase"`
	Status      string    `json:"status"` // "running", "success", "error", "stale"
	Progress    int       `json:"progress"` // 0-100
	Message     string    `json:"message"`
	Error       string    `json:"error,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	PID         int       `json:"pid,omitempty"` // Process ID of install command

	// Log buffer for streaming
	logBuf []string `json:"-"` // Store logs for clients that connect later
	logMu  sync.Mutex `json:"-"`
}

var globalInstallState = &installState{
	current: make(map[string]*installProgress),
}

// addLog adds a log line to the progress
func (p *installProgress) addLog(line string) {
	p.logMu.Lock()
	defer p.logMu.Unlock()
	p.logBuf = append(p.logBuf, line)
}

// getLogs returns all logs
func (p *installProgress) getLogs() []string {
	p.logMu.Lock()
	defer p.logMu.Unlock()
	return append([]string{}, p.logBuf...)
}

func init() {
	// Set state file path
	home, _ := os.UserHomeDir()
	globalInstallState.stateFile = home + "/.eyrie/install_status.json"

	// Load existing state
	globalInstallState.load()
}

func (s *installState) load() {
	s.mu.Lock()

	data, err := os.ReadFile(s.stateFile)
	if err != nil {
		s.mu.Unlock()
		return
	}

	json.Unmarshal(data, &s.current)

	// Check for stale installations (process died but status still "running")
	needsSave := false
	for _, progress := range s.current {
		if progress.Status == "running" && progress.PID > 0 {
			if !isProcessAlive(progress.PID) {
				progress.Status = "error"
				progress.Error = "installation process died unexpectedly"
				progress.Message = "Installation interrupted"
				now := time.Now()
				progress.CompletedAt = &now
				needsSave = true
			}
		}
	}

	s.mu.Unlock()

	// Save after unlocking if we detected stale installs
	if needsSave {
		s.save()
	}
}

func (s *installState) save() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, _ := json.MarshalIndent(s.current, "", "  ")
	os.MkdirAll(config.ExpandHome("~/.eyrie"), 0755)
	os.WriteFile(s.stateFile, data, 0644)
}

func (s *installState) set(id string, progress *installProgress) {
	s.mu.Lock()
	if progress == nil {
		delete(s.current, id)
	} else {
		s.current[id] = progress
	}
	s.mu.Unlock()
	s.save()
}

func (s *installState) get(id string) *installProgress {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current[id]
}

func (s *installState) getAll() map[string]*installProgress {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]*installProgress)
	for k, v := range s.current {
		result[k] = v
	}
	return result
}

// isProcessAlive checks if a process with the given PID is still running
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	// Try to find the process
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// Send signal 0 to check if process exists (doesn't actually signal it)
	err = process.Signal(os.Signal(nil))
	return err == nil
}

type frameworkWithStatus struct {
	registry.Framework
	Installed bool `json:"installed"`
}

// handleListFrameworks returns all frameworks from the registry with installation status
func (s *Server) handleListFrameworks(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	client, err := registry.NewClient("")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// WHY refresh param: The registry has a 24h cache. The refresh button on the
	// install page sends ?refresh=true to bypass the cache so newly added
	// frameworks appear without waiting for cache expiry.
	forceRefresh := r.URL.Query().Get("refresh") == "true"
	frameworks, err := client.ListFrameworks(ctx, forceRefresh)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Check installation status for each framework
	result := make([]frameworkWithStatus, len(frameworks))
	for i, fw := range frameworks {
		result[i] = frameworkWithStatus{
			Framework: fw,
			Installed: isFrameworkInstalled(fw),
		}
	}

	writeJSON(w, http.StatusOK, result)
}

// isFrameworkInstalled checks if a framework is already installed
func isFrameworkInstalled(fw registry.Framework) bool {
	// Check if config file exists
	configPath := config.ExpandHome(fw.ConfigPath)
	if _, err := os.Stat(configPath); err == nil {
		return true
	}

	// Check if binary exists
	binaryPath := config.ExpandHome(fw.BinaryPath)
	if _, err := os.Stat(binaryPath); err == nil {
		return true
	}

	return false
}

// handleInstallFramework installs a framework with SSE progress streaming
func (s *Server) handleInstallFramework(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FrameworkID string `json:"framework_id"`
		CopyFrom    string `json:"copy_from,omitempty"`
		SkipConfirm bool   `json:"skip_confirm"`
		Force       bool   `json:"force"` // Force restart even if running
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.FrameworkID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "framework_id is required"})
		return
	}

	// Check if already installing
	if existing := globalInstallState.get(req.FrameworkID); existing != nil && existing.Status == "running" {
		if !req.Force {
			// Already installing - just stream the existing progress
			s.streamInstallProgress(w, r, req.FrameworkID)
			return
		}
		// Force restart - kill the old process if it exists
		if existing.PID > 0 {
			if process, err := os.FindProcess(existing.PID); err == nil {
				process.Kill()
			}
		}
	}

	// Clear any previous state
	globalInstallState.set(req.FrameworkID, nil)

	// Fetch framework metadata
	ctx := r.Context()
	client, err := registry.NewClient("")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	fw, err := client.GetFramework(ctx, req.FrameworkID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	// Start installation in background
	go s.runInstallation(fw, req.CopyFrom)

	// Stream progress to this client
	s.streamInstallProgress(w, r, req.FrameworkID)
}

// runInstallation runs the installation in the background
func (s *Server) runInstallation(fw *registry.Framework, copyFrom string) {
	ctx := context.Background()

	// Send initial progress
	progress := &installProgress{
		FrameworkID: fw.ID,
		Phase:       "starting",
		Status:      "running",
		Progress:    0,
		Message:     fmt.Sprintf("Installing %s...", fw.Name),
		StartedAt:   time.Now(),
		logBuf:      make([]string, 0),
	}
	globalInstallState.set(fw.ID, progress)
	progress.addLog(fmt.Sprintf("Starting installation of %s", fw.Name))

	// Run installation phases
	phases := []struct {
		name       string
		progressPct int
		fn         func() error
	}{
		{"binary", 25, func() error { return installBinary(ctx, fw, progress) }},
		{"config", 50, func() error { return scaffoldConfig(ctx, fw, copyFrom, progress) }},
		{"discovery", 75, func() error { return wireDiscovery(fw, progress) }},
		{"adapter", 90, func() error { return setupAdapter(fw, progress) }},
	}

	for _, phase := range phases {
		progress.Phase = phase.name
		progress.Progress = phase.progressPct
		progress.Message = fmt.Sprintf("Phase %s...", phase.name)
		progress.addLog(fmt.Sprintf("Starting phase: %s", phase.name))
		globalInstallState.set(fw.ID, progress)

		if err := phase.fn(); err != nil {
			progress.Status = "error"
			progress.Error = err.Error()
			progress.Message = fmt.Sprintf("Failed at phase %s: %s", phase.name, err.Error())
			progress.addLog(fmt.Sprintf("ERROR: %s", err.Error()))
			now := time.Now()
			progress.CompletedAt = &now
			globalInstallState.set(fw.ID, progress)
			return
		}
	}

	// Success
	progress.Phase = "complete"
	progress.Status = "success"
	progress.Progress = 100
	progress.Message = fmt.Sprintf("%s installed successfully!", fw.Name)
	now := time.Now()
	progress.CompletedAt = &now
	globalInstallState.set(fw.ID, progress)
}

// streamInstallProgress streams the progress of an installation via SSE
func (s *Server) streamInstallProgress(w http.ResponseWriter, r *http.Request, frameworkID string) {
	sse, err := NewSSEWriter(w)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Poll the install state and stream updates
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastProgress *installProgress
	lastLogCount := 0

	for {
		select {
		case <-r.Context().Done():
			// Client disconnected - that's fine, installation continues in background
			return

		case <-ticker.C:
			progress := globalInstallState.get(frameworkID)
			if progress == nil {
				// No installation found
				return
			}

			// Send progress update if changed
			if lastProgress == nil || progress.Phase != lastProgress.Phase || progress.Progress != lastProgress.Progress || progress.Status != lastProgress.Status {
				sse.WriteEvent(progress)
				lastProgress = progress
			}

			// Send new logs
			logs := progress.getLogs()
			for i := lastLogCount; i < len(logs); i++ {
				sse.WriteEvent(map[string]string{"type": "log", "message": logs[i]})
			}
			lastLogCount = len(logs)

			// If completed or errored, stop streaming
			if progress.Status == "success" || progress.Status == "error" {
				return
			}
		}
	}
}

// handleInstallStatus returns the status of all installations
func (s *Server) handleInstallStatus(w http.ResponseWriter, r *http.Request) {
	statuses := globalInstallState.getAll()
	writeJSON(w, http.StatusOK, statuses)
}

// installBinary installs the framework binary
func installBinary(ctx context.Context, fw *registry.Framework, progress *installProgress) error {
	var cmd *exec.Cmd

	switch fw.InstallMethod {
	case "script":
		progress.addLog(fmt.Sprintf("Running install script: %s", fw.InstallCmd))
		cmd = exec.CommandContext(ctx, "bash", "-c", fw.InstallCmd)

	case "cargo":
		progress.addLog(fmt.Sprintf("Running: cargo install %s", fw.ID))
		cmd = exec.CommandContext(ctx, "cargo", "install", fw.ID)

	case "npm":
		progress.addLog(fmt.Sprintf("Running: npm install -g %s", fw.ID))
		cmd = exec.CommandContext(ctx, "npm", "install", "-g", fw.ID)

	case "pip":
		progress.addLog(fmt.Sprintf("Running: pip install %s", fw.ID))
		cmd = exec.CommandContext(ctx, "pip", "install", fw.ID)

	case "manual":
		progress.addLog("Manual installation - skipping")
		return nil

	default:
		return fmt.Errorf("unsupported install method: %s", fw.InstallMethod)
	}

	// Capture output and stream to logs
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		return err
	}

	// Store PID so we can detect if process dies
	progress.PID = cmd.Process.Pid
	globalInstallState.save()

	// Stream output to logs
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) != "" {
				progress.addLog(line)
			}
		}
	}()

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) != "" {
				progress.addLog(line)
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		return err
	}

	progress.addLog("Binary installation complete")
	return nil
}

// scaffoldConfig sets up configuration
func scaffoldConfig(ctx context.Context, fw *registry.Framework, copyFrom string, progress *installProgress) error {
	expandedPath := config.ExpandHome(fw.ConfigPath)

	// Check if config already exists
	if _, err := os.Stat(expandedPath); err == nil {
		progress.addLog(fmt.Sprintf("Config already exists at %s", fw.ConfigPath))
		return nil
	}

	if copyFrom != "" {
		progress.addLog(fmt.Sprintf("Copying config from %s (not yet implemented)", copyFrom))
		// TODO: Implement config copying (needs discovery context)
		return fmt.Errorf("config copying not yet implemented")
	}

	progress.addLog("Using default config from framework installer")
	return nil
}

// wireDiscovery adds framework to discovery config
func wireDiscovery(fw *registry.Framework, progress *installProgress) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	expandedPath := config.ExpandHome(fw.ConfigPath)

	// Check if already in discovery paths
	for _, path := range cfg.Discovery.ConfigPaths {
		if config.ExpandHome(path) == expandedPath {
			progress.addLog("Already in discovery paths")
			return nil
		}
	}

	// Add to config
	cfg.Discovery.ConfigPaths = append(cfg.Discovery.ConfigPaths, fw.ConfigPath)

	if err := config.Save(cfg); err != nil {
		return err
	}

	progress.addLog(fmt.Sprintf("Added %s to discovery paths", fw.ConfigPath))
	return nil
}

// setupAdapter verifies adapter support
func setupAdapter(fw *registry.Framework, progress *installProgress) error {
	progress.addLog(fmt.Sprintf("Setting up %s adapter", fw.AdapterType))

	switch fw.AdapterType {
	case "http", "websocket", "cli", "hybrid":
		progress.addLog(fmt.Sprintf("Using %s adapter", fw.AdapterType))
		return nil
	default:
		return fmt.Errorf("unsupported adapter type: %s", fw.AdapterType)
	}
}
