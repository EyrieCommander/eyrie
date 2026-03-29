import { Routes, Route, Navigate, useParams, useNavigate } from "react-router-dom";
import { RefreshCw } from "lucide-react";
import type { AgentInfo } from "./lib/types";
import { formatUptime, formatBytes } from "./lib/format";
import { useAgentMetrics, latencyPercentiles } from "./lib/useAgentMetrics";
import { DataProvider, useData } from "./lib/DataContext";
import Sidebar from "./components/Sidebar";
import AgentDetail from "./components/AgentDetail";
import InstallPage from "./components/InstallPage";
import PersonasPage from "./components/PersonasPage";
import HierarchyPage from "./components/HierarchyPage";
import ProjectListPage from "./components/ProjectListPage";
import ProjectDetail from "./components/ProjectDetail";
import SettingsPage from "./components/SettingsPage";
import FrameworkDetail from "./components/FrameworkDetail";
import { useFont } from "./lib/useFont";
import { useTheme } from "./lib/useTheme";

export default function App() {
  return (
    <DataProvider>
      <AppContent />
    </DataProvider>
  );
}

function AppContent() {
  const { agents, loading, error, refresh } = useData();
  useFont(); // Apply saved font on mount
  useTheme(); // Apply saved theme on mount

  return (
    <div className="flex h-screen overflow-hidden">
      <Sidebar />

      <main className="flex-1 overflow-hidden">
        <Routes>
          {/* Full-width routes (no padding/max-width) */}
          <Route path="/projects/:id" element={<ProjectDetail />} />

          {/* Constrained routes */}
          <Route path="*" element={
            <div className="mx-auto max-w-5xl px-8 py-8 h-full overflow-y-auto">
              {error && (
                <div className="mb-6 rounded border border-red/30 bg-red/5 px-4 py-3 text-xs text-red">
                  {error}
                </div>
              )}
              <Routes>
                <Route path="/" element={<Navigate to="/hierarchy" replace />} />
                <Route path="/hierarchy" element={<HierarchyPage />} />
                <Route path="/projects" element={<ProjectListPage />} />
            <Route
              path="/agents/overview"
              element={
                <AgentList
                  agents={agents}
                  loading={loading}
                  onRefresh={refresh}
                />
              }
            />
            <Route
              path="/agents/:name/:tab?"
              element={<AgentDetailRoute />}
            />
                <Route path="/install" element={<InstallPage />} />
                <Route path="/frameworks/:id" element={<FrameworkDetail />} />
                <Route path="/personas" element={<PersonasPage />} />
                <Route path="/settings" element={<SettingsPage />} />
              </Routes>
            </div>
          } />
        </Routes>
      </main>
    </div>
  );
}

