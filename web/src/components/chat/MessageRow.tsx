import type { ChatPart } from "../../lib/types";
import { ToolRunCard, groupPartsIntoRuns } from "./ToolCallCard";
import { roleLabel, roleColor } from "./MessageHeader";

export interface MessageRowProps {
  msg: {
    timestamp: string;
    role: string;
    content: string;
    parts?: ChatPart[];
    sender?: string;
    display_name?: string;
  };
  expanded: boolean;
  onToggle?: () => void;
}

export function MessageRow({ msg, expanded, onToggle }: MessageRowProps) {
  const parts = msg.parts ?? [];
  const toolCount = parts.filter((p) => p.type === "tool_call").length;
  const hasParts = parts.length > 0;
  const isLong = msg.content.length > 200 || toolCount > 0;
  const canToggle = isLong && onToggle;
  const displayText =
    isLong && !expanded
      ? msg.content.length > 200
        ? msg.content.slice(0, 200) + "..."
        : msg.content
      : msg.content;
  const toolSummary =
    !expanded && toolCount > 0
      ? ` [${toolCount} tool${toolCount > 1 ? "s" : ""}]`
      : "";

  return (
    <div
      className={`py-1 ${canToggle ? "cursor-pointer hover:bg-surface-hover/50 rounded px-1 -mx-1" : ""}`}
      onClick={
        canToggle
          ? () => {
              if (!window.getSelection()?.toString()) onToggle!();
            }
          : undefined
      }
      {...(canToggle ? {
        role: "button",
        tabIndex: 0,
        onKeyDown: (e: React.KeyboardEvent) => {
          if ((e.key === "Enter" || e.key === " ") && !window.getSelection()?.toString()) {
            if (e.key === " ") e.preventDefault();
            onToggle!();
          }
        },
      } : {})}
    >
      <span className="text-text-muted">
        {(() => { const d = new Date(msg.timestamp); return isNaN(d.getTime()) ? "-" : d.toLocaleTimeString(); })()}
      </span>{" "}
      <span className={`font-medium ${roleColor(msg.role)}`}>
        {roleLabel(msg.role, msg.display_name, msg.sender)}:
      </span>{" "}
      {!expanded && (
        <>
          <span className="text-text">{displayText}</span>
          {toolSummary && (
            <span className="ml-1 text-accent/60 text-[10px]">
              {toolSummary}
            </span>
          )}
        </>
      )}
      {canToggle && !expanded && <span className="ml-1 text-green">{"\u25B8"}</span>}
      {canToggle && expanded && <span className="ml-1 text-green">{"\u25BE"}</span>}
      {expanded && hasParts && (
        <div className="mt-0.5" onClick={(e) => e.stopPropagation()}>
          {groupPartsIntoRuns(parts).map((run, ri) =>
            run.type === "text" ? (
              <div
                key={`text-${ri}`}
                className="text-text whitespace-pre-wrap py-0.5"
              >
                {run.text}
              </div>
            ) : (
              <ToolRunCard key={`run-${ri}`} tools={run.tools} />
            ),
          )}
        </div>
      )}
      {expanded && !hasParts && msg.content && (
        <span className="text-text whitespace-pre-wrap">{msg.content}</span>
      )}
    </div>
  );
}
