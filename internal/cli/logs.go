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

var logsCmd = &cobra.Command{
	Use:   "logs <agent-name>",
	Short: "Tail logs from an agent in real time",
	Args:  cobra.ExactArgs(1),
	RunE:  runLogs,
}

func init() {
	rootCmd.AddCommand(logsCmd)
}

func runLogs(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle Ctrl+C gracefully
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
			fmt.Printf("Tailing logs for %s (%s)... Press Ctrl+C to stop.\n\n", name, ar.Agent.Framework)

			logCh, err := agent.TailLogs(ctx)
			if err != nil {
				return fmt.Errorf("connecting to log stream: %w", err)
			}

			for entry := range logCh {
				ts := entry.Timestamp.Format("15:04:05")
				level := entry.Level
				if level == "" {
					level = "info"
				}
				fmt.Printf("%s [%s] %s\n", ts, level, entry.Message)
			}

			return nil
		}
	}

	return fmt.Errorf("agent %q not found. Run 'eyrie discover' to see available agents", name)
}
