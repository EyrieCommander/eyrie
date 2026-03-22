package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/Audacity88/eyrie/internal/config"
	"github.com/Audacity88/eyrie/internal/discovery"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config <agent-name>",
	Short: "View an agent's configuration",
	Args:  cobra.ExactArgs(1),
	RunE:  runConfig,
}

func init() {
	rootCmd.AddCommand(configCmd)
}

func runConfig(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result := discovery.Run(ctx, cfg)

	name := args[0]
	for _, ar := range result.Agents {
		if ar.Agent.Name == name {
			if !ar.Alive {
				return fmt.Errorf("agent %q is not running; cannot fetch config", name)
			}
			agent := discovery.NewAgent(ar.Agent)
			agentCfg, err := agent.Config(ctx)
			if err != nil {
				return fmt.Errorf("fetching config for %q: %w", name, err)
			}
			fmt.Printf("# %s configuration (%s)\n\n", name, agentCfg.Format)
			fmt.Println(agentCfg.Raw)
			return nil
		}
	}

	return fmt.Errorf("agent %q not found", name)
}
