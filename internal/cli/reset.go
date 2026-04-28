package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Audacity88/eyrie/internal/config"
	"github.com/Audacity88/eyrie/internal/registry"
	"github.com/spf13/cobra"
)

var resetCmd = &cobra.Command{
	Use:   "reset <framework-id>",
	Short: "Remove a framework's config (keeps the binary)",
	Long: `Remove a framework's configuration file so you can redo onboarding
without rebuilding from source. The binary remains installed.

Examples:
  eyrie reset picoclaw           Reset PicoClaw config (prompts for confirmation)
  eyrie reset picoclaw -y        Skip confirmation prompt`,
	Args: cobra.ExactArgs(1),
	RunE: runReset,
}

var resetFlags struct {
	yes      bool
	registry string
}

func init() {
	rootCmd.AddCommand(resetCmd)
	resetCmd.Flags().BoolVarP(&resetFlags.yes, "yes", "y", false, "Skip confirmation prompt")
	resetCmd.Flags().StringVar(&resetFlags.registry, "registry", "", "Custom registry URL")
}

func runReset(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	frameworkID := args[0]

	client, err := registry.NewClient(resetFlags.registry)
	if err != nil {
		return fmt.Errorf("failed to create registry client: %w", err)
	}

	fw, err := client.GetFramework(ctx, frameworkID)
	if err != nil {
		return fmt.Errorf("framework %q not found in registry: %w", frameworkID, err)
	}

	configPath := config.ExpandHome(fw.ConfigPath)
	if !fileExists(configPath) {
		fmt.Printf("%s config not found at %s — nothing to reset.\n", fw.Name, fw.ConfigPath)
		return nil
	}

	fmt.Printf("Resetting %s\n", fw.Name)
	fmt.Printf("  Config: %s (will be removed)\n", fw.ConfigPath)
	fmt.Printf("  Binary: %s (kept)\n", fw.BinaryPath)

	if !resetFlags.yes {
		fmt.Print("\nProceed? [y/N] ")
		var response string
		fmt.Scanln(&response)
		if !strings.HasPrefix(strings.ToLower(response), "y") {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	if err := os.Remove(configPath); err != nil {
		return fmt.Errorf("failed to remove config: %w", err)
	}
	fmt.Printf("✓ Removed %s\n", fw.ConfigPath)

	fmt.Printf("\n✓ %s config reset. Run onboarding again to reconfigure.\n", fw.Name)
	return nil
}
