package cli

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/natalie/eyrie/internal/config"
	"github.com/natalie/eyrie/internal/discovery"
	"github.com/spf13/cobra"
)

var discoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Discover Claw agents on this machine",
	Long:  "Scan known config file locations and probe gateway endpoints to find running Claw agents.",
	RunE:  runDiscover,
}

func init() {
	rootCmd.AddCommand(discoverCmd)
}

func runDiscover(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	fmt.Println("Scanning config files...")
	for _, p := range cfg.Discovery.ConfigPaths {
		fmt.Printf("  %s\n", config.ExpandHome(p))
	}
	if len(cfg.Agents) > 0 {
		fmt.Printf("  + %d manually configured agent(s)\n", len(cfg.Agents))
	}
	fmt.Println()

	result := discovery.Run(ctx, cfg)

	if len(result.Agents) == 0 {
		fmt.Println("No agents discovered.")
		fmt.Println("Checked default config paths. Add custom paths to ~/.eyrie/config.toml")
		return nil
	}

	fmt.Printf("Found %d agent(s):\n\n", len(result.Agents))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tFRAMEWORK\tADDRESS\tCONFIG\tRESPONDING")
	fmt.Fprintln(w, "────\t─────────\t───────\t──────\t──────────")

	for _, ar := range result.Agents {
		responding := "no"
		if ar.Alive {
			responding = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s:%d\t%s\t%s\n",
			ar.Agent.Name,
			ar.Agent.Framework,
			ar.Agent.Host,
			ar.Agent.Port,
			ar.Agent.ConfigPath,
			responding,
		)
	}

	return w.Flush()
}
