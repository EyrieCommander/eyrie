import { Plus, X } from "lucide-react";
import type { Session } from "../../lib/types";

export interface SessionGroup {
  name: string;
  current?: Session;
  archived: Session[];
}

export interface SessionBarProps {
  groups: SessionGroup[];
  activeGroupName: string;
  onSelectGroup: (name: string) => void;
  onCreateSession: (name: string) => void;
  onDestroySession: (group: SessionGroup) => void;
  creatingSession: boolean;
  onSetCreating: (creating: boolean) => void;
  newSessionName: string;
  onNewSessionNameChange: (name: string) => void;
}

export function SessionBar({
  groups,
  activeGroupName,
  onSelectGroup,
  onCreateSession,
  onDestroySession,
  creatingSession,
  onSetCreating,
  newSessionName,
  onNewSessionNameChange,
}: SessionBarProps) {
  if (groups.length === 0) return null;

  return (
    <div className="flex items-center gap-1 overflow-x-auto scrollbar-hide rounded-t border border-b-0 border-border bg-bg-sidebar px-3 py-2">
      {groups.map((g) => (
        <div key={g.name} className="group/tab relative shrink-0">
          <button
            onClick={() => onSelectGroup(g.name)}
            className={`shrink-0 rounded px-3 py-1 text-[11px] font-medium transition-colors ${
              activeGroupName === g.name
                ? "bg-surface-hover text-accent"
                : "text-text-secondary hover:text-text hover:bg-surface-hover/50"
            }`}
          >
            {g.name}
            {g.archived.length > 0 && (
              <span className="ml-1 text-[9px] text-text-muted">
                +{g.archived.length}
              </span>
            )}
          </button>
          <button
            onClick={(e) => {
              e.stopPropagation();
              onDestroySession(g);
            }}
            className="absolute -top-1.5 -right-1.5 hidden group-hover/tab:flex group-focus-within/tab:flex h-4 w-4 items-center justify-center rounded-full bg-surface border border-border text-text-muted transition-colors hover:text-red hover:border-red/50"
            title={`Delete session "${g.name}"`}
          >
            <X className="h-2 w-2" />
          </button>
        </div>
      ))}

      {creatingSession ? (
        <div className="flex items-center gap-1 ml-1">
          <input
            type="text"
            value={newSessionName}
            onChange={(e) => onNewSessionNameChange(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && newSessionName.trim()) onCreateSession(newSessionName.trim());
              if (e.key === "Escape") {
                onSetCreating(false);
                onNewSessionNameChange("");
              }
            }}
            placeholder="session name"
            className="w-24 rounded border border-border bg-surface px-2 py-0.5 text-[11px] text-text placeholder:text-text-muted focus:outline-none focus:border-accent"
            autoFocus
          />
          <button
            onClick={() => onCreateSession(newSessionName.trim())}
            disabled={!newSessionName.trim()}
            className="rounded px-1.5 py-0.5 text-[11px] text-accent hover:bg-surface-hover disabled:opacity-30"
          >
            ok
          </button>
        </div>
      ) : (
        <button
          onClick={() => onSetCreating(true)}
          className="shrink-0 rounded p-1 text-text-muted transition-colors hover:text-accent hover:bg-surface-hover/50"
          title="New session"
        >
          <Plus className="h-3.5 w-3.5" />
        </button>
      )}
    </div>
  );
}
