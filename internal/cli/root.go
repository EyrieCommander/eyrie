package cli

import (
	"github.com/natalie/eyrie/internal/config"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "eyrie",
	Short: "Unified management for the Claw agent ecosystem",
	Long:  "Eyrie discovers, monitors, and controls all your Claw agents from one place.",
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print Eyrie version",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Printf("eyrie %s\n", config.Version)
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
