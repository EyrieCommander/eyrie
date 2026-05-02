package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Audacity88/eyrie/internal/config"
	"github.com/Audacity88/eyrie/internal/discovery"
	"github.com/Audacity88/eyrie/internal/registry"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install [framework-id]",
	Short: "Install a new Claw framework",
	Long: `Install a new Claw agent framework from the registry.

Examples:
  eyrie install               List available frameworks
  eyrie install hermes        Install Hermes agent
  eyrie install hermes --from zeroclaw   Install Hermes and copy config from ZeroClaw
  eyrie install hermes -y     Install without confirmation prompts`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInstall,
}

var installFlags struct {
	copyFrom string
	yes      bool
	registry string
}

func init() {
	rootCmd.AddCommand(installCmd)
	installCmd.Flags().StringVar(&installFlags.copyFrom, "from", "", "Copy config from existing agent")
	installCmd.Flags().BoolVarP(&installFlags.yes, "yes", "y", false, "Skip confirmation prompts")
	installCmd.Flags().StringVar(&installFlags.registry, "registry", "", "Custom registry URL")
}

func runInstall(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// Create registry client
	client, err := registry.NewClient(installFlags.registry)
	if err != nil {
		return fmt.Errorf("failed to create registry client: %w", err)
	}

	// No args: list available frameworks
	if len(args) == 0 {
		return listFrameworks(ctx, client)
	}

	frameworkID := args[0]

	// Fetch framework metadata
	fw, err := client.GetFramework(ctx, frameworkID)
	if err != nil {
		return err
	}

	// Display framework info
	fmt.Printf("Installing %s (%s)\n", fw.Name, fw.Description)
	fmt.Printf("  Language:   %s\n", fw.Language)
	fmt.Printf("  Repository: %s\n", fw.Repository)
	fmt.Printf("  Config:     %s\n", fw.ConfigPath)
	fmt.Printf("  Binary:     %s\n", fw.BinaryPath)

	// Check requirements
	if err := checkRequirements(fw); err != nil {
		return fmt.Errorf("requirements not met: %w", err)
	}

	// Confirm installation
	if !installFlags.yes {
		fmt.Print("\nProceed with installation? [y/N] ")
		var response string
		fmt.Scanln(&response)
		if !strings.HasPrefix(strings.ToLower(response), "y") {
			fmt.Println("Installation cancelled")
			return nil
		}
	}

	// Phase 1: Install binary
	fmt.Println("\n━━━ Phase 1/4: Installing Binary ━━━")
	if err := installBinary(ctx, fw); err != nil {
		return fmt.Errorf("binary installation failed: %w", err)
	}
	fmt.Println("✓ Binary installed successfully")

	// Phase 2: Scaffold config
	fmt.Println("\n━━━ Phase 2/4: Setting Up Configuration ━━━")
	if err := scaffoldConfig(ctx, fw); err != nil {
		return fmt.Errorf("config setup failed: %w", err)
	}
	fmt.Println("✓ Configuration ready")

	// Phase 3: Wire discovery
	fmt.Println("\n━━━ Phase 3/4: Wiring Discovery ━━━")
	if err := wireDiscovery(fw); err != nil {
		return fmt.Errorf("discovery setup failed: %w", err)
	}
	fmt.Println("✓ Discovery configured")

	// Phase 4: Setup adapter
	fmt.Println("\n━━━ Phase 4/4: Setting Up Adapter ━━━")
	if err := setupAdapter(fw); err != nil {
		return fmt.Errorf("adapter setup failed: %w", err)
	}
	fmt.Println("✓ Adapter ready")

	// Success message
	fmt.Printf("\n✓ %s installed successfully!\n", fw.Name)
	fmt.Printf("\n📋 Next steps:\n")
	fmt.Printf("  1. Configure %s:  edit %s\n", fw.Name, fw.ConfigPath)
	fmt.Printf("  2. Start the agent:  eyrie start %s\n", fw.ID)
	fmt.Printf("  3. Check status:     eyrie status %s\n", fw.ID)
	fmt.Printf("  4. View logs:        eyrie logs %s\n", fw.ID)

	return nil
}

func listFrameworks(ctx context.Context, client *registry.Client) error {
	frameworks, err := client.ListFrameworks(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to fetch registry: %w", err)
	}

	fmt.Println("Available Claw frameworks:")
	fmt.Println()

	for _, fw := range frameworks {
		fmt.Printf("  %s\n", fw.ID)
		fmt.Printf("    Name:       %s\n", fw.Name)
		fmt.Printf("    Language:   %s\n", fw.Language)
		fmt.Printf("    Descripton: %s\n", fw.Description)
		fmt.Printf("    Repository: %s\n", fw.Repository)
		fmt.Println()
	}

	fmt.Printf("Install a framework:  eyrie install <framework-id>\n")
	fmt.Printf("Example:              eyrie install hermes\n")

	return nil
}

func checkRequirements(fw *registry.Framework) error {
	if len(fw.Requirements) == 0 {
		return nil
	}

	fmt.Println("\n⚠️  Requirements:")
	for _, req := range fw.Requirements {
		fmt.Printf("  • %s\n", req)
	}

	fmt.Println("\nNote: Requirements are not automatically verified. Please ensure they are met.")

	return nil
}

