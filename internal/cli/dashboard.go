package cli

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/natalie/eyrie/internal/config"
	"github.com/natalie/eyrie/internal/server"
	"github.com/spf13/cobra"
)

var dashboardNoOpen bool

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Start the Eyrie web dashboard",
	RunE:  runDashboard,
}

func init() {
	dashboardCmd.Flags().BoolVar(&dashboardNoOpen, "no-open", false, "Don't open the browser automatically")
	rootCmd.AddCommand(dashboardCmd)
}

func runDashboard(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	url := fmt.Sprintf("http://%s:%d", cfg.Dashboard.Host, cfg.Dashboard.Port)

	if isEyrieDashboardRunning(cfg.Dashboard.Host, cfg.Dashboard.Port) {
		fmt.Printf("Dashboard already running at %s\n", url)
		if cfg.Dashboard.OpenBrowser && !dashboardNoOpen {
			openBrowser(url)
		}
		return nil
	}

	srv := server.New(cfg)

	if cfg.Dashboard.OpenBrowser && !dashboardNoOpen {
		go openBrowser(url)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		if !dashboardNoOpen {
			slog.Info("Shutting down dashboard...")
		}
		if err := srv.Shutdown(context.Background()); err != nil {
			slog.Error("shutdown error", "error", err)
		}
	}()

	if !dashboardNoOpen {
		fmt.Printf("Eyrie dashboard: %s\n", url)
		fmt.Println("Press Ctrl+C to stop.")
	}

	if err := srv.Start(); err != nil && ctx.Err() == nil {
		return err
	}

	return nil
}

// isEyrieDashboardRunning checks if an Eyrie dashboard is already serving on the given address
// by hitting the /api/agents endpoint (distinguishes Eyrie from some other process on the port).
func isEyrieDashboardRunning(host string, port int) bool {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		return false
	}
	conn.Close()

	// Port is open -- verify it's actually Eyrie by probing our API
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://%s/api/agents", addr))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return
	}
	cmd.Run()
}
