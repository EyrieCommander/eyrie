import { useState, useEffect, useCallback } from "react";
import type { AgentInstance } from "../lib/types";
import { fetchInstances, createInstance, updateProject, streamCaptainBriefing, instanceAction } from "../lib/api";

export interface SetCaptainDialogProps {
  projectId: string;
  projectName: string;
  onDone: () => void;
  onClose: () => void;
}

export function SetCaptainDialog({
  projectId,
  projectName,
  onDone,
  onClose,
}: SetCaptainDialogProps) {
  const [mode, setMode] = useState<"create" | "existing">("create");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [startingCaptain, setStartingCaptain] = useState("");

  // Create new form — default name derived from project
  const defaultName = `captain-${projectName.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "")}`;
  const [name, setName] = useState("");
  const [framework, setFramework] = useState("openclaw");
  const [captainInstances, setCaptainInstances] = useState<AgentInstance[]>([]);

  const refreshInstances = useCallback(() => {
    fetchInstances().then((all) => {
      setCaptainInstances(all.filter((i) => i.hierarchy_role === "captain"));
    }).catch((err) => { console.error("Failed to fetch instances:", err); });
  }, []);

  useEffect(() => {
    refreshInstances();
  }, [refreshInstances]);

  const handleCreate = async () => {
    const effectiveName = name.trim() || defaultName;
    setSaving(true);
    setError("");
    try {
      const inst = await createInstance({
        name: effectiveName,
        framework,
        hierarchy_role: "captain",
        project_id: projectId,
        auto_start: true,
      });
      await updateProject(projectId, { orchestrator_id: inst.id });
      // Brief the captain on the project (fire and forget — it runs in background).
      // Note: the returned controller could be used to abort, but the briefing
      // should complete even after the dialog closes, so we intentionally let it run.
      streamCaptainBriefing(projectId, (ev) => {
        if (ev.type === "error") {
          console.error("Captain briefing failed:", ev.error);
        }
      });
      onDone();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to create captain");
    } finally {
      setSaving(false);
    }
  };

  const handleSelectExisting = async (agentName: string) => {
    setSaving(true);
    setError("");
    try {
      await updateProject(projectId, { orchestrator_id: agentName });
      onDone();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to set captain");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={onClose}>
      <div className="w-full max-w-md rounded border border-border bg-bg p-6 space-y-4" onClick={(e) => e.stopPropagation()}>
        <h2 className="text-sm font-bold text-text">assign captain</h2>

        {error && (
          <div className="rounded border border-red/30 bg-red/5 px-3 py-2 text-xs text-red">{error}</div>
        )}

        {mode === "create" ? (
          <div className="space-y-3">
            <p className="text-xs text-text-muted">create a dedicated captain agent for this project</p>
            <div>
              <label className="block text-xs font-medium text-text-secondary mb-1">name</label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="w-full rounded border border-border bg-surface px-3 py-2 text-xs text-text focus:border-accent focus:outline-none"
                placeholder={defaultName}
                autoFocus
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-text-secondary mb-1">framework</label>
              <select
                value={framework}
                onChange={(e) => setFramework(e.target.value)}
                className="w-full rounded border border-border bg-surface px-3 py-2 text-xs text-text focus:border-accent focus:outline-none"
              >
                <option value="openclaw">OpenClaw</option>
                <option value="zeroclaw">ZeroClaw</option>
                <option value="hermes">Hermes</option>
              </select>
            </div>
            <p className="text-[10px] text-text-muted">
              the captain will use the built-in project manager identity
            </p>
            <div className="flex items-center justify-between">
              <button
                onClick={() => setMode("existing")}
                className="text-xs text-green hover:text-green/80 transition-colors"
              >
                use existing captain
              </button>
              <div className="flex gap-2">
                <button onClick={onClose} className="rounded border border-border px-3 py-1.5 text-xs text-text-secondary hover:bg-surface-hover">
                  cancel
                </button>
                <button
                  onClick={handleCreate}
                  disabled={saving}
                  className="rounded bg-accent px-3 py-1.5 text-xs font-medium text-white hover:bg-accent/80 disabled:opacity-50"
                >
                  {saving ? "creating..." : "create captain"}
                </button>
              </div>
            </div>
          </div>
        ) : (
          <div className="space-y-3">
            <p className="text-xs text-text-muted">select an existing captain instance</p>
            <div className="space-y-1.5 max-h-64 overflow-y-auto">
              {captainInstances.length === 0 ? (
                <div className="rounded border border-border bg-surface p-4 text-center text-xs text-text-muted">
                  no captain instances available
                </div>
              ) : (
                captainInstances.map((inst) => {
                  const isStopped = inst.status !== "running";
                  return (
                    <div
                      key={inst.id}
                      className="flex items-center gap-3 rounded border border-border bg-surface px-4 py-3 text-xs transition-all hover:border-green/50 hover:bg-surface-hover/50"
                    >
                      <button
                        onClick={() => handleSelectExisting(inst.id)}
                        disabled={saving}
                        className="flex flex-1 items-center gap-3 text-left disabled:opacity-50"
                      >
                        <span className={`h-1.5 w-1.5 rounded-full ${isStopped ? "bg-text-muted" : "bg-green"}`} />
                        <div className="flex-1">
                          <span className="font-medium text-text">{inst.display_name}</span>
                          <span className="ml-2 text-text-muted">{inst.framework} · :{inst.port}</span>
                          {inst.project_id && (
                            <span className="ml-2 text-[10px] text-text-muted">(assigned)</span>
                          )}
                        </div>
                      </button>
                      {isStopped && (
                        <button
                          disabled={startingCaptain === inst.id}
                          onClick={async () => {
                            setStartingCaptain(inst.id);
                            try {
                              await instanceAction(inst.id, "start");
                              setTimeout(refreshInstances, 2000);
                            } catch (e) {
                              setError(e instanceof Error ? e.message : "failed to start captain");
                            } finally {
                              setStartingCaptain("");
                            }
                          }}
                          className="shrink-0 rounded bg-green px-2 py-0.5 text-[10px] font-medium text-white hover:bg-green/80 disabled:opacity-50"
                        >
                          {startingCaptain === inst.id ? "starting..." : "start"}
                        </button>
                      )}
                    </div>
                  );
                })
              )}
            </div>
            <div className="flex items-center justify-between">
              <button
                onClick={() => setMode("create")}
                className="text-xs text-green hover:text-green/80 transition-colors"
              >
                &larr; create new instead
              </button>
              <button onClick={onClose} className="rounded border border-border px-3 py-1.5 text-xs text-text-secondary hover:bg-surface-hover">
                cancel
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
