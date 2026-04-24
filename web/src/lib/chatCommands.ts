/** Mapping from framework safe-id to the chat/agent command used to launch it. */
export const CHAT_COMMANDS: Record<string, string> = {
  zeroclaw: "zeroclaw agent",
  openclaw: "openclaw tui",
  picoclaw: "picoclaw agent",
  hermes: "hermes",
};
