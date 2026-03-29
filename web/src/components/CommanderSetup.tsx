import { useState, useEffect, useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { Plus, Crown, ChevronRight } from "lucide-react";
import type { Persona } from "../lib/types";
import { fetchPersonas, fetchFrameworks, createInstance, setCommander } from "../lib/api";
import { useData } from "../lib/DataContext";
import type { Framework } from "../lib/types";

interface CommanderSetupProps {
  onCreated: () => void;
  /** Show the "setup commander" heading. Defaults to true. */
  showHeader?: boolean;
}

export function CommanderSetup({ onCreated, showHeader = true }: CommanderSetupProps) {
  const navigate = useNavigate();
  const { agents, loading: loadingAgents } = useData();
  const [mode, setMode] = useState<"choose" | "existing" | "new">("choose");
  const [frameworks, setFrameworks] = useState<Framework[]>([]);
  const [personas, setPersonas] = useState<Persona[]>([]);
  const [saving, setSaving] = useState(false);
  const [savingStatus, setSavingStatus] = useState("");
  const [error, setError] = useState("");
  const [loadError, setLoadError] = useState<string | null>(null);

  const [name, setName] = useState("atlas");
  const [framework, setFramework] = useState("openclaw");
  const [personaId, setPersonaId] = useState("");

  const loadData = useCallback(async () => {
    setLoadError(null);
    const [frameworksResult, personasResult] = await Promise.allSettled([
      fetchFrameworks(),
      fetchPersonas(),
    ]);

    if (frameworksResult.status === "fulfilled") setFrameworks(frameworksResult.value);
    if (personasResult.status === "fulfilled") setPersonas(personasResult.value);

    const errors: string[] = [];
    if (frameworksResult.status === "rejected") {
      console.error("Failed to fetch frameworks:", frameworksResult.reason);
      errors.push(frameworksResult.reason instanceof Error ? frameworksResult.reason.message : "Failed to fetch frameworks");
    }
    if (personasResult.status === "rejected") {
      console.error("Failed to fetch personas:", personasResult.reason);
      errors.push(personasResult.reason instanceof Error ? personasResult.reason.message : "Failed to fetch personas");
    }

    setLoadError(errors.length > 0 ? errors.join("; ") : null);
  }, []);

  useEffect(() => { loadData(); }, [loadData]);

  useEffect(() => {
    const installedIds = frameworks.filter((f) => f.installed).map((f) => f.id);
    if (installedIds.length > 0 && !installedIds.includes(framework)) {
      setFramework(installedIds[0]);
    }
  }, [frameworks, framework]);

  const handleSelectExisting = async (agentName: string) => {
    setSaving(true);
    setSavingStatus("setting commander...");
    setError("");
    try {
      await setCommander({ agentName });
      onCreated();
      navigate(`/agents/${agentName}/chat?brief=commander`);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to set commander");
      setSaving(false);
    }
  };

  const handleCreateNew = async () => {
    setSaving(true);
    setSavingStatus("creating agent instance...");
    setError("");
    try {
      const inst = await createInstance({
        name,
        framework,
        persona_id: personaId || undefined,
        hierarchy_role: "commander",
        auto_start: true,
      });
      await setCommander({ instanceId: inst.id });
      onCreated();
      navigate(`/agents/${inst.name}/chat?brief=commander`);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to create commander");
      setSaving(false);
    }
  };

  const runningAgents = agents.filter((a) => a.alive);
  const installedFrameworks = frameworks.filter((f) => f.installed);

  return (
    <div className="space-y-6">
      {showHeader && (
        <div>
          <h2 className="text-lg font-bold text-text">
            <span className="text-accent">&gt;</span> setup commander
          </h2>
          <p className="mt-1 text-xs text-text-muted">
            the commander is your master agent — it manages all your projects and helps you grow your agent team
          </p>
        </div>
      )}

      {error && (
        <div className="rounded border border-red/30 bg-red/5 px-3 py-2 text-xs text-red">
          {error}
        </div>
      )}

      {loadError && (
        <div className="rounded border border-red/30 bg-red/5 px-3 py-2 text-xs text-red flex items-center justify-between">
          <span>{loadError}</span>
          <button
            onClick={loadData}
            className="ml-3 shrink-0 rounded bg-red/10 px-2 py-1 text-[10px] font-medium text-red hover:bg-red/20 transition-colors"
          >
            retry
          </button>
        </div>
      )}

      {mode === "choose" && (
        <div className="space-y-3">
          <div
            onClick={() => runningAgents.length > 0 && setMode("existing")}
            role={runningAgents.length > 0 ? "button" : undefined}
            tabIndex={runningAgents.length > 0 ? 0 : undefined}
            onKeyDown={runningAgents.length > 0 ? (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); setMode("existing"); } } : undefined}
            className={`flex w-full items-center gap-4 rounded border border-border bg-surface p-5 text-left transition-all ${
              runningAgents.length > 0 ? "hover:border-accent/50 hover:bg-surface-hover/50 cursor-pointer" : "opacity-50"
            }`}
          >
            <div className="flex h-10 w-10 items-center justify-center rounded-full bg-green/10">
              <Crown className="h-5 w-5 text-green" />
            </div>
            <div>
              <div className="text-sm font-medium text-text">use an existing agent</div>
              <div className="mt-0.5 text-xs text-text-muted">
                {runningAgents.length > 0
                  ? `promote one of your ${runningAgents.length} running agent${runningAgents.length !== 1 ? "s" : ""} to commander`
                  : loadError ? "failed to discover agents" : loadingAgents ? "discovering running agents..." : "no running agents found"}
              </div>
            </div>
            {runningAgents.length > 0 ? (
              <ChevronRight className="ml-auto h-4 w-4 text-text-muted" />
            ) : loadError ? (
              <button
                onClick={(e) => { e.stopPropagation(); loadData(); }}
                className="ml-auto text-[10px] text-red hover:text-red/80 cursor-pointer"
              >
                retry
              </button>
            ) : (
              <span className="ml-auto h-3 w-3 animate-spin rounded-full border-2 border-text-muted/30 border-t-text-muted" />
            )}
          </div>

          <button
            onClick={() => setMode("new")}
            className="flex w-full items-center gap-4 rounded border border-border bg-surface p-5 text-left transition-all hover:border-accent/50 hover:bg-surface-hover/50"
          >
            <div className="flex h-10 w-10 items-center justify-center rounded-full bg-accent/10">
              <Plus className="h-5 w-5 text-accent" />
            </div>
            <div>
              <div className="text-sm font-medium text-text">create a new agent</div>
              <div className="mt-0.5 text-xs text-text-muted">
                provision a dedicated commander with its own identity and workspace
              </div>
            </div>
            <ChevronRight className="ml-auto h-4 w-4 text-text-muted" />
          </button>
        </div>
      )}

      {mode === "existing" && (
        <div className="space-y-3">
          <button onClick={() => setMode("choose")} className="text-xs text-text-muted hover:text-text">&larr; back</button>
          <div className="text-xs font-medium text-text-secondary">select an agent to be your commander</div>
          <div className="space-y-1.5">
            {runningAgents.length === 0 && (
              loadError ? (
                <div className="flex items-center justify-between rounded border border-red/30 bg-red/5 px-4 py-3">
                  <span className="text-xs text-red">failed to discover agents</span>
                  <button onClick={loadData} className="rounded bg-red/10 px-2 py-1 text-[10px] font-medium text-red hover:bg-red/20 transition-colors">retry</button>
                </div>
              ) : loadingAgents ? (
                <div className="flex items-center gap-3 rounded border border-border bg-surface px-4 py-3 opacity-50">
                  <span className="h-1.5 w-1.5 rounded-full bg-text-muted animate-pulse" />
                  <span className="text-xs text-text-muted">discovering agents...</span>
                </div>
              ) : (
                <div className="rounded border border-border bg-surface px-4 py-3 text-center text-xs text-text-muted">
                  no running agents found
                </div>
              )
            )}
            {runningAgents.map((agent) => {
              const canBeCommander = agent.commander_capable;
              return (
                <button
                  key={agent.name}
                  onClick={() => canBeCommander && handleSelectExisting(agent.name)}
                  disabled={saving || !canBeCommander}
                  className={`flex w-full items-center gap-3 rounded border border-border bg-surface px-4 py-3 text-left text-xs transition-all disabled:opacity-50 ${
                    canBeCommander ? "hover:border-green/50 hover:bg-surface-hover/50" : "cursor-not-allowed"
                  }`}
                >
                  <span className="h-1.5 w-1.5 rounded-full bg-green" />
                  <div className="flex-1">
                    <span className="font-medium text-text">{agent.display_name || agent.name}</span>
                    <span className="ml-2 text-text-muted">{agent.framework} · :{agent.port}</span>
                  </div>
                  {canBeCommander ? (
                    <span className="rounded bg-green/10 px-1.5 py-0.5 text-[10px] font-medium text-green">running</span>
                  ) : (
                    <span className="rounded bg-text-muted/10 px-1.5 py-0.5 text-[10px] text-text-muted">talon only</span>
                  )}
                </button>
              );
            })}
          </div>
          {saving && savingStatus && (
            <div className="flex items-center gap-2 rounded border border-purple/20 bg-purple/5 px-3 py-2 text-xs text-purple">
              <span className="inline-block h-3 w-3 animate-spin rounded-full border-2 border-purple/30 border-t-purple" />
              {savingStatus}
            </div>
          )}
        </div>
      )}

      {mode === "new" && (
        <div className="space-y-4">
          <button onClick={() => setMode("choose")} className="text-xs text-text-muted hover:text-text">&larr; back</button>

          <div className="space-y-4 rounded border border-border bg-surface p-6">
            <div>
              <label className="block text-xs font-medium text-text-secondary mb-1.5">name</label>
              <input type="text" value={name} onChange={(e) => setName(e.target.value)}
                className="w-full rounded border border-border bg-bg px-3 py-2 text-xs text-text focus:border-accent focus:outline-none" placeholder="atlas" />
            </div>
            <div>
              <label className="block text-xs font-medium text-text-secondary mb-1.5">framework</label>
              <select value={framework} onChange={(e) => setFramework(e.target.value)}
                className="w-full rounded border border-border bg-bg px-3 py-2 text-xs text-text focus:border-accent focus:outline-none">
                {installedFrameworks.length > 0
                  ? installedFrameworks.map((f) => <option key={f.id} value={f.id}>{f.name}</option>)
                  : <option value="" disabled>no frameworks installed</option>}
              </select>
            </div>
            <div>
              <label className="block text-xs font-medium text-text-secondary mb-1.5">persona (optional)</label>
              <select value={personaId} onChange={(e) => setPersonaId(e.target.value)}
                className="w-full rounded border border-border bg-bg px-3 py-2 text-xs text-text focus:border-accent focus:outline-none">
                <option value="">default commander</option>
                {personas.map((p) => <option key={p.id} value={p.id}>{p.icon} {p.name}</option>)}
              </select>
            </div>
            <button onClick={handleCreateNew} disabled={saving || !name || installedFrameworks.length === 0}
              className="rounded bg-accent px-4 py-2 text-xs font-medium text-white transition-colors hover:bg-accent/80 disabled:opacity-50">
              {saving ? "creating..." : "create commander"}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
