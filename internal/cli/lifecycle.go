package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/natalie/eyrie/internal/config"
	"github.com/natalie/eyrie/internal/discovery"
	"github.com/natalie/eyrie/internal/manager"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start <agent-name>",
	Short: "Start an agent",
	Args:  cobra.ExactArgs(1),
	RunE:  makeLifecycleRunner(manager.ActionStart),
}

var stopCmd = &cobra.Command{
	Use:   "stop <agent-name>",
	Short: "Stop an agent",
	Args:  cobra.ExactArgs(1),
	RunE:  makeLifecycleRunner(manager.ActionStop),
}

var restartCmd = &cobra.Command{
	Use:   "restart <agent-name>",
	Short: "Restart an agent",
	Args:  cobra.ExactArgs(1),
	RunE:  makeLifecycleRunner(manager.ActionRestart),
}

func init() {
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(restartCmd)
}

func makeLifecycleRunner(action manager.LifecycleAction) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result := discovery.Run(ctx, cfg)

		name := args[0]
		for _, ar := range result.Agents {
			if ar.Agent.Name == name {
				fmt.Printf("%sing %s (%s)...\n", capitalize(string(action)), name, ar.Agent.Framework)

				if err := manager.Execute(ctx, ar.Agent.Framework, action); err != nil {
					return fmt.Errorf("%s failed: %w", action, err)
				}

				fmt.Printf("Agent %q %sed successfully.\n", name, action)
				return nil
			}
		}

		return fmt.Errorf("agent %q not found. Run 'eyrie discover' to see available agents", name)
	}
}

func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
