import { Link } from "react-router-dom";
import { useData } from "../lib/DataContext";
import { formatUptime, formatBytes } from "../lib/format";
import type { AgentInfo } from "../lib/types";

const FRAMEWORK_EMOJI: Record<string, string> = {
  zeroclaw: "🌀",
  openclaw: "🦞",
  hermes: "🔱",
};

function StatusBadge({ alive }: { alive: boolean }) {
  return (
    <span
      className={`rounded px-2 py-0.5 text-[10px] font-medium uppercase ${
        alive ? "bg-green/10 text-green" : "bg-red/10 text-red"
      }`}
    >
      {alive ? "running" : "stopped"}
    </span>
  );
}

function ProviderBadge({ status }: { status?: string }) {
  if (!status) return <span className="text-text-muted">-</span>;
  if (status === "ok") {
    return <span className="text-green">ok</span>;
  }
  return <span className="text-red">{status}</span>;
}

/** Horizontal bar for resource comparison charts. */
function Bar({ value, max, color }: { value: number; max: number; color: string }) {
  // Avoid division by zero — show empty bar when max is 0
  const pct = max > 0 ? Math.min((value / max) * 100, 100) : 0;
  return (
    <div className="h-3 w-full rounded bg-surface-hover/50">
      <div
        className={`h-full rounded ${color}`}
        style={{ width: `${pct}%`, minWidth: pct > 0 ? "2px" : "0" }}
      />
    </div>
  );
}

function ResourceChart({
  title,
  agents,
  getValue,
  formatValue,
  color,
}: {
  title: string;
  agents: AgentInfo[];
  getValue: (a: AgentInfo) => number;
  formatValue: (a: AgentInfo) => string;
  color: string;
}) {
  const maxVal = Math.max(...agents.map(getValue), 0);

  return (
    <div className="rounded border border-border bg-surface p-4">
      <h3 className="mb-3 text-xs font-medium text-text-muted">{title}</h3>
      <div className="space-y-2">
        {agents.map((agent) => {
          const val = getValue(agent);
          return (
            <div key={agent.name} className="flex items-center gap-3">
              <span className="w-24 shrink-0 truncate text-xs text-text-secondary">
                {agent.display_name || agent.name}
              </span>
              <div className="flex-1">
                <Bar value={val} max={maxVal} color={color} />
              </div>
              <span className="w-16 shrink-0 text-right text-xs text-text-muted">
                {formatValue(agent)}
              </span>
            </div>
          );
        })}
        {agents.length === 0 && (
          <p className="text-xs text-text-muted">no agents to compare</p>
        )}
      </div>
    </div>
  );
}

export default function AgentCompare() {
  const { agents, loading } = useData();

  return (
    <div className="space-y-6">
      <div className="text-xs text-text-muted">
        <Link to="/agents/overview" className="hover:text-text transition-colors">
          ~/agents
        </Link>
        /compare
      </div>

      <h1 className="text-xl font-bold">
        <span className="text-accent">&gt;</span> agent comparison
      </h1>

      {loading && agents.length === 0 ? (
        <div className="py-12 text-center text-xs text-text-muted">
          loading agents...
        </div>
      ) : agents.length === 0 ? (
        <div className="rounded border border-border bg-surface p-8 text-center text-xs text-text-muted">
          no agents discovered. install a framework to get started.
        </div>
      ) : (
        <>
          {/* Comparison table */}
          <div className="overflow-x-auto overflow-hidden rounded border border-border">
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b border-border bg-surface text-left text-text-muted">
                  <th className="px-4 py-2.5 font-medium">name</th>
                  <th className="px-4 py-2.5 font-medium">framework</th>
                  <th className="px-4 py-2.5 font-medium">status</th>
                  <th className="px-4 py-2.5 font-medium">model</th>
                  <th className="px-4 py-2.5 font-medium">provider</th>
                  <th className="px-4 py-2.5 font-medium">provider health</th>
                  <th className="px-4 py-2.5 font-medium">uptime</th>
                  <th className="px-4 py-2.5 font-medium text-right">memory</th>
                  <th className="px-4 py-2.5 font-medium text-right">cpu</th>
                  <th className="px-4 py-2.5 font-medium text-right">errors (24h)</th>
                </tr>
              </thead>
              <tbody className="[&>tr+tr]:border-t [&>tr+tr]:border-border">
                {agents.map((agent) => (
                  <tr key={agent.name} className="transition-colors hover:bg-surface-hover/50">
                    <td className="px-4 py-2.5">
                      <Link
                        to={`/agents/${agent.name}`}
                        className="flex items-center gap-2 hover:text-accent transition-colors"
                      >
                        <span
                          className={`h-1.5 w-1.5 shrink-0 rounded-full ${
                            agent.alive ? "bg-green" : "bg-red"
                          }`}
                        />
                        {agent.display_name || agent.name}
                      </Link>
                    </td>
                    <td className="px-4 py-2.5 text-text-secondary">
                      {FRAMEWORK_EMOJI[agent.framework] || ""} {agent.framework}
                    </td>
                    <td className="px-4 py-2.5">
                      <StatusBadge alive={agent.alive} />
                    </td>
                    <td className="px-4 py-2.5 text-text-secondary">
                      {agent.status?.model || "-"}
                    </td>
                    <td className="px-4 py-2.5 text-text-secondary">
                      {agent.status?.provider || "-"}
                    </td>
                    <td className="px-4 py-2.5">
                      <ProviderBadge status={agent.status?.provider_status} />
                    </td>
                    <td className="px-4 py-2.5 text-text-secondary">
                      {formatUptime(agent.health?.uptime)}
                    </td>
                    <td className="px-4 py-2.5 text-right text-text-secondary">
                      {formatBytes(agent.health?.ram_bytes)}
                    </td>
                    <td className="px-4 py-2.5 text-right text-text-secondary">
                      {agent.health?.cpu_percent != null
                        ? `${agent.health.cpu_percent.toFixed(1)}%`
                        : "-"}
                    </td>
                    <td className="px-4 py-2.5 text-right">
                      {agent.status?.errors_24h != null ? (
                        <span className={agent.status.errors_24h > 0 ? "text-red" : "text-text-secondary"}>
                          {agent.status.errors_24h}
                        </span>
                      ) : (
                        <span className="text-text-muted">-</span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {/* Resource comparison bar charts */}
          <div>
            <h2 className="mb-3 text-xs font-medium uppercase tracking-wider text-text-muted">
              resource comparison
            </h2>
            <div className="grid gap-4 md:grid-cols-2">
              <ResourceChart
                title="memory usage"
                agents={agents}
                getValue={(a) => a.health?.ram_bytes ?? 0}
                formatValue={(a) => formatBytes(a.health?.ram_bytes)}
                color="bg-purple-400"
              />
              <ResourceChart
                title="cpu usage"
                agents={agents}
                getValue={(a) => a.health?.cpu_percent ?? 0}
                formatValue={(a) =>
                  a.health?.cpu_percent != null
                    ? `${a.health.cpu_percent.toFixed(1)}%`
                    : "-"
                }
                color="bg-accent"
              />
            </div>
          </div>
        </>
      )}
    </div>
  );
}
