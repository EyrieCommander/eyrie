import { useState, useEffect, useCallback, useRef } from "react";
import { useNavigate } from "react-router-dom";
import { Plus, RefreshCw, Crown, ChevronRight, MessageSquare, ChevronLeft } from "lucide-react";
import type { HierarchyTree, Persona, ProjectTree } from "../lib/types";
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

// ─── Helper components ───

function MetricCard({ label, value, valueColor, sub }: {
  label: string; value: number; valueColor?: string; sub?: string;
}) {
  return (
    <div className="rounded border border-border p-3 space-y-1">
      <div className="text-[9px] font-medium text-text-muted">// {label}</div>
      <div className={`text-xl font-bold ${valueColor || "text-text"}`}>{value}</div>
      {sub && <div className="text-[10px] text-text-muted">{sub}</div>}
    </div>
  );
}

// ─── Swim Lane Timeline ───
// TODO: Connect to real event data from GET /api/projects/{id}/activity
// Currently renders with placeholder blocks. The data layer needs an
// aggregated cross-project events endpoint to populate real content.

interface SwimLaneProject {
  name: string;
  status: string;
  goal?: string;
  agents: { name: string; role: string; status: string }[];
}

// Example projects shown when no real projects exist, so the timeline
// structure is visible during development.
const EXAMPLE_PROJECTS: SwimLaneProject[] = [
  {
    name: "finance tracker",
    status: "active",
    goal: "mvp ready for demo by end of week",
    agents: [
      { name: "captain-finance", role: "captain", status: "running" },
      { name: "talon-researcher", role: "talon", status: "running" },
      { name: "talon-developer", role: "talon", status: "running" },
    ],
  },
  {
    name: "chess coach",
    status: "active",
    goal: "teach positional chess concepts",
    agents: [
      { name: "captain-chess", role: "captain", status: "running" },
      { name: "talon-curriculum", role: "talon", status: "running" },
    ],
  },
  {
    name: "blog migration",
    status: "paused",
    goal: "migrate wordpress to astro",
    agents: [
      { name: "captain-blog", role: "captain", status: "stopped" },
      { name: "talon-writer", role: "talon", status: "stopped" },
    ],
  },
];

function buildSwimLaneProjects(trees: ProjectTree[]): SwimLaneProject[] {
  // Real projects first, then example projects for visual structure.
  const real: SwimLaneProject[] = trees.map((t) => ({
    name: t.project.name,
    status: t.project.status,
    goal: t.project.goal,
    agents: [
      ...(t.captain
        ? [{ name: t.captain.display_name || t.captain.name, role: "captain", status: t.captain.status }]
        : []),
      ...t.talons.map((tl) => ({
        name: tl.display_name || tl.name,
        role: "talon",
        status: tl.status,
      })),
    ],
  }));
  // Filter out examples whose names collide with real projects
  const realNames = new Set(real.map((p) => p.name));
  const examples = EXAMPLE_PROJECTS.filter((e) => !realNames.has(e.name));
  return [...real, ...examples];
}

