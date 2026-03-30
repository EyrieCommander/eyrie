package embedded

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Tool is a single executable tool that can be registered with the agent loop.
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
	Execute     func(ctx context.Context, args map[string]any) (string, error)
}

// ToolRegistry manages the set of tools available to an embedded agent.
// Tools are opt-in — only those listed in the agent config are registered.
type ToolRegistry struct {
	tools map[string]*Tool
}

// NewToolRegistry creates an empty registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]*Tool)}
}

// Register adds a tool to the registry, replacing any existing tool with
// the same name.
func (r *ToolRegistry) Register(t *Tool) {
	r.tools[t.Name] = t
}

// Get returns a tool by name, or nil if not found.
func (r *ToolRegistry) Get(name string) *Tool {
	return r.tools[name]
}

// List returns all registered tool names.
func (r *ToolRegistry) List() []string {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// Definitions returns the registered tools as ToolDef values suitable for
// passing to the LLM provider.
func (r *ToolRegistry) Definitions() []ToolDef {
	defs := make([]ToolDef, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, ToolDef{
			Type: "function",
			Function: ToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}
	return defs
}

// RegisterBuiltins registers the subset of built-in tools specified by names.
// Unknown tool names are silently ignored. workspace is the root directory
// for file-system tools (read_file, write_file, list_dir, exec).
func (r *ToolRegistry) RegisterBuiltins(names []string, workspace string) {
	builtins := map[string]func(string) *Tool{
		"read_file":  builtinReadFile,
		"write_file": builtinWriteFile,
		"list_dir":   builtinListDir,
		"exec":       builtinExec,
		"web_fetch":  func(_ string) *Tool { return builtinWebFetch() },
	}
	for _, name := range names {
		factory, ok := builtins[name]
		if !ok {
			continue
		}
		r.Register(factory(workspace))
	}
}

// --- Built-in tool implementations ---

func builtinReadFile(workspace string) *Tool {
	return &Tool{
		Name:        "read_file",
		Description: "Read the contents of a file within the workspace.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Relative path within the workspace.",
				},
			},
			"required": []string{"path"},
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			relPath, _ := args["path"].(string)
			if relPath == "" {
				return "", fmt.Errorf("path is required")
			}
			absPath, err := safePath(workspace, relPath)
			if err != nil {
				return "", err
			}
			data, err := os.ReadFile(absPath)
			if err != nil {
				return "", fmt.Errorf("reading file: %w", err)
			}
			// 100 KB limit to avoid blowing up context
			const maxSize = 100 * 1024
			if len(data) > maxSize {
				return string(data[:maxSize]) + "\n... (truncated)", nil
			}
			return string(data), nil
		},
	}
}

func builtinWriteFile(workspace string) *Tool {
	return &Tool{
		Name:        "write_file",
		Description: "Write or create a file within the workspace.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Relative path within the workspace.",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "File contents to write.",
				},
			},
			"required": []string{"path", "content"},
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			relPath, _ := args["path"].(string)
			content, _ := args["content"].(string)
			if relPath == "" {
				return "", fmt.Errorf("path is required")
			}
			absPath, err := safePath(workspace, relPath)
			if err != nil {
				return "", err
			}
			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
				return "", fmt.Errorf("creating directories: %w", err)
			}
			if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
				return "", fmt.Errorf("writing file: %w", err)
			}
			return fmt.Sprintf("wrote %d bytes to %s", len(content), relPath), nil
		},
	}
}

func builtinListDir(workspace string) *Tool {
	return &Tool{
		Name:        "list_dir",
		Description: "List files and directories within a workspace directory.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Relative directory path (empty or '.' for workspace root).",
				},
			},
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			relPath, _ := args["path"].(string)
			if relPath == "" {
				relPath = "."
			}
			absPath, err := safePath(workspace, relPath)
			if err != nil {
				return "", err
			}
			entries, err := os.ReadDir(absPath)
			if err != nil {
				return "", fmt.Errorf("listing directory: %w", err)
			}
			var sb strings.Builder
			for _, e := range entries {
				if e.IsDir() {
					sb.WriteString(e.Name() + "/\n")
				} else {
					info, _ := e.Info()
					if info != nil {
						sb.WriteString(fmt.Sprintf("%s  (%d bytes)\n", e.Name(), info.Size()))
					} else {
						sb.WriteString(e.Name() + "\n")
					}
				}
			}
			if sb.Len() == 0 {
				return "(empty directory)", nil
			}
			return sb.String(), nil
		},
	}
}

func builtinExec(workspace string) *Tool {
	return &Tool{
		Name:        "exec",
		Description: "Execute a shell command within the workspace directory.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "Shell command to execute.",
				},
			},
			"required": []string{"command"},
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			command, _ := args["command"].(string)
			if command == "" {
				return "", fmt.Errorf("command is required")
			}

			// 60-second per-tool timeout
			execCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()

			cmd := exec.CommandContext(execCtx, "sh", "-c", command)
			cmd.Dir = workspace
			output, err := cmd.CombinedOutput()

			// Truncate large output to avoid blowing up context
			const maxOutput = 50 * 1024 // 50 KB
			result := string(output)
			if len(result) > maxOutput {
				result = result[:maxOutput] + "\n... (truncated)"
			}

			if err != nil {
				return fmt.Sprintf("%s\nexit status: %v", result, err), nil
			}
			return result, nil
		},
	}
}

func builtinWebFetch() *Tool {
	return &Tool{
		Name:        "web_fetch",
		Description: "Fetch the contents of a URL via HTTP GET.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "URL to fetch.",
				},
			},
			"required": []string{"url"},
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			rawURL, _ := args["url"].(string)
			if rawURL == "" {
				return "", fmt.Errorf("url is required")
			}

			// 30-second timeout for web requests
			fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(fetchCtx, "GET", rawURL, nil)
			if err != nil {
				return "", fmt.Errorf("creating request: %w", err)
			}
			req.Header.Set("User-Agent", "EyrieClaw/1.0")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return "", fmt.Errorf("fetching URL: %w", err)
			}
			defer resp.Body.Close()

			// 1 MB size limit
			const maxSize = 1024 * 1024
			limited := io.LimitReader(resp.Body, maxSize+1)
			data, err := io.ReadAll(limited)
			if err != nil {
				return "", fmt.Errorf("reading response: %w", err)
			}

			result := string(data)
			if len(data) > maxSize {
				result = result[:maxSize] + "\n... (truncated)"
			}
			return fmt.Sprintf("HTTP %d\n\n%s", resp.StatusCode, result), nil
		},
	}
}

// safePath resolves a relative path against the workspace root and ensures
// the result stays within the workspace. Prevents path traversal attacks.
func safePath(workspace, relPath string) (string, error) {
	// Clean the relative path to remove .. components
	cleaned := filepath.Clean(relPath)
	if filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("absolute paths not allowed: %s", relPath)
	}

	absPath := filepath.Join(workspace, cleaned)
	// Verify the resolved path is still under workspace
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("resolving workspace: %w", err)
	}
	absResolved, err := filepath.Abs(absPath)
	if err != nil {
		return "", fmt.Errorf("resolving path: %w", err)
	}
	if !strings.HasPrefix(absResolved, absWorkspace+string(filepath.Separator)) && absResolved != absWorkspace {
		return "", fmt.Errorf("path escapes workspace: %s", relPath)
	}
	return absResolved, nil
}

// parseToolArgs unmarshals the raw JSON arguments string from a tool call
// into a map. Returns an empty map on parse failure.
func parseToolArgs(raw string) map[string]any {
	args := make(map[string]any)
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &args)
	}
	return args
}
