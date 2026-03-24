import { useEffect, useState, useCallback } from "react";
import { Routes, Route, Navigate, useParams, useNavigate } from "react-router-dom";
import { RefreshCw } from "lucide-react";
import type { AgentInfo, Project } from "./lib/types";
import { fetchAgents, fetchProjects } from "./lib/api";
import Sidebar from "./components/Sidebar";
import AgentDetail from "./components/AgentDetail";
import InstallPage from "./components/InstallPage";
import PersonasPage from "./components/PersonasPage";
import HierarchyPage from "./components/HierarchyPage";
import ProjectListPage from "./components/ProjectListPage";
import ProjectDetail from "./components/ProjectDetail";

export default function App() {
  const [agents, setAgents] = useState<AgentInfo[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const [agentData, projectData] = await Promise.all([
        fetchAgents(),
        fetchProjects().catch((e) => {
          console.error("Failed to fetch projects:", e);
          return [] as Project[];
        }),
      ]);
      setAgents(agentData);
      setProjects(projectData);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to fetch agents");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
    const interval = setInterval(refresh, 30000);
    return () => clearInterval(interval);
  }, [refresh]);

  return (
    <div className="flex h-screen overflow-hidden">
      <Sidebar agents={agents} projects={projects} />

      <main className="flex-1 overflow-y-auto">
        <div className="mx-auto max-w-5xl px-8 py-8">
          {error && (
            <div className="mb-6 rounded border border-red/30 bg-red/5 px-4 py-3 text-xs text-red">
              {error}
            </div>
          )}

          <Routes>
            <Route path="/" element={<Navigate to="/hierarchy" replace />} />
            <Route path="/hierarchy" element={<HierarchyPage />} />
            <Route path="/projects" element={<ProjectListPage />} />
            <Route path="/projects/:id" element={<ProjectDetail />} />
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
              element={<AgentDetailRoute agents={agents} onRefresh={refresh} />}
            />
            <Route path="/install" element={<InstallPage />} />
            <Route path="/personas" element={<PersonasPage />} />
          </Routes>
        </div>
      </main>
    </div>
  );
}

function formatUptime(nanoseconds: number): string {
  if (!nanoseconds) return "-";
  const seconds = nanoseconds / 1e9;
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const mins = Math.floor((seconds % 3600) / 60);
  if (days > 0) return `${days}d ${hours}h`;
  if (hours > 0) return `${hours}h ${mins}m`;
  return `${mins}m`;
}

function formatBytes(bytes: number): string {
  if (!bytes) return "-";
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)}KB`;
  if (bytes < 1024 * 1024 * 1024)
    return `${(bytes / (1024 * 1024)).toFixed(0)}MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)}GB`;
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
            // monitor agent status and activity
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
                      {agent.name}
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

function AgentDetailRoute({ agents, onRefresh }: { agents: AgentInfo[]; onRefresh: () => Promise<void> }) {
  const { name } = useParams<{ name: string }>();
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

  return <AgentDetail agent={agent} onRefresh={onRefresh} />;
}
