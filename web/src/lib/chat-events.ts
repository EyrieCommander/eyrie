export interface ToolCall {
  tool: string;
  toolId?: string;
  args?: Record<string, unknown>;
  output?: string;
  success?: boolean;
  done: boolean;
}

/**
 * Match a tool_result event to the most recent unfinished tool_start entry
 * in the toolCalls array (reverse scan by tool_id or tool name).
 * Returns a new array with the matched entry updated.
 */
export function matchToolResult(
  toolCalls: ToolCall[],
  event: { tool_id?: string; tool?: string; output?: string; success?: boolean },
): ToolCall[] {
  if (!event.tool_id && !event.tool) return toolCalls;
  const updated = [...toolCalls];
  let idx = -1;
  for (let i = updated.length - 1; i >= 0; i--) {
    if (
      ((event.tool_id && updated[i].toolId === event.tool_id) ||
        (!event.tool_id && updated[i].tool === event.tool)) &&
      !updated[i].done
    ) {
      idx = i;
      break;
    }
  }
  if (idx >= 0) {
    updated[idx] = {
      ...updated[idx],
      output: event.output,
      success: event.success,
      done: true,
    };
  }
  return updated;
}
