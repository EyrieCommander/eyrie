import { Routes, Route, Navigate, useParams, useNavigate, Link } from "react-router-dom";
import { RefreshCw } from "lucide-react";
import type { AgentInfo } from "./lib/types";
import { formatUptime, formatBytes } from "./lib/format";
import { DataProvider, useData } from "./lib/DataContext";
import Sidebar from "./components/Sidebar";
import AgentDetail from "./components/AgentDetail";
import PersonasPage from "./components/PersonasPage";
import HierarchyPage from "./components/HierarchyPage";
import OnboardingFlow from "./components/OnboardingFlow";
import AgentsPage from "./components/AgentsPage";
import ProjectListPage from "./components/ProjectListPage";
import ProjectDetail from "./components/ProjectDetail";
import SettingsPage from "./components/SettingsPage";
import FrameworkDetail from "./components/FrameworkDetail";
import AgentCompare from "./components/AgentCompare";
import FrameworkCompare from "./components/FrameworkCompare";
import MeshStatusPage from "./components/MeshStatusPage";
import CommanderChat from "./components/CommanderChat";
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
  const { agents, loading, error, backendDown, refresh } = useData();
  useFont(); // Apply saved font on mount
  useTheme(); // Apply saved theme on mount

  return (
    <div className="flex h-screen overflow-hidden">
      <Sidebar />

      <main className="flex-1 overflow-hidden min-w-0">
        {/* Persistent banner when backend is unreachable — shown across all
            routes so the user always knows why data isn't updating. */}
        {backendDown && (
          <div className="flex items-center gap-2 border-b border-yellow-500/30 bg-yellow-500/5 px-4 py-2 text-xs text-yellow-400">
            <span className="h-1.5 w-1.5 rounded-full bg-yellow-400 animate-pulse" />
            backend unreachable — retrying...
          </div>
        )}
        <Routes>
          {/* Full-width routes (no padding/max-width) */}
          <Route path="/projects/:id" element={<ProjectDetail />} />

          {/* Constrained routes */}
          <Route path="*" element={
            <div className="mx-auto max-w-5xl px-8 py-8 h-full overflow-y-auto">
              {error && !backendDown && (
                <div className="mb-6 rounded border border-red/30 bg-red/5 px-4 py-3 text-xs text-red">
                  {error}
                </div>
              )}
              <Routes>
                <Route path="/" element={<OnboardingFlow />} />
                <Route path="/hierarchy" element={<Navigate to="/mission-control" replace />} />
                <Route path="/mission-control" element={<HierarchyPage />} />
                <Route path="/mission-control/agents" element={<AgentsPage />} />
                <Route path="/mission-control/mesh" element={<MeshStatusPage />} />
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
            <Route path="/agents/compare" element={<AgentCompare />} />
            <Route path="/frameworks" element={<FrameworkCompare />} />
            <Route path="/frameworks/compare" element={<Navigate to="/frameworks" replace />} />
            <Route path="/install" element={<Navigate to="/frameworks" replace />} />
            <Route
              path="/agents/:name/:tab?"
              element={<AgentDetailRoute />}
            />
                <Route path="/frameworks/:id" element={<FrameworkDetail />} />
                <Route path="/personas" element={<PersonasPage />} />
                <Route path="/settings" element={<SettingsPage />} />
              </Routes>
            </div>
          } />
        </Routes>
      </main>

      <CommanderChat />
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
  const running = agents.filter((a) => a.alive).length;
  const totalUptime = agents.reduce(
    (sum, a) => sum + (a.health?.uptime ?? 0),
    0,
  );
  const avgUptime = agents.length > 0 ? totalUptime / agents.length : 0;

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
          Discovering agents...
        </div>
      ) : agents.length === 0 ? (
        <div className="rounded border border-border bg-surface p-8 text-center text-xs text-text-muted">
          No agents discovered. Make sure ZeroClaw or OpenClaw is installed and
          configured.
        </div>
      ) : (
        <div className="overflow-hidden rounded border border-border">
          <table className="w-full text-xs">
            <thead>
              <tr className="border-b border-border bg-surface text-left text-text-muted">
                <th className="px-4 py-2.5 font-medium">name</th>
                <th className="px-4 py-2.5 font-medium">framework</th>
                <th className="px-4 py-2.5 font-medium">status</th>
                <th className="px-4 py-2.5 font-medium">port</th>
                <th className="px-4 py-2.5 font-medium">memory</th>
                <th className="px-4 py-2.5 font-medium">cpu</th>
              </tr>
            </thead>
            <tbody className="[&>tr+tr]:border-t [&>tr+tr]:border-border">
              {agents.map((agent) => (
                <tr
                  key={agent.name}
                  onClick={() => navigate(`/agents/${agent.name}`)}
                  className="group relative cursor-pointer transition-all hover:bg-surface-hover/50 hover:shadow-[inset_0_0_0_1px_var(--color-accent)] hover:z-10"
                >
                  <td className="px-4 py-2.5 transition-colors group-hover:text-accent">
                    <span className="flex items-center gap-2">
                      <span
                        className={`h-1.5 w-1.5 rounded-full ${agent.alive ? "bg-green" : "bg-red"}`}
                      />
                      {agent.display_name || agent.name}
                    </span>
                  </td>
                  <td className="px-4 py-2.5 text-text-secondary transition-colors group-hover:text-accent">
                    {agent.framework}
                  </td>
                  <td className="px-4 py-2.5">
                    <span
                      className={`rounded px-2 py-0.5 text-[10px] font-medium uppercase ${
                        agent.alive
                          ? "bg-green/10 text-green"
                          : "bg-red/10 text-red"
                      }`}
                    >
                      {agent.alive ? "running" : "stopped"}
                    </span>
                  </td>
                  <td className="px-4 py-2.5 text-text-secondary transition-colors group-hover:text-accent">
                    :{agent.port}
                  </td>
                  <td className="px-4 py-2.5 text-text-secondary transition-colors group-hover:text-accent">
                    {agent.health ? formatBytes(agent.health.ram_bytes) : "-"}
                  </td>
                  <td className="px-4 py-2.5 text-text-secondary transition-colors group-hover:text-accent">
                    {agent.health?.cpu_percent != null
                      ? `${agent.health.cpu_percent.toFixed(1)}%`
                      : "-"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {agents.length > 0 && (
        <div className="flex justify-end">
          <Link
            to="/agents/compare"
            className="inline-flex items-center gap-1.5 text-xs font-medium text-text-secondary hover:text-accent transition-colors"
          >
            compare agent performance →
          </Link>
        </div>
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
