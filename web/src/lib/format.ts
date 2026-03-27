export function formatUptime(nanoseconds: number | null | undefined): string {
  if (nanoseconds == null) return "-";
  if (nanoseconds <= 0) return "0m";
  const seconds = nanoseconds / 1e9;
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const mins = Math.floor((seconds % 3600) / 60);
  if (days > 0) return `${days}d ${hours}h`;
  if (hours > 0) return `${hours}h ${mins}m`;
  return `${mins}m`;
}

/** Strip markdown bold markers (e.g., "** Magnus" → "Magnus") from display names. */
export function cleanDisplayName(name: string | undefined): string | undefined {
  if (!name) return name;
  return name.replace(/\*+/g, "").trim() || name;
}

export function formatBytes(bytes: number | null | undefined): string {
  if (bytes == null) return "-";
  if (bytes <= 0) return "0KB";
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)}KB`;
  if (bytes < 1024 * 1024 * 1024)
    return `${(bytes / (1024 * 1024)).toFixed(0)}MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)}GB`;
}
