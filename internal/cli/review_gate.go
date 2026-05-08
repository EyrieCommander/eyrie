package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Audacity88/eyrie/internal/config"
	"github.com/spf13/cobra"
)

const defaultReviewGateTimeout = 5 * time.Minute

type reviewGateOptions struct {
	input        string
	out          string
	runner       string
	model        string
	baseURL      string
	maxFileBytes int
	maxTokens    int
	timeout      time.Duration
}

var reviewGateFlags reviewGateOptions

var reviewGateCmd = &cobra.Command{
	Use:   "review-gate",
	Short: "Run the ZeroClaw OpenRouter review gate with Eyrie vault keys",
	Long: `Run the optional ZeroClaw OpenRouter/Grok review gate while sourcing
OPENROUTER_API_KEY from Eyrie's key vault. This command does not post to GitHub
or expose the key; it only injects the key into the child review-gate process.`,
	RunE: runReviewGate,
}

func init() {
	rootCmd.AddCommand(reviewGateCmd)
	reviewGateCmd.Flags().StringVar(&reviewGateFlags.input, "input", "", "Gate bundle or gate-input-* directory")
	reviewGateCmd.Flags().StringVar(&reviewGateFlags.out, "out", "", "Markdown result path to write")
	reviewGateCmd.Flags().StringVar(&reviewGateFlags.runner, "runner", "", "OpenRouter gate runner path")
	reviewGateCmd.Flags().StringVar(&reviewGateFlags.model, "model", "", "OpenRouter model id")
	reviewGateCmd.Flags().StringVar(&reviewGateFlags.baseURL, "base-url", "", "OpenRouter API base URL")
	reviewGateCmd.Flags().IntVar(&reviewGateFlags.maxFileBytes, "max-file-bytes", 0, "Maximum bytes to include per file")
	reviewGateCmd.Flags().IntVar(&reviewGateFlags.maxTokens, "max-tokens", 0, "Maximum completion tokens")
	reviewGateCmd.Flags().DurationVar(&reviewGateFlags.timeout, "timeout", defaultReviewGateTimeout, "Review gate timeout")
}

func runReviewGate(cmd *cobra.Command, args []string) error {
	if reviewGateFlags.input == "" {
		return errors.New("--input is required")
	}
	if reviewGateFlags.out == "" {
		return errors.New("--out is required")
	}

	runner, err := resolveReviewGateRunner(reviewGateFlags.runner)
	if err != nil {
		return err
	}

	vault := config.GetKeyVault()
	apiKey := ""
	var vaultEnv []string
	if vault != nil {
		apiKey = vault.Get("openrouter")
		vaultEnv = vault.EnvSlice()
	}
	if apiKey == "" {
		return errors.New("no OpenRouter key found; add provider \"openrouter\" in Eyrie settings or set OPENROUTER_API_KEY")
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), reviewGateFlags.timeout)
	defer cancel()

	childArgs := reviewGateRunnerArgs(reviewGateFlags)
	child := exec.CommandContext(ctx, "python3", append([]string{runner}, childArgs...)...)
	child.Env = reviewGateEnv(config.EnrichedEnv(), vaultEnv, apiKey, reviewGateFlags.model)
	child.Stdin = os.Stdin
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr

	if err := child.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("review gate timed out after %s", reviewGateFlags.timeout)
		}
		return fmt.Errorf("review gate failed: %w", err)
	}
	return nil
}

func reviewGateRunnerArgs(flags reviewGateOptions) []string {
	args := []string{"--input", flags.input, "--out", flags.out}
	if flags.model != "" {
		args = append(args, "--model", flags.model)
	}
	if flags.baseURL != "" {
		args = append(args, "--base-url", flags.baseURL)
	}
	if flags.maxFileBytes > 0 {
		args = append(args, "--max-file-bytes", strconv.Itoa(flags.maxFileBytes))
	}
	if flags.maxTokens > 0 {
		args = append(args, "--max-tokens", strconv.Itoa(flags.maxTokens))
	}
	return args
}

func reviewGateEnv(baseEnv, vaultEnv []string, apiKey, model string) []string {
	env := append([]string{}, baseEnv...)
	env = append(env, vaultEnv...)
	env = setEnv(env, "OPENROUTER_API_KEY", apiKey)
	if model != "" {
		env = setEnv(env, "OPENROUTER_REVIEW_GATE_MODEL", model)
	}
	return env
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	next := make([]string, 0, len(env)+1)
	for _, item := range env {
		if len(item) >= len(prefix) && item[:len(prefix)] == prefix {
			continue
		}
		next = append(next, item)
	}
	return append(next, prefix+value)
}

func resolveReviewGateRunner(explicit string) (string, error) {
	if explicit != "" {
		return requireRegularFile(explicit)
	}

	candidates := []string{
		os.Getenv("EYRIE_REVIEW_GATE_RUNNER"),
		"/Users/natalie/Development/.agents/skills/zeroclaw-pr-review-session/scripts/run-openrouter-gate.py",
		filepath.Join(os.Getenv("HOME"), "Development/.agents/skills/zeroclaw-pr-review-session/scripts/run-openrouter-gate.py"),
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if path, err := requireRegularFile(candidate); err == nil {
			return path, nil
		}
	}

	return "", errors.New("cannot find OpenRouter gate runner; pass --runner or set EYRIE_REVIEW_GATE_RUNNER")
}

func requireRegularFile(path string) (string, error) {
	expanded := os.ExpandEnv(path)
	if !filepath.IsAbs(expanded) {
		abs, err := filepath.Abs(expanded)
		if err != nil {
			return "", err
		}
		expanded = abs
	}
	info, err := os.Stat(expanded)
	if err != nil {
		return "", fmt.Errorf("review gate runner not found: %s", expanded)
	}
	if info.IsDir() {
		return "", fmt.Errorf("review gate runner is a directory: %s", expanded)
	}
	return expanded, nil
}
