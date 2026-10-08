package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Audacity88/eyrie/internal/bridge"
	"github.com/Audacity88/eyrie/internal/config"
	"github.com/Audacity88/eyrie/internal/server"
	"github.com/spf13/cobra"
)

var bridgeCmd = &cobra.Command{
	Use:   "bridge",
	Short: "Manage the chief bridge listener (off unless ~/.eyrie/bridge.toml exists)",
}

var bridgeTokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Manage the bridge bearer token",
}

var bridgeTokenRotateCmd = &cobra.Command{
	Use:   "rotate",
	Short: "Generate a new bridge token, print it once, and store only its hash",
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := bridge.DefaultConfigPath()
		if err != nil {
			return err
		}
		tok, err := bridge.RotateToken(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "New bridge token (shown once; only its hash is saved in %s):\n\n  %s\n\n", path, tok)
		fmt.Fprintln(cmd.OutOrStdout(), "Give it to the chief through a private secret request. Restart `eyrie dashboard` to apply.")
		return nil
	},
}

var bridgeCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Validate the bridge config without starting anything",
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := bridge.DefaultConfigPath()
		if err != nil {
			return err
		}
		cfg, err := bridge.LoadConfig(path)
		if errors.Is(err, bridge.ErrNotConfigured) {
			fmt.Fprintf(cmd.OutOrStdout(), "bridge off: %s does not exist\n", path)
			return nil
		}
		if err != nil {
			return err
		}
		if dash, derr := config.Load(); derr == nil {
			if why := bridgeConflict(cfg, dash); why != "" {
				return errors.New(why)
			}
		}
		fsys, rerrs := bridge.NewFS(cfg.Roots, cfg.ExtraDeny)
		defer fsys.Close()
		for _, e := range rerrs {
			fmt.Fprintf(cmd.OutOrStdout(), "warning: %v (root skipped)\n", e)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "bridge ok: 127.0.0.1:%d, roots %v, chief wake configured: %v\n", cfg.Port, fsys.Aliases(), cfg.WakeConfigured())
		return nil
	},
}

func init() {
	bridgeTokenCmd.AddCommand(bridgeTokenRotateCmd)
	bridgeCmd.AddCommand(bridgeTokenCmd, bridgeCheckCmd)
	rootCmd.AddCommand(bridgeCmd)
}

// startBridge starts the bridge listener when configured. It returns a stop
// func (always non-nil). Any config problem leaves the bridge off and is
// logged; the dashboard itself still runs.
// bridgeConflict reports why a bridge config can't run next to this
// dashboard, or "" if it can. The bridge binds first, so a bridge port equal
// to the dashboard port would take the dashboard's port and leave
// management unable to start.
func bridgeConflict(cfg bridge.Config, dash config.Config) string {
	if cfg.Port == dash.Dashboard.Port {
		return fmt.Sprintf("bridge port %d is the dashboard port; pick another (default %d)", cfg.Port, bridge.DefaultPort)
	}
	return ""
}

func startBridge(srv *server.Server, dash config.Config) func() {
	noop := func() {}
	path, err := bridge.DefaultConfigPath()
	if err != nil {
		slog.Warn("bridge off", "error", err)
		return noop
	}
	cfg, err := bridge.LoadConfig(path)
	if errors.Is(err, bridge.ErrNotConfigured) {
		return noop
	}
	if err != nil {
		slog.Error("bridge refused to start", "error", err)
		return noop
	}
	if why := bridgeConflict(cfg, dash); why != "" {
		slog.Error("bridge refused to start", "error", why)
		return noop
	}
	fsys, rerrs := bridge.NewFS(cfg.Roots, cfg.ExtraDeny)
	for _, e := range rerrs {
		slog.Warn("bridge root skipped", "error", e)
	}
	storePath, err := bridge.DefaultStorePath()
	if err != nil {
		fsys.Close()
		slog.Error("bridge refused to start", "error", err)
		return noop
	}
	store, err := bridge.OpenStore(storePath)
	if err != nil {
		fsys.Close()
		slog.Error("bridge refused to start: store", "error", err)
		return noop
	}
	logPath := cfg.AccessLog
	if logPath == "" {
		if logPath, err = bridge.DefaultAccessLogPath(); err != nil {
			fsys.Close()
			store.Close()
			slog.Error("bridge refused to start", "error", err)
			return noop
		}
	}
	alog, err := bridge.OpenAccessLog(logPath)
	if err != nil {
		fsys.Close()
		store.Close()
		slog.Error("bridge refused to start: access log", "error", err)
		return noop
	}
	svc := bridge.NewService(store, cfg)
	bs := bridge.NewServer(cfg, fsys, svc, alog)
	// Bind first. If the port is taken, the Chief front door stays off:
	// sending prompts with nowhere for replies to land would strand them.
	ln, err := bs.Listen()
	if err != nil {
		svc.Close()
		fsys.Close()
		_ = alog.Close()
		_ = store.Close()
		slog.Error("bridge refused to start", "error", err)
		return noop
	}
	srv.AttachChief(svc)
	svc.ResumePending()
	go func() {
		if err := bs.ServeListener(ln); err != nil {
			slog.Error("bridge listener stopped", "error", err)
		}
	}()
	slog.Info("bridge listening", "addr", bs.Addr(), "roots", fsys.Aliases(), "chief_wake", cfg.WakeConfigured())
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = bs.Shutdown(ctx)
		svc.Close()
		fsys.Close()
		_ = alog.Close()
		_ = store.Close()
	}
}