function SwimLaneTimeline({ projects, onProjectClick }: {
  projects: ProjectTree[];
  onProjectClick?: (id: string) => void;
}) {
  const swimProjects = buildSwimLaneProjects(projects);

  // Date columns: today and 2 days prior
  const today = new Date();
  const days = Array.from({ length: 3 }, (_, i) => {
    const d = new Date(today);
    d.setDate(d.getDate() - (2 - i));
    return d;
  });

  const formatDay = (d: Date) => {
    const dayNames = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"];
    const monthNames = ["jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"];
    const isToday = d.toDateString() === today.toDateString();
    return isToday
      ? `${monthNames[d.getMonth()]} ${d.getDate()} (today)`
      : `${monthNames[d.getMonth()]} ${d.getDate()} (${dayNames[d.getDay()]})`;
  };

  const ROW_H = "h-10";

  return (
    <div className="flex flex-col h-full">
      <div className="flex-1 overflow-auto">
        {/* Shared date headers */}
        <div className="flex sticky top-0 z-10 bg-bg">
          {/* Corner: project column header */}
          <div className="flex-shrink-0 w-[220px] border-r border-b border-border px-3 py-2">
            <span className="text-[9px] font-medium text-text-muted">// projects</span>
          </div>
          {/* Date columns */}
          <div className="flex flex-1">
            {days.map((day, di) => {
              const isToday = day.toDateString() === today.toDateString();
              return (
                <div
                  key={di}
                  className={`flex-1 flex items-center justify-center py-2 border-b ${
                    isToday ? "border-accent" : "border-border"
                  } ${di < days.length - 1 ? "border-r border-r-border" : ""}`}
                >
                  <span className={`text-[10px] font-medium ${isToday ? "text-accent" : "text-text-muted"}`}>
                    {formatDay(day)}
                  </span>
                </div>
              );
            })}
          </div>
        </div>

        {/* Per-project sections */}
        {swimProjects.map((proj) => {
          const realTree = projects.find((t) => t.project.name === proj.name);
          const isExample = !realTree;
          return (
            <div key={proj.name} className={`flex border-b border-border ${isExample ? "opacity-50" : ""}`}>
              {/* Project card */}
              <div
                onClick={() => realTree && onProjectClick?.(realTree.project.id)}
                role={realTree ? "button" : undefined}
                className={`flex-shrink-0 w-[220px] border-r border-border p-3 space-y-2 ${
                  realTree ? "cursor-pointer hover:bg-surface-hover/30 transition-colors" : ""
                }`}
              >
                <div className="flex items-center gap-2">
                  <span className={`h-1.5 w-1.5 flex-shrink-0 rounded-full ${
                    proj.status === "active" ? "bg-green"
                      : proj.status === "paused" ? "bg-purple-400"
                      : "bg-text-muted"
                  }`} />
                  <span className="text-[11px] font-semibold text-text truncate">{proj.name}</span>
                  <span className={`rounded px-1 py-0.5 text-[8px] font-medium ${
                    proj.status === "active"
                      ? "bg-green/10 text-green"
                      : proj.status === "paused"
                        ? "bg-purple-400/10 text-purple-400"
                        : "bg-text-muted/10 text-text-muted"
                  }`}>
                    {proj.status}
                  </span>
                </div>
                {proj.goal && (
                  <div className="text-[9px] text-green truncate">{proj.goal}</div>
                )}
                <div className="space-y-1 pt-1">
                  {proj.agents.map((agent) => (
                    <div key={agent.name} className="flex items-center gap-1.5">
                      <span className={`h-1 w-1 flex-shrink-0 rounded-full ${
                        agent.status === "running" ? "bg-green"
                          : agent.status === "starting" ? "bg-green animate-pulse"
                          : agent.status === "error" ? "bg-red"
                          : "bg-text-muted/50"
                      }`} />
                      <span className={`text-[9px] truncate ${
                        agent.role === "captain" ? "text-text-secondary" : "text-text-muted"
                      }`}>
                        {agent.name}
                      </span>
                    </div>
                  ))}
                </div>
              </div>

              {/* Day columns with agent rows */}
              <div className="flex flex-1">
                {days.map((day, di) => {
                  const isToday = day.toDateString() === today.toDateString();
                  return (
                    <div
                      key={di}
                      className={`flex-1 flex flex-col ${di < days.length - 1 ? "border-r border-border" : ""}`}
                    >
                      {proj.agents.map((agent, ai) => (
                        <div
                          key={agent.name}
                          className={`${ROW_H} flex items-center gap-1 px-2 ${
                            ai < proj.agents.length - 1 ? "border-b border-border/50" : ""
                          }`}
                        >
                          {renderPlaceholderBlocks(agent.name, agent.status, di, isToday)}
                        </div>
                      ))}
                    </div>
                  );
                })}
              </div>
            </div>
          );
        })}
      </div>

      {/* Legend */}
      <div className="flex items-center justify-end gap-5 border-t border-border px-4 py-2">
        <span className="text-[9px] text-text-muted">legend:</span>
        <LegendItem color="bg-green/30" label="research" />
        <LegendItem color="bg-purple-400/30" label="development" />
        <LegendItem color="bg-amber-400/30" label="coordination" />
        <LegendItem color="bg-surface-hover border border-border" label="planned" />
      </div>
    </div>
  );
}

function LegendItem({ color, label }: { color: string; label: string }) {
  return (
    <div className="flex items-center gap-1.5">
      <div className={`h-2.5 w-4 rounded-sm ${color}`} />
      <span className="text-[9px] text-text-muted">{label}</span>
    </div>
  );
}

