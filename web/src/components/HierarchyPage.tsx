import { useState, useEffect, useCallback, useRef } from "react";
import { useNavigate } from "react-router-dom";
import { Plus, RefreshCw, Crown, Briefcase, ChevronRight, MessageSquare, Bot, AlertTriangle } from "lucide-react";
import type { HierarchyTree, Persona } from "../lib/types";
import { fetchHierarchy, fetchPersonas, fetchFrameworks, createInstance, setCommander } from "../lib/api";
import { useData } from "../lib/DataContext";
import type { Framework } from "../lib/types";

interface DashboardMetrics {
  active_projects: number;
  paused_projects: number;
  running_agents: number;
  busy_agents: number;
  stopped_agents: number;
  total_instances: number;
}

// --- Coordinator Setup ---

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

  // "new" form state
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

  useEffect(() => {
    loadData();
  }, [loadData]);

  // Sync default framework selection to an actually-installed framework
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
      // Navigate to chat with briefing flag — the chat page will send the briefing
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
      <div>
        <h2 className="text-lg font-bold text-text">
          <span className="text-accent">&gt;</span> setup commander
        </h2>
        <p className="mt-1 text-xs text-text-muted">
          the commander is your master agent — it manages all your projects and helps you grow your agent team
        </p>
      </div>

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
          <button
            onClick={() => setMode("choose")}
            className="text-xs text-text-muted hover:text-text"
          >
            &larr; back
          </button>
          <div className="text-xs font-medium text-text-secondary">select an agent to be your commander</div>
          <div className="space-y-1.5">
            {runningAgents.length === 0 && (
              loadError ? (
                <div className="flex items-center justify-between rounded border border-red/30 bg-red/5 px-4 py-3">
                  <span className="text-xs text-red">failed to discover agents</span>
                  <button
                    onClick={loadData}
                    className="rounded bg-red/10 px-2 py-1 text-[10px] font-medium text-red hover:bg-red/20 transition-colors"
                  >
                    retry
                  </button>
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
                    <span className="font-medium text-text">{agent.name}</span>
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
          <button
            onClick={() => setMode("choose")}
            className="text-xs text-text-muted hover:text-text"
          >
            &larr; back
          </button>

          <div className="space-y-4 rounded border border-border bg-surface p-6">
            <div>
              <label className="block text-xs font-medium text-text-secondary mb-1.5">name</label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="w-full rounded border border-border bg-bg px-3 py-2 text-xs text-text focus:border-accent focus:outline-none"
                placeholder="atlas"
              />
            </div>

            <div>
              <label className="block text-xs font-medium text-text-secondary mb-1.5">framework</label>
              <select
                value={framework}
                onChange={(e) => setFramework(e.target.value)}
                className="w-full rounded border border-border bg-bg px-3 py-2 text-xs text-text focus:border-accent focus:outline-none"
              >
                {installedFrameworks.length > 0
                  ? installedFrameworks.map((f) => (
                      <option key={f.id} value={f.id}>{f.name}</option>
                    ))
                  : (
                    <option value="" disabled>no frameworks installed</option>
                  )}
              </select>
            </div>

            <div>
              <label className="block text-xs font-medium text-text-secondary mb-1.5">persona (optional)</label>
              <select
                value={personaId}
                onChange={(e) => setPersonaId(e.target.value)}
                className="w-full rounded border border-border bg-bg px-3 py-2 text-xs text-text focus:border-accent focus:outline-none"
              >
                <option value="">default commander</option>
                {personas.map((p) => (
                  <option key={p.id} value={p.id}>{p.icon} {p.name}</option>
                ))}
              </select>
            </div>

            <button
              onClick={handleCreateNew}
              disabled={saving || !name || installedFrameworks.length === 0}
              className="rounded bg-accent px-4 py-2 text-xs font-medium text-white transition-colors hover:bg-accent/80 disabled:opacity-50"
            >
              {saving ? "creating..." : "create commander"}
            </button>
          </div>
        </div>
      )}

    </div>
  );
}

// --- Main Page ---

