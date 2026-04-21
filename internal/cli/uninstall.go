package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Audacity88/eyrie/internal/config"
	"github.com/Audacity88/eyrie/internal/registry"
	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall <framework-id>",
	Short: "Uninstall a Claw framework",
	Long: `Uninstall a Claw agent framework, removing its binary and optionally its config.

Examples:
  eyrie uninstall hermes             Uninstall Hermes (keeps config)
  eyrie uninstall hermes --purge     Also remove config directory
  eyrie uninstall hermes -y          Skip confirmation prompt`,
	Args: cobra.ExactArgs(1),
	RunE: runUninstall,
}

var uninstallFlags struct {
	purge    bool
	yes      bool
	registry string
}

func init() {
	rootCmd.AddCommand(uninstallCmd)
	uninstallCmd.Flags().BoolVar(&uninstallFlags.purge, "purge", false, "Also remove config directory")
	uninstallCmd.Flags().BoolVarP(&uninstallFlags.yes, "yes", "y", false, "Skip confirmation prompts")
	uninstallCmd.Flags().StringVar(&uninstallFlags.registry, "registry", "", "Custom registry URL")
}

func runUninstall(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	frameworkID := args[0]

	// Create registry client to look up framework metadata
	client, err := registry.NewClient(uninstallFlags.registry)
	if err != nil {
		return fmt.Errorf("failed to create registry client: %w", err)
	}

	fw, err := client.GetFramework(ctx, frameworkID)
	if err != nil {
		return fmt.Errorf("framework %q not found in registry: %w", frameworkID, err)
	}

	binaryPath := config.ExpandHome(fw.BinaryPath)
	configDir := config.ExpandHome(fw.ConfigDir)

	// Check if anything is actually installed
	binaryExists := fileExists(binaryPath)
	configFileExists := fileExists(config.ExpandHome(fw.ConfigPath))
	configDirExists := fileExists(configDir)
	configExists := configFileExists || configDirExists

	if !binaryExists && !configExists {
		fmt.Printf("%s does not appear to be installed.\n", fw.Name)
		return nil
	}

	// Show what will be removed
	fmt.Printf("Uninstalling %s\n", fw.Name)
	if binaryExists {
		fmt.Printf("  Binary: %s\n", fw.BinaryPath)
	}
	if uninstallFlags.purge && configExists {
		fmt.Printf("  Config: %s\n", fw.ConfigDir)
	}

	// Confirm
	if !uninstallFlags.yes {
		fmt.Print("\nProceed? [y/N] ")
		var response string
		fmt.Scanln(&response)
		if !strings.HasPrefix(strings.ToLower(response), "y") {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	// Phase 1: Remove binary
	if binaryExists {
		fmt.Println("\n━━━ Removing binary ━━━")
		if err := uninstallBinary(ctx, fw); err != nil {
			return fmt.Errorf("failed to remove binary: %w", err)
		}
		fmt.Printf("✓ Removed %s\n", fw.BinaryPath)
	}

	// Phase 2: Remove from discovery
	fmt.Println("\n━━━ Removing from discovery ━━━")
	if err := unwireDiscovery(fw); err != nil {
		fmt.Printf("⚠️  Could not remove from discovery: %s\n", err)
	} else {
		fmt.Println("✓ Removed from discovery paths")
	}

	// Phase 3: Purge config (optional)
	if uninstallFlags.purge {
		fmt.Println("\n━━━ Removing config ━━━")
		configPath := config.ExpandHome(fw.ConfigPath)
		removed := false

		// Remove config file
		if fileExists(configPath) {
			if err := os.Remove(configPath); err != nil {
				fmt.Printf("⚠️  Could not remove config file: %s\n", err)
			} else {
				fmt.Printf("✓ Removed %s\n", fw.ConfigPath)
				removed = true
			}
		}

		// Remove config directory — but only after a sanity check so a
		// bad ConfigDir in a registry entry (or a symlinked "/") can't
		// take out critical system paths.
		if fileExists(configDir) {
			if !isSafeToRemove(configDir) {
				fmt.Printf("⚠️  Refusing to remove %s: path failed safety check\n", configDir)
			} else if err := os.RemoveAll(configDir); err != nil {
				fmt.Printf("⚠️  Could not remove config directory: %s\n", err)
			} else {
				fmt.Printf("✓ Removed %s\n", fw.ConfigDir)
				removed = true
			}
		}

		if !removed {
			fmt.Println("Config not found, skipping")
		}
	}

	// Phase 4: Clear install status from state file
	clearInstallStatus(fw.ID)

	fmt.Printf("\n✓ %s uninstalled successfully.\n", fw.Name)
	if !uninstallFlags.purge && configExists {
		fmt.Printf("\nNote: Config directory kept at %s (use --purge to remove)\n", fw.ConfigDir)
	}

	return nil
}

// uninstallBinary removes the framework binary using the appropriate method
func uninstallBinary(ctx context.Context, fw *registry.Framework) error {
	binaryPath := config.ExpandHome(fw.BinaryPath)

	switch fw.InstallMethod {
	case "cargo":
		// cargo uninstall is cleaner — removes from cargo's tracking
		fmt.Printf("Running: cargo uninstall %s\n", fw.ID)
		if err := runCommand(ctx, "cargo", "uninstall", fw.ID); err != nil {
			// Fall back to direct removal if cargo uninstall fails
			fmt.Printf("cargo uninstall failed, removing binary directly\n")
			return os.Remove(binaryPath)
		}
		return nil

	case "npm":
		fmt.Printf("Running: npm uninstall -g %s\n", fw.ID)
		if err := runCommand(ctx, "npm", "uninstall", "-g", fw.ID); err != nil {
			fmt.Printf("npm uninstall failed, removing binary directly\n")
			return os.Remove(binaryPath)
		}
		return nil

	case "pip":
		fmt.Printf("Running: pip uninstall -y %s\n", fw.ID)
		if err := runCommand(ctx, "pip", "uninstall", "-y", fw.ID); err != nil {
			fmt.Printf("pip uninstall failed, removing binary directly\n")
			return os.Remove(binaryPath)
		}
		return nil

	default:
		// script, manual, or unknown — just remove the binary
		return os.Remove(binaryPath)
	}
}

// unwireDiscovery removes the framework's config path from eyrie's discovery config
func unwireDiscovery(fw *registry.Framework) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	expandedPath := config.ExpandHome(fw.ConfigPath)
	filtered := make([]string, 0, len(cfg.Discovery.ConfigPaths))
	found := false

	for _, path := range cfg.Discovery.ConfigPaths {
		if config.ExpandHome(path) == expandedPath {
			found = true
			continue
		}
		filtered = append(filtered, path)
	}

	if !found {
		return nil // not in discovery
	}

	cfg.Discovery.ConfigPaths = filtered
	return config.Save(cfg)
}

// clearInstallStatus removes a framework's entry from the install status file.
// Best-effort: the file is derived state (gets rebuilt from filesystem checks),
// so we log and skip rather than fail the uninstall if we can't touch it.
func clearInstallStatus(frameworkID string) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		fmt.Fprintf(os.Stderr, "clearInstallStatus: cannot resolve home dir (%v); skipping\n", err)
		return
	}
	statusFile := filepath.Join(home, ".eyrie", "install_status.json")

	data, err := os.ReadFile(statusFile)
	if err != nil {
		return
	}

	var statuses map[string]interface{}
	if err := json.Unmarshal(data, &statuses); err != nil {
		return
	}

	delete(statuses, frameworkID)

	out, _ := json.MarshalIndent(statuses, "", "  ")
	os.WriteFile(statusFile, out, 0644)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// isSafeToRemove returns false for paths we should never recursively delete:
// empty strings, relative paths, root ("/"), the user's home directory, and
// well-known system directories. This is a defence-in-depth check — the
// registry is trusted, but a bad entry shouldn't be able to wipe the system.
func isSafeToRemove(path string) bool {
	if path == "" {
		return false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	// Resolve symlinks so a symlinked configDir can't point to a dangerous target.
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	} else {
		return false
	}
	cleaned := filepath.Clean(abs)
	if cleaned == "/" || cleaned == "." || cleaned == ".." {
		return false
	}
	// Block well-known danger paths (not exhaustive — just guardrails).
	dangerous := []string{"/", "/tmp", "/var", "/etc", "/usr", "/bin", "/sbin", "/opt", "/Users", "/home", "/root"}
	for _, d := range dangerous {
		if cleaned == d {
			return false
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if cleaned == filepath.Clean(home) {
			return false
		}
	}
	return true
}
