package cli

import (
	"fmt"

	"github.com/Audacity88/eyrie/internal/instance"
	"github.com/spf13/cobra"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Update provisioned instance configs to current defaults",
	Long: `Applies idempotent config migrations to all provisioned instances.

Each migration rule ensures a config value matches the current provisioner
defaults. Only values that differ from the expected value are updated —
custom user settings are preserved unless they conflict with a required fix.

Safe to run repeatedly. Changes are written atomically.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		results, err := instance.MigrateAll()
		if err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}

		applied := 0
		skipped := 0
		errored := 0
		for _, r := range results {
			if r.Error != "" {
				cmd.Printf("  ✗ %s (%s): %s\n", r.InstanceName, r.Framework, r.Error)
				errored++
			} else if r.Skipped {
				cmd.Printf("  · %s (%s): up to date\n", r.InstanceName, r.Framework)
				skipped++
			} else {
				cmd.Printf("  ✓ %s (%s):\n", r.InstanceName, r.Framework)
				for _, change := range r.Applied {
					cmd.Printf("      %s\n", change)
				}
				applied++
			}
		}

		cmd.Printf("\n%d migrated, %d up to date, %d errors\n", applied, skipped, errored)
		if errored > 0 {
			return fmt.Errorf("%d instance(s) failed to migrate", errored)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(migrateCmd)
}