export default function HierarchyPage() {
  const navigate = useNavigate();
  const [hierarchy, setHierarchy] = useState<HierarchyTree | null>(null);
  const hierarchyRef = useRef<HierarchyTree | null>(null);
  const [loading, setLoading] = useState(true);
  const [fetchError, setFetchError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      setFetchError(null);
      setLoading(true);
      const data = await fetchHierarchy();
      setHierarchy(data);
      hierarchyRef.current = data;
    } catch (e) {
      const msg = e instanceof Error ? e.message : "Failed to fetch hierarchy";
      if (hierarchyRef.current === null) {
        setFetchError(msg);
      }
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
    return (
      <div className="py-20 text-center text-xs text-text-muted">
        loading hierarchy...
      </div>
    );
  }

  if (fetchError) {
    return (
      <div className="py-20 text-center space-y-3">
        <div className="rounded border border-red/30 bg-red/5 px-4 py-3 text-xs text-red inline-block">
          {fetchError}
        </div>
        <div>
          <button
            onClick={() => refresh()}
            disabled={loading}
            className="text-xs text-text-muted hover:text-text transition-colors disabled:opacity-50"
          >
            <RefreshCw className={`inline h-3 w-3 mr-1 ${loading ? "animate-spin" : ""}`} />
            retry
          </button>
        </div>
      </div>
    );
  }

  // No commander set up yet — show setup wizard
  if (!hierarchy?.commander) {
    return <CommanderSetup onCreated={refresh} />;
  }

  // Fetch metrics for dashboard cards
  const [metrics, setMetrics] = useState<DashboardMetrics | null>(null);
  useEffect(() => {
    fetch("/api/metrics").then((r) => r.json()).then(setMetrics).catch(() => {});
  }, [hierarchy]);

  // Compute derived stats
  const allCaptains = hierarchy.projects.filter((t) => t.captain).length;
  const allTalons = hierarchy.projects.reduce((n, t) => n + t.talons.length, 0);

  return (
    <div className="space-y-6">
      <div className="text-xs text-text-muted">~/mission-control</div>

      {/* Commander bar */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className="flex h-9 w-9 items-center justify-center rounded-full bg-purple-500/20">
            <Crown className="h-4 w-4 text-purple-400" />
          </div>
          <div>
            <h1 className="text-xl font-bold">
              <span className="text-accent">&gt;</span> mission control
            </h1>
            <p className="text-xs text-text-muted">
              commander: {hierarchy.commander.display_name || hierarchy.commander.name}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => refresh()}
            disabled={loading}
            className="flex items-center gap-1.5 rounded border border-border px-3 py-1.5 text-xs text-text-muted transition-colors hover:text-text disabled:opacity-50"
          >
            <RefreshCw className={`h-3 w-3 ${loading ? "animate-spin" : ""}`} />
            refresh
          </button>
          <button
            onClick={() => navigate(`/agents/${hierarchy.commander!.name}/chat`)}
            className="flex items-center gap-1.5 rounded border border-purple-400/30 px-3 py-1.5 text-xs text-purple-400 transition-colors hover:bg-purple-400/10"
          >
            <MessageSquare className="h-3 w-3" />
            ask commander
          </button>
        </div>
      </div>

      {/* Metric cards */}
      {metrics && (
        <div className="grid grid-cols-4 gap-3">
          <div className="rounded border border-border p-3.5 space-y-1">
            <div className="text-[10px] font-medium text-text-muted">// active projects</div>
            <div className="text-2xl font-bold text-text">{metrics.active_projects}</div>
            {metrics.paused_projects > 0 && (
              <div className="text-[10px] text-text-muted">{metrics.paused_projects} paused</div>
            )}
          </div>
          <div className="rounded border border-border p-3.5 space-y-1">
            <div className="text-[10px] font-medium text-text-muted">// running agents</div>
            <div className="text-2xl font-bold text-green">{metrics.running_agents}</div>
            <div className="text-[10px] text-text-muted">
              {allCaptains} captain{allCaptains !== 1 ? "s" : ""} · {allTalons} talon{allTalons !== 1 ? "s" : ""}
            </div>
          </div>
          <div className="rounded border border-border p-3.5 space-y-1">
            <div className="text-[10px] font-medium text-text-muted">// instances</div>
            <div className="text-2xl font-bold text-text">{metrics.total_instances}</div>
            {metrics.busy_agents > 0 && (
              <div className="text-[10px] text-yellow-400">{metrics.busy_agents} busy</div>
            )}
          </div>
          <div className="rounded border border-border p-3.5 space-y-1">
            <div className="text-[10px] font-medium text-text-muted">// stopped</div>
            <div className={`text-2xl font-bold ${metrics.stopped_agents > 0 ? "text-yellow-400" : "text-text"}`}>
              {metrics.stopped_agents}
            </div>
            {metrics.stopped_agents > 0 && (
              <div className="flex items-center gap-1 text-[10px] text-yellow-400">
                <AlertTriangle className="h-2.5 w-2.5" /> needs attention
              </div>
            )}
          </div>
        </div>
      )}

      {/* Projects grid */}
      <div>
        <div className="mb-3 flex items-center justify-between">
          <span className="text-[10px] font-medium uppercase tracking-wider text-text-muted">
            // projects ({hierarchy.projects.length})
          </span>
          <button
            onClick={() => navigate("/projects")}
            className="flex items-center gap-1 text-xs text-accent transition-colors hover:text-accent/80"
          >
            <Plus className="h-3 w-3" /> new project
          </button>
        </div>

        {hierarchy.projects.length === 0 ? (
          <div className="rounded border border-border bg-surface p-6 text-center text-xs text-text-muted space-y-3">
            <p>no projects yet</p>
            <div className="flex items-center justify-center gap-2">
              <button
                onClick={() => navigate("/projects")}
                className="rounded bg-accent px-4 py-2 text-xs font-medium text-white transition-colors hover:bg-accent/80"
              >
                create project
              </button>
              <button
                onClick={() => navigate(`/agents/${hierarchy.commander!.name}/chat`)}
                className="rounded border border-border px-4 py-2 text-xs font-medium text-text-muted transition-colors hover:text-text"
              >
                ask commander
              </button>
            </div>
          </div>
        ) : (
          <div className="grid grid-cols-1 gap-3 lg:grid-cols-2 xl:grid-cols-3">
            {hierarchy.projects.map((tree) => (
              <button
                key={tree.project.id}
                onClick={() => navigate(`/projects/${tree.project.id}`)}
                className="rounded border border-border p-4 text-left text-xs transition-all hover:border-accent/30 hover:bg-surface-hover/30 space-y-3"
              >
                {/* Project header */}
                <div className="flex items-center gap-2">
                  <Briefcase className="h-3.5 w-3.5 text-green" />
                  <span className="font-semibold text-text">{tree.project.name}</span>
                  <span className={`rounded px-1.5 py-0.5 text-[9px] font-medium ${
                    tree.project.status === "active"
                      ? "bg-green/10 text-green"
                      : tree.project.status === "paused"
                        ? "bg-yellow-400/10 text-yellow-400"
                        : "bg-text-muted/10 text-text-muted"
                  }`}>
                    {tree.project.status}
                  </span>
                </div>

                {/* Description */}
                {tree.project.description && (
                  <p className="text-text-muted line-clamp-2">{tree.project.description}</p>
                )}

                {/* Team summary */}
                <div className="flex items-center gap-3 text-text-muted">
                  {tree.captain && (
                    <span className="flex items-center gap-1">
                      <span className={`h-1.5 w-1.5 rounded-full ${tree.captain.status === "running" ? "bg-green" : "bg-text-muted"}`} />
                      {tree.captain.display_name || tree.captain.name}
                    </span>
                  )}
                  {tree.talons.length > 0 && (
                    <span className="flex items-center gap-1">
                      <Bot className="h-3 w-3" />
                      {tree.talons.length} talon{tree.talons.length !== 1 ? "s" : ""}
                    </span>
                  )}
                </div>

                {/* Goal */}
                {tree.project.goal && (
                  <div className="flex items-center gap-1 text-[10px] text-green">
                    <span>goal: {tree.project.goal}</span>
                  </div>
                )}
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
