// MessageHeader.tsx — Shared role colors and header label for chat messages.
//
// Used by both 1:1 chat (MessageRow, inline) and project chat (block layout).
// Centralizes role → color mapping so new roles only need one update.

export const ROLE_COLORS: Record<string, string> = {
  user: "text-green",
  assistant: "text-purple",
  commander: "text-purple",
  captain: "text-yellow-400",
  talon: "text-blue-400",
  system: "text-text-muted",
};

/** Returns the display label for a message sender. */
export function roleLabel(role: string, displayName?: string, sender?: string): string {
  if (role === "user") return "you";
  return displayName || sender || role;
}

/** Returns the CSS color class for a role. */
export function roleColor(role: string): string {
  return ROLE_COLORS[role] || "text-purple";
}
