import { useState, useEffect, useCallback, useRef } from "react";
import { useNavigate } from "react-router-dom";
import { BarChart3, Plus, RefreshCw, Crown, ChevronRight, MessageSquare, ChevronLeft } from "lucide-react";
import type { HierarchyTree, ProjectTree } from "../lib/types";
import { fetchHierarchy } from "../lib/api";
import { useData } from "../lib/DataContext";
import { CommanderSetup } from "./CommanderSetup";

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

// Collect timestamped events from project trees for the timeline.
interface TimelineEvent {
  label: string;
  type: "project-created" | "captain-assigned" | "talon-added";
  date: string; // ISO timestamp
}

function collectProjectEvents(tree: ProjectTree): TimelineEvent[] {
  const events: TimelineEvent[] = [];
  if (tree.project.created_at) {
    events.push({ label: "project created", type: "project-created", date: tree.project.created_at });
  }
  if (tree.captain?.created_at) {
    events.push({
      label: `captain: ${tree.captain.display_name || tree.captain.name}`,
      type: "captain-assigned",
      date: tree.captain.created_at,
    });
  }
  for (const talon of tree.talons) {
    if (talon.created_at) {
      events.push({
        label: `talon: ${talon.display_name || talon.name}`,
        type: "talon-added",
        date: talon.created_at,
      });
    }
  }
  return events;
}

// Full Tailwind classes — dynamic suffixing (e.g., `${color}/20`) gets purged.
const EVENT_DOT: Record<TimelineEvent["type"], string> = {
  "project-created": "bg-accent",
  "captain-assigned": "bg-purple-400",
  "talon-added": "bg-amber-400",
};
const EVENT_BG: Record<TimelineEvent["type"], string> = {
  "project-created": "bg-accent/20",
  "captain-assigned": "bg-purple-400/20",
  "talon-added": "bg-amber-400/20",
};

function sameDay(a: Date, b: Date): boolean {
  return a.getUTCFullYear() === b.getUTCFullYear() && a.getUTCMonth() === b.getUTCMonth() && a.getUTCDate() === b.getUTCDate();
}