func installBinary(ctx context.Context, fw *registry.Framework) error {
	switch fw.InstallMethod {
	case "script":
		fmt.Printf("Running install script: %s\n", fw.InstallCmd)
		return runInstallScript(ctx, fw.InstallCmd)

	case "cargo":
		if fw.IsCustomInstallCmd() {
			fmt.Printf("Running: %s\n", fw.InstallCmd)
			return runInstallScript(ctx, fw.InstallCmd)
		}
		fmt.Printf("Running: cargo install %s\n", fw.ID)
		return runCommand(ctx, "cargo", "install", fw.ID)

	case "npm":
		if fw.IsCustomInstallCmd() {
			fmt.Printf("Running: %s\n", fw.InstallCmd)
			return runInstallScript(ctx, fw.InstallCmd)
		}
		fmt.Printf("Running: npm install -g %s\n", fw.ID)
		return runCommand(ctx, "npm", "install", "-g", fw.ID)

	case "pip":
		if fw.IsCustomInstallCmd() {
			fmt.Printf("Running: %s\n", fw.InstallCmd)
			return runInstallScript(ctx, fw.InstallCmd)
		}
		fmt.Printf("Running: pip install %s\n", fw.ID)
		return runCommand(ctx, "pip", "install", fw.ID)

	case "manual":
		fmt.Println("⚠️  Manual installation required")
		fmt.Printf("Please install %s manually according to the repository instructions:\n", fw.Name)
		fmt.Printf("  %s\n", fw.Repository)
		return nil

	default:
		return fmt.Errorf("unsupported install method: %s", fw.InstallMethod)
	}
}

func runInstallScript(ctx context.Context, scriptURL string) error {
	cmd := exec.CommandContext(ctx, "bash", "-c", scriptURL)
	cmd.Env = config.EnrichedEnv()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func runCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, config.LookPathEnriched(name), args...)
	cmd.Env = config.EnrichedEnv()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func scaffoldConfig(ctx context.Context, fw *registry.Framework) error {
	expandedPath := config.ExpandHome(fw.ConfigPath)

	// Check if config already exists
	if _, err := os.Stat(expandedPath); err == nil {
		fmt.Printf("Config already exists at %s\n", fw.ConfigPath)

		if installFlags.copyFrom == "" {
			return nil
		}

		// If --from is specified, offer to overwrite
		if !installFlags.yes {
			fmt.Print("Overwrite with config from existing agent? [y/N] ")
			var response string
			fmt.Scanln(&response)
			if !strings.HasPrefix(strings.ToLower(response), "y") {
				fmt.Println("Keeping existing config")
				return nil
			}
		}
	}

	// If --from is specified, copy from existing agent
	if installFlags.copyFrom != "" {
		return copyConfigFrom(fw, installFlags.copyFrom)
	}

	// Otherwise, config should have been created by the installer
	if _, err := os.Stat(expandedPath); os.IsNotExist(err) {
		fmt.Printf("⚠️  Config not found at %s\n", fw.ConfigPath)
		fmt.Printf("The framework installer may not have created a default config.\n")
		fmt.Printf("Please create one manually or run the framework's setup command.\n")
	} else {
		fmt.Printf("Using default config at %s\n", fw.ConfigPath)
	}

	return nil
}

func copyConfigFrom(fw *registry.Framework, sourceName string) error {
	fmt.Printf("Copying config from %s...\n", sourceName)

	// Load eyrie config to discover source agent
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Discover agents
	result := discovery.Run(ctx, cfg)

	// Find source agent
	var sourceAgent *discovery.AgentResult
	for i := range result.Agents {
		if result.Agents[i].Agent.Name == sourceName || result.Agents[i].Agent.Framework == sourceName {
			sourceAgent = &result.Agents[i]
			break
		}
	}

	if sourceAgent == nil {
		return fmt.Errorf("source agent %q not found (is it running?)", sourceName)
	}

	// Get source config path from the discovered agent
	sourceConfigPath := sourceAgent.Agent.ConfigPath
	if sourceConfigPath == "" {
		return fmt.Errorf("source agent config path not available")
	}

	// Read source config
	sourceData, err := os.ReadFile(config.ExpandHome(sourceConfigPath))
	if err != nil {
		return fmt.Errorf("failed to read source config: %w", err)
	}

	// For now, just copy as-is
	// TODO: Implement format conversion (TOML -> YAML, JSON -> YAML, etc.)
	destPath := config.ExpandHome(fw.ConfigPath)

	// Ensure directory exists
	if err := os.MkdirAll(config.ExpandHome(fw.ConfigDir), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Write to destination
	if err := os.WriteFile(destPath, sourceData, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Printf("✓ Config copied from %s\n", sourceName)
	fmt.Printf("⚠️  Note: You may need to adjust the config for %s\n", fw.Name)

	return nil
}

func wireDiscovery(fw *registry.Framework) error {
	// Load current config
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	expandedPath := config.ExpandHome(fw.ConfigPath)

	// Check if already in discovery paths
	for _, path := range cfg.Discovery.ConfigPaths {
		if config.ExpandHome(path) == expandedPath {
			fmt.Printf("Already in discovery paths: %s\n", fw.ConfigPath)
			return nil
		}
	}

	// Add to config
	cfg.Discovery.ConfigPaths = append(cfg.Discovery.ConfigPaths, fw.ConfigPath)

	// Save config
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Added %s to discovery paths\n", fw.ConfigPath)

	return nil
}

func setupAdapter(fw *registry.Framework) error {
	switch fw.AdapterType {
	case "http":
		fmt.Printf("Using HTTP REST adapter (like ZeroClaw)\n")
		return nil

	case "websocket":
		fmt.Printf("Using WebSocket RPC adapter (like OpenClaw)\n")
		return nil

	case "cli":
		fmt.Printf("Using CLI-based adapter\n")
		fmt.Printf("⚠️  Note: CLI adapters require custom implementation\n")
		fmt.Printf("The adapter will invoke commands and parse file-based status.\n")
		return nil

	case "hybrid":
		fmt.Printf("Using hybrid adapter (HTTP + CLI)\n")
		return nil

	default:
		return fmt.Errorf("unsupported adapter type: %s", fw.AdapterType)
	}
}