function formatMs(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

function MetricBar({ value, max, color }: { value: number; max: number; color: string }) {
  const pct = max > 0 ? Math.min((value / max) * 100, 100) : 0;
  return (
    <div className="h-3 w-full rounded bg-surface-hover/50">
      <div className={`h-full rounded ${color}`} style={{ width: `${pct}%`, minWidth: pct > 0 ? "2px" : "0" }} />
    </div>
  );
}

function AgentList({
  agents,
  loading,
  onRefresh,
}: {
  agents: AgentInfo[];
  loading: boolean;
  onRefresh: () => void;
}) {
  const navigate = useNavigate();
  const { metrics } = useAgentMetrics();
  const running = agents.filter((a) => a.alive).length;
  const totalUptime = agents.reduce(
    (sum, a) => sum + (a.health?.uptime ?? 0),
    0,
  );
  const avgUptime = agents.length > 0 ? totalUptime / agents.length : 0;

  // Build latency data for bar charts
  const latencyData = agents
    .map((a) => {
      const m = metrics[a.name];
      const avg = m && m.latencies.length > 0
        ? Math.round(m.latencies.reduce((s, v) => s + v, 0) / m.latencies.length)
        : null;
      const p = latencyPercentiles(a.name);
      return { name: a.display_name || a.name, avg, p90: p?.p90 ?? null, samples: m?.latencies.length ?? 0 };
    })
    .filter((d) => d.avg !== null);

  return (
    <div className="space-y-6">
      <div className="text-xs text-text-muted">~/agents/overview</div>

      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-bold"><span className="text-accent">&gt;</span> agent_overview</h1>
          <p className="mt-1 text-xs text-text-muted">
            // monitor agent status and performance
          </p>
        </div>
        <button
          onClick={onRefresh}
          disabled={loading}
          className="flex items-center gap-2 text-xs text-text-muted transition-colors hover:text-text disabled:opacity-50"
        >
          <RefreshCw className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} />
          $ refresh
        </button>
      </div>

      <div className="grid grid-cols-3 gap-4">
        <StatCard label="total_agents" value={String(agents.length)} />
        <StatCard label="running" value={String(running)} highlight />
        <StatCard label="avg_uptime" value={formatUptime(avgUptime)} />
      </div>

      {loading && agents.length === 0 ? (
        <div className="py-12 text-center text-xs text-text-muted">
          discovering agents...
        </div>
      ) : agents.length === 0 ? (
        <div className="rounded border border-border bg-surface p-8 text-center text-xs text-text-muted">
          no agents discovered. install a framework to get started.
        </div>
      ) : (
        <>
          <div className="overflow-x-auto overflow-hidden rounded border border-border">
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b border-border bg-surface text-left text-text-muted">
                  <th className="px-4 py-2.5 font-medium">name</th>
                  <th className="px-4 py-2.5 font-medium">framework</th>
                  <th className="px-4 py-2.5 font-medium">status</th>
                  <th className="px-4 py-2.5 font-medium">model</th>
                  <th className="px-4 py-2.5 font-medium">port</th>
                  <th className="px-4 py-2.5 font-medium">provider</th>
                  <th className="px-4 py-2.5 font-medium text-right">latency (avg)</th>
                  <th className="px-4 py-2.5 font-medium text-right">latency (p90)</th>
                  <th className="px-4 py-2.5 font-medium text-right">memory</th>
                  <th className="px-4 py-2.5 font-medium text-right">cpu</th>
                  <th className="px-4 py-2.5 font-medium text-right">errors (24h)</th>
                </tr>
              </thead>
              <tbody className="[&>tr+tr]:border-t [&>tr+tr]:border-border">
                {agents.map((agent) => {
                  const m = metrics[agent.name];
                  const avg = m && m.latencies.length > 0
                    ? Math.round(m.latencies.reduce((s, v) => s + v, 0) / m.latencies.length)
                    : null;
                  const p = latencyPercentiles(agent.name);
                  return (
                    <tr
                      key={agent.name}
                      onClick={() => navigate(`/agents/${agent.name}`)}
                      className="group relative cursor-pointer transition-all hover:bg-surface-hover/50 hover:shadow-[inset_0_0_0_1px_var(--color-accent)] hover:z-10"
                    >
                      <td className="px-4 py-2.5 transition-colors group-hover:text-accent">
                        <span className="flex items-center gap-2">
                          <span className={`h-1.5 w-1.5 rounded-full ${agent.alive ? "bg-green" : "bg-red"}`} />
                          {agent.display_name || agent.name}
                        </span>
                      </td>
                      <td className="px-4 py-2.5 text-text-secondary">{agent.framework}</td>
                      <td className="px-4 py-2.5">
                        <span className={`rounded px-2 py-0.5 text-[10px] font-medium uppercase ${agent.alive ? "bg-green/10 text-green" : "bg-red/10 text-red"}`}>
                          {agent.alive ? "running" : "stopped"}
                        </span>
                      </td>
                      <td className="px-4 py-2.5 text-text-secondary truncate max-w-32">{agent.status?.model || "-"}</td>
                      <td className="px-4 py-2.5 text-text-secondary">:{agent.port}</td>
                      <td className="px-4 py-2.5 text-text-secondary">
                        {agent.status?.provider || "-"}
                        {agent.status?.provider_status && (
                          <span className={`ml-1.5 text-[10px] ${agent.status.provider_status === "ok" ? "text-green" : "text-red"}`}>
                            {agent.status.provider_status === "ok" ? "✓" : "✗"}
                          </span>
                        )}
                      </td>
                      <td className="px-4 py-2.5 text-right font-mono">
                        {avg !== null ? (
                          <span className={avg > 10000 ? "text-red" : avg > 5000 ? "text-yellow" : "text-accent"}>{formatMs(avg)}</span>
                        ) : <span className="text-text-muted">-</span>}
                      </td>
                      <td className="px-4 py-2.5 text-right font-mono text-text-secondary">
                        {p?.p90 != null ? formatMs(p.p90) : "-"}
                      </td>
                      <td className="px-4 py-2.5 text-right text-text-secondary">{agent.health ? formatBytes(agent.health.ram_bytes) : "-"}</td>
                      <td className="px-4 py-2.5 text-right text-text-secondary">
                        {agent.health?.cpu_percent != null ? `${agent.health.cpu_percent.toFixed(1)}%` : "-"}
                      </td>
                      <td className="px-4 py-2.5 text-right">
                        {agent.status?.errors_24h != null ? (
                          <span className={agent.status.errors_24h > 0 ? "text-red" : "text-text-secondary"}>{agent.status.errors_24h}</span>
                        ) : <span className="text-text-muted">-</span>}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>

          {/* Performance charts */}
          {(latencyData.length > 0 || agents.some((a) => a.health)) && (
            <div>
              <h2 className="mb-3 text-[10px] font-medium uppercase tracking-wider text-text-muted">
                performance comparison
              </h2>
              <div className="grid gap-4 md:grid-cols-2">
                {latencyData.length > 0 && (
                  <div className="rounded border border-border bg-surface p-4">
                    <h3 className="mb-1 text-xs font-medium text-text-muted">avg latency</h3>
                    <p className="mb-3 text-[10px] text-text-muted/60">time to first token — lower is better</p>
                    <div className="space-y-2">
                      {latencyData.map((d) => (
                        <div key={d.name} className="flex items-center gap-3">
                          <span className="w-20 shrink-0 truncate text-xs text-text-secondary">{d.name}</span>
                          <div className="flex-1">
                            <MetricBar value={d.avg!} max={Math.max(...latencyData.map((x) => x.avg!))} color="bg-accent" />
                          </div>
                          <span className="w-14 shrink-0 text-right text-xs text-text-muted">{formatMs(d.avg!)}</span>
                        </div>
                      ))}
                    </div>
                  </div>
                )}
                {latencyData.filter((d) => d.p90 !== null).length > 0 && (
                  <div className="rounded border border-border bg-surface p-4">
                    <h3 className="mb-1 text-xs font-medium text-text-muted">p90 latency</h3>
                    <p className="mb-3 text-[10px] text-text-muted/60">90th percentile — worst-case typical</p>
                    <div className="space-y-2">
                      {latencyData.filter((d) => d.p90 !== null).map((d) => (
                        <div key={d.name} className="flex items-center gap-3">
                          <span className="w-20 shrink-0 truncate text-xs text-text-secondary">{d.name}</span>
                          <div className="flex-1">
                            <MetricBar value={d.p90!} max={Math.max(...latencyData.map((x) => x.p90 ?? 0))} color="bg-yellow" />
                          </div>
                          <span className="w-14 shrink-0 text-right text-xs text-text-muted">{formatMs(d.p90!)}</span>
                        </div>
                      ))}
                    </div>
                  </div>
                )}
                <div className="rounded border border-border bg-surface p-4">
                  <h3 className="mb-3 text-xs font-medium text-text-muted">memory</h3>
                  <div className="space-y-2">
                    {agents.map((a) => (
                      <div key={a.name} className="flex items-center gap-3">
                        <span className="w-20 shrink-0 truncate text-xs text-text-secondary">{a.display_name || a.name}</span>
                        <div className="flex-1">
                          <MetricBar value={a.health?.ram_bytes ?? 0} max={Math.max(...agents.map((x) => x.health?.ram_bytes ?? 0))} color="bg-purple-400" />
                        </div>
                        <span className="w-14 shrink-0 text-right text-xs text-text-muted">{formatBytes(a.health?.ram_bytes)}</span>
                      </div>
                    ))}
                  </div>
                </div>
                <div className="rounded border border-border bg-surface p-4">
                  <h3 className="mb-3 text-xs font-medium text-text-muted">cpu</h3>
                  <div className="space-y-2">
                    {agents.map((a) => (
                      <div key={a.name} className="flex items-center gap-3">
                        <span className="w-20 shrink-0 truncate text-xs text-text-secondary">{a.display_name || a.name}</span>
                        <div className="flex-1">
                          <MetricBar value={a.health?.cpu_percent ?? 0} max={Math.max(...agents.map((x) => x.health?.cpu_percent ?? 0), 1)} color="bg-accent" />
                        </div>
                        <span className="w-14 shrink-0 text-right text-xs text-text-muted">
                          {a.health?.cpu_percent != null ? `${a.health.cpu_percent.toFixed(1)}%` : "-"}
                        </span>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  );
}

function StatCard({
  label,
  value,
  highlight,
}: {
  label: string;
  value: string;
  highlight?: boolean;
}) {
  return (
    <div className="rounded border border-border bg-surface p-4">
      <p className="text-[10px] font-medium uppercase tracking-wider text-text-muted">
        {label}
      </p>
      <p
        className={`mt-1.5 text-xl font-bold ${highlight ? "text-accent" : "text-text"}`}
      >
        {value}
      </p>
    </div>
  );
}

function AgentDetailRoute() {
  const { name } = useParams<{ name: string }>();
  const { agents, refresh } = useData();
  const agent = agents.find((a) => a.name === name);

  if (!agent) {
    return (
      <div className="py-20 text-center text-xs text-text-muted">
        {agents.length === 0
          ? "Loading agents..."
          : `Agent "${name}" not found.`}
      </div>
    );
  }

  return <AgentDetail agent={agent} onRefresh={refresh} />;
}
