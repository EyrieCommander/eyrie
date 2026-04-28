// Phase 0: Commander setup.
//
// The commander backend is now merged. This card shows live status:
// health check, history count, memory count, and a clear button.
// If the endpoint is unreachable (missing API key), shows a yellow
// "not configured" card instead.

import { useCallback, useEffect, useState } from "react";
import { Check, Trash2, Brain, ChevronDown, ChevronRight, Key } from "lucide-react";
import {
  fetchCommanderHistory,
  fetchCommanderMemory,
  clearCommanderHistory,
} from "../../lib/api";
import type { MemoryEntry } from "../../lib/types";
import ApiKeysSection from "../ApiKeysSection";

export default function CommanderPhase({ onContinue }: { onContinue?: () => void }) {
  const [healthy, setHealthy] = useState<boolean | null>(null);
  const [historyCount, setHistoryCount] = useState(0);
  const [memories, setMemories] = useState<MemoryEntry[]>([]);
  const [detailsOpen, setDetailsOpen] = useState(false);
  const [clearing, setClearing] = useState(false);
  const [clearError, setClearError] = useState(false);

  const check = useCallback(async () => {
    try {
      const history = await fetchCommanderHistory();
      setHealthy(true);
      setHistoryCount(history.length);
    } catch {
      setHealthy(false);
    }
    try {
      const mem = await fetchCommanderMemory();
      setMemories(mem);
    } catch { /* advisory */ }
  }, []);

  useEffect(() => { check(); }, [check]);

  const handleClear = async () => {
    setClearing(true);
    setClearError(false);
    try {
      await clearCommanderHistory();
      setHistoryCount(0);
    } catch {
      setClearError(true);
    } finally {
      setClearing(false);
    }
  };

  // Loading
  if (healthy === null) {
    return (
      <div className="rounded border border-border bg-surface p-6 text-center text-xs text-text-muted">
        checking commander...
      </div>
    );
  }

  // Unhealthy — guide the user through adding an API key
  if (!healthy) {
    return <CommanderSetupCard onKeySaved={check} />;
  }

  // Healthy
  return (
    <div className="rounded border border-border bg-surface p-6 space-y-4">
      <div className="flex items-center gap-3">
        <div className="flex h-8 w-8 items-center justify-center rounded-full bg-green/20 text-green text-sm">
          <Check className="h-4 w-4" />
        </div>
        <div className="flex-1">
          <div className="text-sm font-semibold text-text">Eyrie is your commander</div>
          <p className="text-xs text-text-muted mt-0.5">
            Ask me anything via the chat panel &rarr;
          </p>
        </div>
        {onContinue && (
          <button
            onClick={onContinue}
            className="rounded bg-accent px-4 py-1.5 text-xs font-medium text-white hover:bg-accent-hover transition-colors"
          >
            continue to frameworks &rarr;
          </button>
        )}
      </div>

      {/* Collapsible details + key management */}
      <button
        onClick={() => setDetailsOpen(!detailsOpen)}
        className="flex items-center gap-1.5 text-[10px] text-text-muted hover:text-text transition-colors"
        aria-expanded={detailsOpen}
        aria-controls="commander-settings"
      >
        {detailsOpen ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
        settings
      </button>

      {detailsOpen && (
        <div id="commander-settings" className="space-y-3 border-t border-border pt-3">
          {/* API key management */}
          <ApiKeysSection compact onChanged={check} />

          {/* History + memory */}
          <div className="space-y-2 text-xs">
            <div className="flex items-center justify-between">
              <span className="text-text-muted">history</span>
              <div className="flex items-center gap-2">
                <span className="text-text">{historyCount} messages</span>
                {historyCount > 0 && (
                  <>
                    <button
                      onClick={handleClear}
                      disabled={clearing}
                      className="flex items-center gap-1 text-[10px] text-text-muted hover:text-red transition-colors disabled:opacity-40"
                    >
                      <Trash2 className="h-3 w-3" />
                      clear
                    </button>
                    {clearError && <span className="text-[10px] text-red">failed</span>}
                  </>
                )}
              </div>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-text-muted">memory</span>
              <div className="flex items-center gap-1 text-text">
                <Brain className="h-3 w-3 text-purple" />
                {memories.length} entries
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

/** Setup card shown when the commander has no API key. Uses the same
 *  key management UI as the Settings page. */
function CommanderSetupCard({ onKeySaved }: { onKeySaved: () => void }) {
  return (
    <div className="rounded border border-border bg-surface p-6 space-y-4">
      <div className="flex items-center gap-3">
        <div className="flex h-8 w-8 items-center justify-center rounded-full bg-accent/20 text-accent text-sm">
          <Key className="h-4 w-4" />
        </div>
        <div>
          <div className="text-sm font-semibold text-text">Set up your commander</div>
          <p className="text-xs text-text-muted mt-0.5">
            Eyrie needs an LLM API key to power the commander. Add one below to get started.
          </p>
        </div>
      </div>

      <ApiKeysSection compact onChanged={onKeySaved} />
    </div>
  );
}
