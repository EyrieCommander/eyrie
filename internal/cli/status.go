package cli

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/Audacity88/eyrie/internal/adapter"
	"github.com/Audacity88/eyrie/internal/config"
	"github.com/Audacity88/eyrie/internal/discovery"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status [agent-name]",
	Short: "Show status of discovered agents",
	Long:  "Discover all running Claw agents and show their health. Optionally pass an agent name for detailed info.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result := discovery.Run(ctx, cfg)

	if len(result.Agents) == 0 {
		fmt.Println("No agents discovered. Check config paths or add agents to ~/.eyrie/config.toml")
		return nil
	}

	// If a specific agent was requested, show detailed view
	if len(args) == 1 {
		return showDetailedStatus(ctx, args[0], result)
	}

	return showStatusTable(ctx, result)
}

func showStatusTable(ctx context.Context, result discovery.Result) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "AGENT\tFRAMEWORK\tSTATUS\tPROVIDER\tPORT\tUPTIME")
	fmt.Fprintln(w, "─────\t─────────\t──────\t────────\t────\t──────")

	for _, ar := range result.Agents {
		status := "● stopped"
		uptime := "-"
		provider := "-"

		if ar.Alive {
			status = "● running"
			agent := discovery.NewAgent(ar.Agent)
			if health, err := agent.Health(ctx); err == nil && health.Alive {
				uptime = formatDuration(health.Uptime)
			}
			if st, err := agent.Status(ctx); err == nil {
				name := st.Provider
				if name == "" {
					name = "-"
				}
				switch adapter.ProbeProvider(ctx, st.Provider) {
				case "ok":
					provider = "● " + name
				case "error":
					provider = "▲ " + name
				default:
					provider = name
				}
			}
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\n",
			ar.Agent.Name,
			ar.Agent.Framework,
			status,
			provider,
			ar.Agent.Port,
			uptime,
		)
	}

	return w.Flush()
}

func showDetailedStatus(ctx context.Context, name string, result discovery.Result) error {
	var found *discovery.AgentResult
	for i := range result.Agents {
		if result.Agents[i].Agent.Name == name {
			found = &result.Agents[i]
			break
		}
	}

	if found == nil {
		return fmt.Errorf("agent %q not found. Run 'eyrie discover' to see available agents", name)
	}

	fmt.Printf("Agent: %s\n", found.Agent.Name)
	fmt.Printf("Framework: %s\n", found.Agent.Framework)
	fmt.Printf("Gateway: %s:%d\n", found.Agent.Host, found.Agent.Port)
	fmt.Printf("Config: %s\n", found.Agent.ConfigPath)

	if !found.Alive {
		fmt.Println("Status: stopped")
		return nil
	}

	fmt.Println("Status: running")

	agent := discovery.NewAgent(found.Agent)

	if health, err := agent.Health(ctx); err == nil {
		fmt.Printf("PID: %d\n", health.PID)
		fmt.Printf("Uptime: %s\n", formatDuration(health.Uptime))
		if len(health.Components) > 0 {
			fmt.Println("Components:")
			for name, c := range health.Components {
				fmt.Printf("  %s: %s", name, c.Status)
				if c.RestartCount > 0 {
					fmt.Printf(" (restarts: %d)", c.RestartCount)
				}
				if c.LastError != "" {
					fmt.Printf(" [error: %s]", c.LastError)
				}
				fmt.Println()
			}
		}
	}

	if status, err := agent.Status(ctx); err == nil {
		if status.Provider != "" {
			ps := adapter.ProbeProvider(ctx, status.Provider)
			switch ps {
			case "ok":
				fmt.Printf("Provider: %s (ok)\n", status.Provider)
			case "error":
				fmt.Printf("Provider: %s (unreachable)\n", status.Provider)
			default:
				fmt.Printf("Provider: %s\n", status.Provider)
			}
		}
		if status.Model != "" {
			fmt.Printf("Model: %s\n", status.Model)
		}
		if len(status.Channels) > 0 {
			fmt.Printf("Channels: %v\n", status.Channels)
		}
	}

	return nil
}

func formatDuration(d time.Duration) string {
	if d == 0 {
		return "-"
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}
