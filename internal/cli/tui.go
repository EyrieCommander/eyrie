package cli

import (
	"fmt"

	"github.com/natalie/eyrie/internal/config"
	"github.com/natalie/eyrie/internal/tui"
	"github.com/spf13/cobra"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Launch the interactive terminal UI",
	Long:  "Start an interactive terminal interface for monitoring and managing Claw agents.",
	RunE:  runTUI,
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}

func runTUI(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	return tui.Run(cfg)
}