function SwimLaneTimeline({ projects, onProjectClick }: {
  projects: ProjectTree[];
  onProjectClick?: (id: string) => void;
}) {
  // Date columns: today and 6 days prior (1 week view)
  const today = new Date();
  const days = Array.from({ length: 7 }, (_, i) => {
    const d = new Date(today);
    d.setDate(d.getDate() - (6 - i));
    return d;
  });

  const formatDay = (d: Date) => {
    const dayNames = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"];
    const monthNames = ["jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"];
    const isToday = sameDay(d, today);
    return isToday
      ? `today`
      : `${dayNames[d.getDay()]} ${monthNames[d.getMonth()]} ${d.getDate()}`;
  };

  if (projects.length === 0) {
    return (
      <div className="flex h-full items-center justify-center">
        <p className="text-xs text-text-muted">no projects yet — create one to see the timeline</p>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full">
      <div className="flex-1 overflow-auto">
        {/* Date headers */}
        <div className="flex sticky top-0 z-10 bg-bg">
          <div className="flex-shrink-0 w-[200px] border-r border-b border-border px-3 py-2">
            <span className="text-[9px] font-medium text-text-muted">// projects</span>
          </div>
          <div className="flex flex-1">
            {days.map((day, di) => {
              const isToday = sameDay(day, today);
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

        {/* Project rows */}
        {projects.map((tree) => {
          const proj = tree.project;
          const events = collectProjectEvents(tree);
          const agentCount = (tree.captain ? 1 : 0) + tree.talons.length;
          return (
            <div key={proj.id} className="flex border-b border-border">
              {/* Project card */}
              <div
                onClick={() => onProjectClick?.(proj.id)}
                onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onProjectClick?.(proj.id); } }}
                role="button"
                tabIndex={0}
                className="flex-shrink-0 w-[200px] border-r border-border p-3 space-y-1.5 cursor-pointer hover:bg-surface-hover/30 transition-colors"
              >
                <div className="flex items-center gap-2">
                  <span className={`h-1.5 w-1.5 flex-shrink-0 rounded-full ${
                    proj.status === "active" ? "bg-green"
                      : proj.status === "paused" ? "bg-purple-400"
                      : "bg-text-muted"
                  }`} />
                  <span className="text-[11px] font-semibold text-text truncate">{proj.name}</span>
                </div>
                {proj.goal && (
                  <div className="text-[9px] text-green truncate">{proj.goal}</div>
                )}
                <div className="text-[9px] text-text-muted">
                  {agentCount} agent{agentCount !== 1 ? "s" : ""}
                </div>
              </div>

              {/* Day columns with real events */}
              <div className="flex flex-1">
                {days.map((day, di) => {
                  const isToday = sameDay(day, today);
                  const dayEvents = events.filter((e) => sameDay(new Date(e.date), day));
                  return (
                    <div
                      key={di}
                      className={`flex-1 flex flex-col items-start justify-center gap-1 px-1.5 py-1 ${
                        di < days.length - 1 ? "border-r border-border" : ""
                      } ${isToday ? "bg-accent/5" : ""}`}
                    >
                      {dayEvents.map((evt, ei) => (
                        <div
                          key={ei}
                          className={`flex items-center gap-1 rounded-sm px-1.5 py-0.5 text-[9px] truncate max-w-full ${EVENT_BG[evt.type]}`}
                          title={evt.label}
                        >
                          <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${EVENT_DOT[evt.type]}`} />
                          <span className="truncate text-text-secondary">{evt.label}</span>
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
        <span className="text-[9px] text-text-muted">events:</span>
        <LegendItem color="bg-accent" label="project created" />
        <LegendItem color="bg-purple-400" label="captain assigned" />
        <LegendItem color="bg-amber-400" label="talon added" />
      </div>
    </div>
  );
}

function LegendItem({ color, label }: { color: string; label: string }) {
  return (
    <div className="flex items-center gap-1.5">
      <div className={`h-2 w-2 rounded-full ${color}`} />
      <span className="text-[9px] text-text-muted">{label}</span>
    </div>
  );
}

// ─── Main Page ───

export default function HierarchyPage() {
  const navigate = useNavigate();
  const { backendDown } = useData();

  // All hooks must come before any conditional returns
  const [hierarchy, setHierarchy] = useState<HierarchyTree | null>(null);
  const hierarchyRef = useRef<HierarchyTree | null>(null);
  const [loading, setLoading] = useState(true);
  const [fetchError, setFetchError] = useState<string | null>(null);
  const [metrics, setMetrics] = useState<DashboardMetrics | null>(null);
  const [changingCommander, setChangingCommander] = useState(false);

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
    if (backendDown) return; // don't poll when backend is unreachable
    refresh();
    const interval = setInterval(refresh, 15000);
    return () => clearInterval(interval);
  }, [refresh, backendDown]);

  // Fetch metrics — runs whenever hierarchy updates
  useEffect(() => {
    if (!hierarchy || backendDown) return;
    fetch("/api/metrics").then((r) => { if (r.ok) return r.json(); throw new Error(`metrics: ${r.status}`); }).then(setMetrics).catch(() => {});
  }, [hierarchy, backendDown]);

  // ─── Conditional returns (after all hooks) ───

  if (loading && !hierarchy) {
    return (
      <div className="py-20 text-center text-xs text-text-muted">
        loading mission control...
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
            <div className="flex items-center gap-2">
              <p className="text-[10px] text-text-muted">
                commander: {hierarchy.commander.display_name || hierarchy.commander.name}
              </p>
              <button
                onClick={() => setChangingCommander(true)}
                className="text-[9px] text-purple-400 hover:text-purple-300 transition-colors"
              >
                change
              </button>
            </div>
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
            onClick={() => navigate("/agents/compare")}
            className="flex items-center gap-1.5 rounded border border-border px-3 py-1.5 text-xs text-text-muted transition-colors hover:text-text"
          >
            <BarChart3 className="h-3 w-3" />
            compare agents
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
          sub={metrics?.stopped_agents && metrics.stopped_agents > 0 ? "needs attention" : undefined}
        />
      </div>

      {/* Commander change overlay */}
      {changingCommander && (
        <div className="border-b border-border px-5 py-3">
          <div className="rounded border border-purple-400/30 bg-purple-400/5 p-4">
            <div className="flex items-center justify-between mb-3">
              <span className="text-xs font-medium text-text">change commander</span>
              <button
                onClick={() => setChangingCommander(false)}
                className="text-[10px] text-text-muted hover:text-text"
              >
                cancel
              </button>
            </div>
            <CommanderSetup onCreated={() => { setChangingCommander(false); refresh(); }} />
          </div>
        </div>
      )}

      {/* Agent summary bar with links */}
      <div className="flex items-center justify-between border-b border-border px-5 py-2">
        <span className="text-[10px] font-medium uppercase tracking-wider text-text-muted">
          // agents: {1 + allCaptains + allTalons}
        </span>
        <div className="flex items-center gap-3">
          <button
            onClick={() => navigate("/mission-control/agents")}
            className="text-[10px] text-accent hover:text-accent/80 transition-colors"
          >
            manage agents &rarr;
          </button>
          <button
            onClick={() => navigate("/agents/compare")}
            className="text-[10px] text-accent hover:text-accent/80 transition-colors"
          >
            compare agents &rarr;
          </button>
        </div>
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
