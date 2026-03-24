package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Audacity88/eyrie/internal/config"
	"github.com/Audacity88/eyrie/internal/discovery"
	"github.com/spf13/cobra"
)

var pairCmd = &cobra.Command{
	Use:   "pair <agent-name> <pairing-code>",
	Short: "Pair with an agent's gateway to enable authenticated access",
	Long: `Exchange a one-time pairing code for a bearer token.

ZeroClaw gateways display a 6-digit pairing code on startup when
require_pairing is enabled. Copy that code and run:

  eyrie pair zeroclaw 123456

The resulting bearer token is stored in ~/.eyrie/tokens.json and used
automatically for all subsequent API calls (logs, activity, etc.).`,
	Args: cobra.ExactArgs(2),
	RunE: runPair,
}

func init() {
	rootCmd.AddCommand(pairCmd)
}

func runPair(cmd *cobra.Command, args []string) error {
	agentName := args[0]
	pairingCode := args[1]

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result := discovery.Run(ctx, cfg)

	for _, ar := range result.Agents {
		if ar.Agent.Name != agentName {
			continue
		}
		if !ar.Alive {
			return fmt.Errorf("agent %q is not running", agentName)
		}

		baseURL := ar.Agent.URL()
		token, err := doPair(ctx, baseURL, pairingCode)
		if err != nil {
			return err
		}

		store, err := config.NewTokenStore()
		if err != nil {
			return fmt.Errorf("opening token store: %w", err)
		}
		if err := store.Set(agentName, token); err != nil {
			return fmt.Errorf("saving token: %w", err)
		}

		fmt.Printf("Paired with %s successfully. Token stored in ~/.eyrie/tokens.json\n", agentName)
		return nil
	}

	return fmt.Errorf("agent %q not found. Run 'eyrie discover' to see available agents", agentName)
}

func doPair(ctx context.Context, baseURL, code string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/pair", nil)
	if err != nil {
		return "", fmt.Errorf("creating pair request: %w", err)
	}
	req.Header.Set("X-Pairing-Code", code)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("pairing request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusForbidden {
		return "", fmt.Errorf("invalid pairing code")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return "", fmt.Errorf("too many pairing attempts — try again later")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("pairing failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Paired bool   `json:"paired"`
		Token  string `json:"token"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing pair response: %w", err)
	}
	if result.Token == "" {
		return "", fmt.Errorf("server returned empty token")
	}

	return result.Token, nil
}
