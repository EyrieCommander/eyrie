import { useState, useEffect, useCallback, useRef } from "react";
import { useNavigate } from "react-router-dom";
import { ArrowLeft, Crown, User, Bot, RefreshCw } from "lucide-react";
import type { HierarchyTree, Persona } from "../lib/types";
import { fetchHierarchy, fetchPersonas, fetchFrameworks, createInstance, setCommander } from "../lib/api";
import { useData } from "../lib/DataContext";
import type { Framework } from "../lib/types";

// ─── Commander Setup (reused from HierarchyPage) ───

function CommanderSetup({ onCreated }: { onCreated: () => void }) {
  const navigate = useNavigate();
  const { agents, loading: ctxLoading } = useData();
  const [mode, setMode] = useState<"choose" | "existing" | "new">("choose");
  const [frameworks, setFrameworks] = useState<Framework[]>([]);
  const [personas, setPersonas] = useState<Persona[]>([]);
  const [saving, setSaving] = useState(false);
  const [savingStatus, setSavingStatus] = useState("");
  const [error, setError] = useState("");
  const [loadError, setLoadError] = useState<string | null>(null);
  const loadingAgents = ctxLoading;

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
    if (frameworksResult.status === "rejected") errors.push(frameworksResult.reason instanceof Error ? frameworksResult.reason.message : "Failed to fetch frameworks");
    if (personasResult.status === "rejected") errors.push(personasResult.reason instanceof Error ? personasResult.reason.message : "Failed to fetch personas");
    setLoadError(errors.length > 0 ? errors.join("; ") : null);
  }, []);

  useEffect(() => { loadData(); }, [loadData]);

  useEffect(() => {
    const installedIds = frameworks.filter((f) => f.installed).map((f) => f.id);
    if (installedIds.length > 0 && !installedIds.includes(framework)) setFramework(installedIds[0]);
  }, [frameworks, framework]);

  const handleSelectExisting = async (agentName: string) => {
    setSaving(true); setSavingStatus("setting commander..."); setError("");
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
    setSaving(true); setSavingStatus("creating agent instance..."); setError("");
    try {
      const inst = await createInstance({ name, framework, persona_id: personaId || undefined, hierarchy_role: "commander", auto_start: true });
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
    <div className="space-y-4">
      {error && <div className="rounded border border-red/30 bg-red/5 px-3 py-2 text-xs text-red">{error}</div>}
      {loadError && (
        <div className="rounded border border-red/30 bg-red/5 px-3 py-2 text-xs text-red flex items-center justify-between">
          <span>{loadError}</span>
          <button onClick={loadData} className="ml-3 shrink-0 rounded bg-red/10 px-2 py-1 text-[10px] font-medium text-red hover:bg-red/20 transition-colors">retry</button>
        </div>
      )}

      {mode === "choose" && (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <button
            onClick={() => runningAgents.length > 0 && setMode("existing")}
            disabled={runningAgents.length === 0}
            className="flex items-center gap-4 rounded border border-border bg-surface p-5 text-left transition-all hover:border-accent/50 hover:bg-surface-hover/50 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <div className="flex h-10 w-10 items-center justify-center rounded-full bg-green/10 shrink-0">
              <Crown className="h-5 w-5 text-green" />
            </div>
            <div>
              <div className="text-sm font-medium text-text">use an existing agent</div>
              <div className="mt-0.5 text-xs text-text-muted">
                {runningAgents.length > 0
                  ? `${runningAgents.length} running agent${runningAgents.length !== 1 ? "s" : ""} available`
                  : loadingAgents ? "discovering..." : "no running agents found"}
              </div>
            </div>
          </button>
          <button
            onClick={() => setMode("new")}
            className="flex items-center gap-4 rounded border border-border bg-surface p-5 text-left transition-all hover:border-accent/50 hover:bg-surface-hover/50"
          >
            <div className="flex h-10 w-10 items-center justify-center rounded-full bg-accent/10 shrink-0">
              <Crown className="h-5 w-5 text-accent" />
            </div>
            <div>
              <div className="text-sm font-medium text-text">create a new agent</div>
              <div className="mt-0.5 text-xs text-text-muted">provision a dedicated commander</div>
            </div>
          </button>
        </div>
      )}

      {mode === "existing" && (
        <div className="space-y-3">
          <button onClick={() => setMode("choose")} className="text-xs text-text-muted hover:text-text">&larr; back</button>
          <div className="space-y-1.5">
            {runningAgents.map((agent) => {
              const canBeCommander = agent.commander_capable;
              return (
                <button key={agent.name} onClick={() => canBeCommander && handleSelectExisting(agent.name)} disabled={saving || !canBeCommander}
                  className={`flex w-full items-center gap-3 rounded border border-border bg-surface px-4 py-3 text-left text-xs transition-all disabled:opacity-50 ${canBeCommander ? "hover:border-green/50 hover:bg-surface-hover/50" : "cursor-not-allowed"}`}
                >
                  <span className="h-1.5 w-1.5 rounded-full bg-green" />
                  <span className="font-medium text-text flex-1">{agent.display_name || agent.name}</span>
                  <span className="text-text-muted">{agent.framework} · :{agent.port}</span>
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

// ─── Agent Card ───

function AgentCard({ displayName, role, roleBadgeColor, roleIcon: RoleIcon, status, framework, project, onClick }: {
  displayName: string;
  role: string;
  roleBadgeColor: string;
  roleIcon: React.ComponentType<{ className?: string }>;
  status: string;
  framework: string;
  project?: string;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      className="flex w-full items-center gap-4 rounded border border-border p-4 text-left text-xs transition-all hover:border-accent/30 hover:bg-surface-hover/30"
    >
      <div className={`flex h-9 w-9 items-center justify-center rounded-full shrink-0 ${
        role === "commander" ? "bg-purple-500/20" : role === "captain" ? "bg-accent/10" : "bg-text-muted/10"
      }`}>
        <RoleIcon className={`h-4 w-4 ${
          role === "commander" ? "text-purple-400" : role === "captain" ? "text-accent" : "text-text-muted"
        }`} />
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className={`h-1.5 w-1.5 rounded-full shrink-0 ${status === "running" ? "bg-green" : "bg-text-muted"}`} />
          <span className="font-medium text-text truncate">{displayName}</span>
          <span className={`rounded px-1.5 py-0.5 text-[9px] font-medium shrink-0 ${roleBadgeColor}`}>{role}</span>
        </div>
        <div className="flex items-center gap-2 mt-1 text-text-muted">
          <span>{framework}</span>
          {project && <><span className="text-border">|</span><span className="truncate">{project}</span></>}
          <span className="text-border">|</span>
          <span>{status}</span>
        </div>
      </div>
    </button>
  );
}

// ─── Main Page ───

export default function AgentsPage() {
  const navigate = useNavigate();
  const [hierarchy, setHierarchy] = useState<HierarchyTree | null>(null);
  const hierarchyRef = useRef<HierarchyTree | null>(null);
  const [loading, setLoading] = useState(true);
  const [changingCommander, setChangingCommander] = useState(false);

  const refresh = useCallback(async () => {
    try {
      setLoading(true);
      const data = await fetchHierarchy();
      setHierarchy(data);
      hierarchyRef.current = data;
    } catch {
      // silent — keep stale data
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
    const interval = setInterval(refresh, 15000);
    return () => clearInterval(interval);
  }, [refresh]);

  if (loading && !hierarchy) {
    return <div className="py-20 text-center text-xs text-text-muted">loading agents...</div>;
  }

  if (!hierarchy?.commander) {
    return (
      <div className="space-y-6">
        <button onClick={() => navigate("/mission-control")} className="flex items-center gap-1.5 text-xs text-text-muted hover:text-text">
          <ArrowLeft className="h-3 w-3" /> mission control
        </button>
        <h2 className="text-lg font-bold text-text"><span className="text-accent">&gt;</span> setup commander</h2>
        <p className="text-xs text-text-muted">the commander is your master agent — it manages all your projects and helps you grow your agent team</p>
        <CommanderSetup onCreated={refresh} />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <button onClick={() => navigate("/mission-control")} className="flex items-center gap-1.5 text-xs text-text-muted hover:text-text">
        <ArrowLeft className="h-3 w-3" /> mission control
      </button>

      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-bold"><span className="text-accent">&gt;</span> agents</h1>
          <p className="mt-1 text-xs text-text-muted">// manage your agent hierarchy</p>
        </div>
        <button onClick={() => refresh()} disabled={loading}
          className="flex items-center gap-1.5 text-xs text-text-muted hover:text-text disabled:opacity-50">
          <RefreshCw className={`h-3 w-3 ${loading ? "animate-spin" : ""}`} /> refresh
        </button>
      </div>

      {/* Commander section */}
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <span className="text-[10px] font-medium uppercase tracking-wider text-text-muted">// commander</span>
          <button onClick={() => setChangingCommander(!changingCommander)}
            className="text-[10px] text-purple-400 hover:text-purple-300 transition-colors">
            {changingCommander ? "cancel" : "change commander"}
          </button>
        </div>

        {changingCommander ? (
          <div className="rounded border border-purple-400/30 bg-purple-400/5 p-4">
            <CommanderSetup onCreated={() => { setChangingCommander(false); refresh(); }} />
          </div>
        ) : (
          <AgentCard

            displayName={hierarchy.commander.display_name || hierarchy.commander.name}
            role="commander"
            roleBadgeColor="bg-purple-400/10 text-purple-400"
            roleIcon={Crown}
            status={hierarchy.commander.status}
            framework={hierarchy.commander.framework}
            onClick={() => navigate(`/agents/${hierarchy.commander!.name}`)}
          />
        )}
      </div>

      {/* Captains + Talons grouped by project */}
      {hierarchy.projects.length > 0 && (
        <div className="space-y-3">
          <span className="text-[10px] font-medium uppercase tracking-wider text-text-muted">
            // project agents ({hierarchy.projects.reduce((n, t) => n + (t.captain ? 1 : 0) + t.talons.length, 0)})
          </span>

          {hierarchy.projects.map((tree) => (
            <div key={tree.project.id} className="space-y-2">
              {/* Project label */}
              <div className="flex items-center gap-2 px-1">
                <span className={`h-1.5 w-1.5 rounded-full ${tree.project.status === "active" ? "bg-green" : "bg-text-muted"}`} />
                <button onClick={() => navigate(`/projects/${tree.project.id}`)}
                  className="text-xs font-medium text-text hover:text-accent transition-colors">
                  {tree.project.name}
                </button>
                {tree.project.goal && <span className="text-[10px] text-text-muted truncate">— {tree.project.goal}</span>}
              </div>

              {tree.captain && (
                <AgentCard

                  displayName={tree.captain.display_name || tree.captain.name}
                  role="captain"
                  roleBadgeColor="bg-accent/10 text-accent"
                  roleIcon={User}
                  status={tree.captain.status}
                  framework={tree.captain.framework}
                  project={tree.project.name}
                  onClick={() => navigate(`/agents/${tree.captain!.name}`)}
                />
              )}

              {tree.talons.map((talon) => (
                <div key={talon.id} className="ml-6">
                  <AgentCard

                    displayName={talon.display_name || talon.name}
                    role="talon"
                    roleBadgeColor="bg-text-muted/10 text-text-muted"
                    roleIcon={Bot}
                    status={talon.status}
                    framework={talon.framework}
                    project={tree.project.name}
                    onClick={() => navigate(`/agents/${talon.name}`)}
                  />
                </div>
              ))}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
