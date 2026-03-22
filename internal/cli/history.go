package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/Audacity88/eyrie/internal/adapter"
	"github.com/Audacity88/eyrie/internal/config"
	"github.com/Audacity88/eyrie/internal/discovery"
	"github.com/spf13/cobra"
)

var historyCmd = &cobra.Command{
	Use:   "history <agent-name>",
	Short: "View conversation history for an agent",
	Long:  "List recent sessions or view messages for a specific session. Requires a framework that supports conversation history (e.g. OpenClaw).",
	Args:  cobra.ExactArgs(1),
	RunE:  runHistory,
}

var historySession string
var historyLimit int

func init() {
	historyCmd.Flags().StringVar(&historySession, "session", "", "Session key to view messages for")
	historyCmd.Flags().IntVar(&historyLimit, "limit", 20, "Max number of messages to show")
	rootCmd.AddCommand(historyCmd)
}

func runHistory(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result := discovery.Run(ctx, cfg)

	name := args[0]
	for _, ar := range result.Agents {
		if ar.Agent.Name == name {
			if !ar.Alive {
				return fmt.Errorf("agent %q is not running", name)
			}

			agent := discovery.NewAgent(ar.Agent)

			if historySession != "" {
				return showChatHistory(ctx, agent, name, historySession)
			}
			return listSessions(ctx, agent, name)
		}
	}

	return fmt.Errorf("agent %q not found. Run 'eyrie discover' to see available agents", name)
}

func listSessions(ctx context.Context, agent interface {
	Sessions(context.Context) ([]adapter.Session, error)
}, name string) error {
	sessions, err := agent.Sessions(ctx)
	if err != nil {
		return fmt.Errorf("fetching sessions: %w", err)
	}

	if sessions == nil || len(sessions) == 0 {
		fmt.Printf("No sessions found for %s (this framework may not support conversation history).\n", name)
		return nil
	}

	fmt.Printf("Sessions for %s:\n\n", name)
	for _, s := range sessions {
		title := s.Title
		if title == "" {
			title = s.Key
		}
		age := ""
		if s.LastMsg != nil {
			age = fmt.Sprintf(" (%s)", s.LastMsg.Format("2006-01-02 15:04"))
		}
		channel := ""
		if s.Channel != "" {
			channel = fmt.Sprintf(" [%s]", s.Channel)
		}
		fmt.Printf("  %s%s%s\n", title, channel, age)
	}

	fmt.Printf("\nUse --session <key> to view messages for a session.\n")
	return nil
}

func showChatHistory(ctx context.Context, agent interface {
	ChatHistory(context.Context, string, int) ([]adapter.ChatMessage, error)
}, name, sessionKey string) error {
	messages, err := agent.ChatHistory(ctx, sessionKey, historyLimit)
	if err != nil {
		return fmt.Errorf("fetching chat history: %w", err)
	}

	if messages == nil || len(messages) == 0 {
		fmt.Printf("No messages found for session %q on %s.\n", sessionKey, name)
		return nil
	}

	fmt.Printf("Chat history for %s (session %s):\n\n", name, sessionKey)
	for _, m := range messages {
		ts := ""
		if !m.Timestamp.IsZero() {
			ts = m.Timestamp.Format("15:04") + " "
		}

		content := m.Content
		if len(content) > 500 {
			content = content[:500] + "..."
		}

		fmt.Printf("%s%s: %s\n\n", ts, m.Role, content)
	}

	return nil
}
