import { Link } from "react-router-dom";
import { useData } from "../lib/DataContext";
import { formatBytes } from "../lib/format";
import { useAgentMetrics, latencyPercentiles } from "../lib/useAgentMetrics";
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

function Bar({ value, max, color }: { value: number; max: number; color: string }) {
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
        {agents.map((agent) => (
          <div key={agent.name} className="flex items-center gap-3">
            <span className="w-24 shrink-0 truncate text-xs text-text-secondary">
              {agent.display_name || agent.name}
            </span>
            <div className="flex-1">
              <Bar value={getValue(agent)} max={maxVal} color={color} />
            </div>
            <span className="w-16 shrink-0 text-right text-xs text-text-muted">
              {formatValue(agent)}
            </span>
          </div>
        ))}
        {agents.length === 0 && (
          <p className="text-xs text-text-muted">no agents to compare</p>
        )}
      </div>
    </div>
  );
}

/** Bar chart using raw numeric values instead of AgentInfo */
function MetricChart({
  title,
  items,
  color,
  note,
}: {
  title: string;
  items: { name: string; value: number; label: string }[];
  color: string;
  note?: string;
}) {
  const maxVal = Math.max(...items.map((i) => i.value), 0);
  return (
    <div className="rounded border border-border bg-surface p-4">
      <h3 className="mb-1 text-xs font-medium text-text-muted">{title}</h3>
      {note && <p className="mb-3 text-[10px] text-text-muted/60">{note}</p>}
      <div className="space-y-2">
        {items.map((item) => (
          <div key={item.name} className="flex items-center gap-3">
            <span className="w-24 shrink-0 truncate text-xs text-text-secondary">
              {item.name}
            </span>
            <div className="flex-1">
              <Bar value={item.value} max={maxVal} color={color} />
            </div>
            <span className="w-20 shrink-0 text-right text-xs text-text-muted">
              {item.label}
            </span>
          </div>
        ))}
        {items.length === 0 && (
          <p className="text-xs text-text-muted">no data yet — send messages to collect metrics</p>
        )}
      </div>
    </div>
  );
}

function formatMs(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

export default function AgentCompare() {
  const { agents, loading } = useData();
  const { metrics } = useAgentMetrics();

  // Build latency data for charts
  const latencyItems = agents
    .map((a) => {
      const p = latencyPercentiles(a.name);
      const m = metrics[a.name];
      return {
        agent: a,
        name: a.display_name || a.name,
        avg: m && m.latencies.length > 0
          ? Math.round(m.latencies.reduce((s, v) => s + v, 0) / m.latencies.length)
          : null,
        p50: p?.p50 ?? null,
        p90: p?.p90 ?? null,
        samples: m?.latencies.length ?? 0,
      };
    })
    .filter((i) => i.avg !== null);

  return (
    <div className="space-y-6">
      <div className="text-xs text-text-muted">
        <Link to="/agents/overview" className="hover:text-text transition-colors">
          ~/agents
        </Link>
        /compare
      </div>

      <div>
        <h1 className="text-xl font-bold">
          <span className="text-accent">&gt;</span> agent comparison
        </h1>
        <p className="mt-1 text-xs text-text-muted">
          // latency is measured from send to first token — send messages to collect data
        </p>
      </div>

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
                  <th className="px-4 py-2.5 font-medium text-right">latency (avg)</th>
                  <th className="px-4 py-2.5 font-medium text-right">latency (p90)</th>
                  <th className="px-4 py-2.5 font-medium text-right">samples</th>
                  <th className="px-4 py-2.5 font-medium">provider</th>
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
                      <td className="px-4 py-2.5 text-text-secondary truncate max-w-32">
                        {agent.status?.model || "-"}
                      </td>
                      <td className="px-4 py-2.5 text-right font-mono">
                        {avg !== null ? (
                          <span className={avg > 10000 ? "text-red" : avg > 5000 ? "text-yellow" : "text-accent"}>
                            {formatMs(avg)}
                          </span>
                        ) : (
                          <span className="text-text-muted">-</span>
                        )}
                      </td>
                      <td className="px-4 py-2.5 text-right font-mono text-text-secondary">
                        {p?.p90 != null ? formatMs(p.p90) : "-"}
                      </td>
                      <td className="px-4 py-2.5 text-right text-text-muted">
                        {m?.latencies.length ?? 0}
                      </td>
                      <td className="px-4 py-2.5 text-text-secondary truncate max-w-28">
                        {agent.status?.provider || "-"}
                        {agent.status?.provider_status && (
                          <span className={`ml-1.5 text-[10px] ${agent.status.provider_status === "ok" ? "text-green" : "text-red"}`}>
                            {agent.status.provider_status === "ok" ? "✓" : "✗"}
                          </span>
                        )}
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
                  );
                })}
              </tbody>
            </table>
          </div>

          {/* Latency comparison */}
          <div>
            <h2 className="mb-3 text-xs font-medium uppercase tracking-wider text-text-muted">
              response latency
            </h2>
            <div className="grid gap-4 md:grid-cols-2">
              <MetricChart
                title="average latency (time to first token)"
                items={latencyItems.map((i) => ({
                  name: i.name,
                  value: i.avg!,
                  label: formatMs(i.avg!),
                }))}
                color="bg-accent"
                note="lower is better"
              />
              <MetricChart
                title="p90 latency"
                items={latencyItems
                  .filter((i) => i.p90 !== null)
                  .map((i) => ({
                    name: i.name,
                    value: i.p90!,
                    label: formatMs(i.p90!),
                  }))}
                color="bg-yellow"
                note="90th percentile — worst-case typical"
              />
            </div>
          </div>

          {/* Resource comparison */}
          <div>
            <h2 className="mb-3 text-xs font-medium uppercase tracking-wider text-text-muted">
              resource usage
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
