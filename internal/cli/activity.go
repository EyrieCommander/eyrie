package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/natalie/eyrie/internal/config"
	"github.com/natalie/eyrie/internal/discovery"
	"github.com/spf13/cobra"
)

var activityCmd = &cobra.Command{
	Use:   "activity <agent-name>",
	Short: "Stream activity events from an agent in real time",
	Long:  "Show real-time activity events (agent sessions, tool calls, LLM requests) from a running agent.",
	Args:  cobra.ExactArgs(1),
	RunE:  runActivity,
}

func init() {
	rootCmd.AddCommand(activityCmd)
}

func runActivity(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		cancel()
	}()

	discoveryCtx, discoveryCancel := context.WithTimeout(ctx, 15*time.Second)
	result := discovery.Run(discoveryCtx, cfg)
	discoveryCancel()

	name := args[0]
	for _, ar := range result.Agents {
		if ar.Agent.Name == name {
			if !ar.Alive {
				return fmt.Errorf("agent %q is not running", name)
			}

			agent := discovery.NewAgent(ar.Agent)
			fmt.Printf("Streaming activity for %s (%s)... Press Ctrl+C to stop.\n\n", name, ar.Agent.Framework)

			ch, err := agent.TailActivity(ctx)
			if err != nil {
				return fmt.Errorf("connecting to activity stream: %w", err)
			}

			for ev := range ch {
				ts := ev.Timestamp.Format("15:04:05")
				fmt.Printf("%s [%-14s] %s\n", ts, ev.Type, ev.Summary)
			}

			return nil
		}
	}

	return fmt.Errorf("agent %q not found. Run 'eyrie discover' to see available agents", name)
}
