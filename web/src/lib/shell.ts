// Shell quoting for commands sent to tmux terminals.
//
// Wraps a string in single quotes with proper escaping. Handles ~/
// paths by leaving the tilde unquoted so the shell expands it.

/** Quote a value for safe interpolation into a POSIX shell command.
 *  Tilde paths like ~/go/bin/picoclaw become ~/'go/bin/picoclaw' so
 *  the shell still expands ~ while the rest is safely quoted. */
export function shellQuote(s: string): string {
  if (s.startsWith("~/")) {
    const rest = s.slice(2);
    return `~/'${rest.replace(/'/g, `'\\''`)}'`;
  }
  return `'${s.replace(/'/g, `'\\''`)}'`;
}
