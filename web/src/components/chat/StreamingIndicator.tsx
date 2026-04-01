// StreamingIndicator.tsx — Shared streaming state rendering for both chat types.
//
// Renders the "thinking..." / streaming content / tool calls / stop button
// that appears while an agent is responding. Used by both ChatPanel (1:1)
// and ProjectChat (multi-agent).

import { PartToolCallCard } from "./ToolCallCard";
import { StreamingCursor } from "./StreamingCursor";

export interface StreamingPart {
  kind: "text" | "tool";
  // text parts
  content?: string;
  // tool parts
  name?: string;
  done?: boolean;
  args?: any;
  output?: string;
}

export interface StreamingIndicatorProps {
  /** Interleaved text + tool parts being streamed */
  parts: StreamingPart[];
  /** Called when the user clicks stop */
  onStop?: () => void;
  /** Extra content above the streaming parts (e.g., message header) */
  header?: React.ReactNode;
}

export function StreamingIndicator({ parts, onStop, header }: StreamingIndicatorProps) {
  return (
    <div className="text-xs">
      {header}
      {parts.length === 0 && (
        <div className="mt-0.5 text-text-muted animate-pulse">thinking...</div>
      )}
      {parts.length > 0 && (
        <div className="mt-1 space-y-1">
          {parts.map((part, i) =>
            part.kind === "tool" ? (
              <PartToolCallCard
                key={i}
                part={{
                  type: "tool_call",
                  name: part.name || "tool",
                  args: part.args,
                  output: part.output,
                  pending: !part.done,
                }}
              />
            ) : (
              <div key={i} className="text-text whitespace-pre-wrap">
                {part.content}<StreamingCursor />
              </div>
            )
          )}
        </div>
      )}
      {onStop && (
        <button
          onClick={onStop}
          className="mt-1.5 rounded border border-border px-2 py-0.5 text-[10px] text-text-muted hover:border-red/50 hover:text-red transition-colors"
        >
          stop
        </button>
      )}
    </div>
  );
}
