/**
 * Reads an SSE stream from a ReadableStream, parsing "data: " prefixed lines
 * and calling onEvent with each parsed JSON payload.
 */
export async function readSSEStream(
  body: ReadableStream<Uint8Array>,
  onEvent: (data: any) => void,
): Promise<void> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (!done) {
        buffer += decoder.decode(value, { stream: true });
      } else {
        buffer += decoder.decode();
      }
      const lines = buffer.split(/\r?\n/);
      buffer = lines.pop()!;
      for (const line of lines) {
        if (line.startsWith("data: ")) {
          try {
            onEvent(JSON.parse(line.slice(6)));
          } catch {
            // skip malformed SSE lines
          }
        }
      }
      if (done) {
        // Process any trailing data left in buffer
        if (buffer.startsWith("data: ")) {
          try {
            onEvent(JSON.parse(buffer.slice(6)));
          } catch {
            /* skip */
          }
        }
        break;
      }
    }
  } finally {
    reader.releaseLock();
  }
}