// Placeholder block renderer — deterministic mock blocks from agent name + day.
// Replace with real event data when the activity endpoint is wired up.
function renderPlaceholderBlocks(agentName: string, agentStatus: string, dayIndex: number, isToday: boolean) {
  const seed = agentName.length + dayIndex * 7;
  const blockCount = seed % 3;

  if (agentStatus !== "running" && agentStatus !== "starting") {
    return null; // stopped agents show no activity
  }

  const colors = [
    "bg-green/30",        // research
    "bg-purple-400/30",   // development
    "bg-amber-400/30",    // coordination
  ];

  const blocks = [];
  for (let i = 0; i < blockCount; i++) {
    const colorIdx = (seed + i) % colors.length;
    const width = 30 + ((seed * (i + 1)) % 60);
    blocks.push(
      <div
        key={i}
        className={`h-5 rounded-sm ${colors[colorIdx]} ${isToday ? "opacity-80" : "opacity-50"}`}
        style={{ width: `${width}px` }}
      />
    );
  }
  return <>{blocks}</>;
}

// ─── Commander Setup ───

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

// ─── Main Page ───

export default function HierarchyPage() {
  const navigate = useNavigate();

  // All hooks must come before any conditional returns
  const [hierarchy, setHierarchy] = useState<HierarchyTree | null>(null);
  const hierarchyRef = useRef<HierarchyTree | null>(null);
  const [loading, setLoading] = useState(true);
  const [fetchError, setFetchError] = useState<string | null>(null);
  const [metrics, setMetrics] = useState<DashboardMetrics | null>(null);

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

  // Fetch metrics — runs whenever hierarchy updates
  useEffect(() => {
    if (!hierarchy) return;
    fetch("/api/metrics").then((r) => r.json()).then(setMetrics).catch(() => {});
  }, [hierarchy]);

  // ─── Conditional returns (after all hooks) ───

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

  // ─── Derived stats ───
  const allCaptains = hierarchy.projects.filter((t) => t.captain).length;
  const allTalons = hierarchy.projects.reduce((n, t) => n + t.talons.length, 0);

  return (
    <div className="flex h-full flex-col">
      {/* Commander bar */}
      <div className="flex items-center justify-between border-b border-border px-5 py-3">
        <div className="flex items-center gap-3">
          <div className="flex h-8 w-8 items-center justify-center rounded-full bg-purple-500/20">
            <Crown className="h-3.5 w-3.5 text-purple-400" />
          </div>
          <div>
            <h1 className="text-sm font-bold text-text">
              <span className="text-accent">&gt;</span> mission control
            </h1>
            <p className="text-[10px] text-text-muted">
              commander: {hierarchy.commander.display_name || hierarchy.commander.name}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => navigate("/projects")}
            className="flex items-center gap-1.5 rounded border border-border px-3 py-1.5 text-xs text-text-muted transition-colors hover:text-text"
          >
            <Plus className="h-3 w-3" />
            new project
          </button>
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

      {/* Metrics row — horizontal across top */}
      <div className="flex items-stretch gap-3 border-b border-border px-5 py-3">
        <MetricCard
          label="active projects"
          value={metrics?.active_projects ?? hierarchy.projects.filter((t) => t.project.status === "active").length}
          sub={metrics?.paused_projects && metrics.paused_projects > 0 ? `${metrics.paused_projects} paused` : undefined}
        />
        <MetricCard
          label="running agents"
          value={metrics?.running_agents ?? 0}
          valueColor="text-green"
          sub={`${allCaptains} captain${allCaptains !== 1 ? "s" : ""} · ${allTalons} talon${allTalons !== 1 ? "s" : ""}`}
        />
        <MetricCard
          label="total instances"
          value={metrics?.total_instances ?? 0}
          sub={metrics?.busy_agents && metrics.busy_agents > 0 ? `${metrics.busy_agents} busy` : undefined}
        />
        <MetricCard
          label="stopped"
          value={metrics?.stopped_agents ?? 0}
          valueColor={metrics?.stopped_agents && metrics.stopped_agents > 0 ? "text-red" : undefined}
        />
      </div>

      {/* Timeline header */}
      <div className="flex items-center justify-between border-b border-border px-5 py-2">
        <span className="text-[10px] font-medium uppercase tracking-wider text-text-muted">
          // activity timeline
        </span>
        <div className="flex items-center gap-3">
          <button className="text-text-muted hover:text-text transition-colors">
            <ChevronLeft className="h-3.5 w-3.5" />
          </button>
          <span className="text-xs font-medium text-text">
            {new Date().toLocaleDateString("en-US", { month: "short", day: "numeric" })}
          </span>
          <button className="text-accent hover:text-accent/80 transition-colors">
            <ChevronRight className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>

      {/* Swim lane timeline — takes remaining space */}
      <div className="flex-1 overflow-hidden">
        <SwimLaneTimeline
          projects={hierarchy.projects}
          onProjectClick={(id) => navigate(`/projects/${id}`)}
        />
      </div>
    </div>
  );
}
