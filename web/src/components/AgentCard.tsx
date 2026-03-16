import { Link } from "react-router-dom";
import {
  Activity,
  Server,
  Radio,
  Clock,
  Cpu,
  MemoryStick,
  ChevronRight,
} from "lucide-react";
import type { AgentInfo } from "../lib/types";

interface AgentCardProps {
  agent: AgentInfo;
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

export default function AgentCard({ agent }: AgentCardProps) {
  const alive = agent.alive;

  return (
    <Link
      to={`/agents/${agent.name}`}
      className="block w-full text-left rounded-lg border border-border bg-surface p-5 transition-colors hover:bg-surface-hover focus:outline-none focus:ring-2 focus:ring-accent/50"
    >
      <div className="flex items-start justify-between">
        <div className="flex items-center gap-3">
          <div
            className={`h-2.5 w-2.5 rounded-full ${alive ? "bg-green" : "bg-red"}`}
          />
          <div>
            <h3 className="text-base font-semibold text-text">{agent.name}</h3>
            <p className="text-sm text-text-muted">{agent.framework}</p>
          </div>
        </div>
        <ChevronRight className="h-5 w-5 text-text-muted" />
      </div>

      <div className="mt-4 grid grid-cols-2 gap-3 text-sm">
        <div className="flex items-center gap-2 text-text-muted">
          <Activity className="h-4 w-4" />
          <span>{alive ? "Running" : "Stopped"}</span>
        </div>
        <div className="flex items-center gap-2 text-text-muted">
          <Server className="h-4 w-4" />
          <span>:{agent.port}</span>
        </div>
        <div className="flex items-center gap-2 text-text-muted">
          <Clock className="h-4 w-4" />
          <span>{agent.health ? formatUptime(agent.health.uptime) : "-"}</span>
        </div>
        <div className="flex items-center gap-2 text-text-muted">
          <MemoryStick className="h-4 w-4" />
          <span>
            {agent.health ? formatBytes(agent.health.ram_bytes) : "-"}
          </span>
        </div>
        <div className="flex items-center gap-2 text-text-muted">
          <Cpu className="h-4 w-4" />
          <span>
            {agent.health?.cpu_percent != null
              ? `${agent.health.cpu_percent.toFixed(1)}%`
              : "-"}
          </span>
        </div>
      </div>

      {agent.status && (
        <div className="mt-3 flex flex-wrap gap-2">
          {agent.status.provider && (
            <span className="inline-flex items-center gap-1 rounded-md bg-accent/10 px-2 py-0.5 text-xs text-accent">
              <Radio className="h-3 w-3" />
              {agent.status.provider}
            </span>
          )}
          {agent.status.channels?.map((ch) => (
            <span
              key={ch}
              className="rounded-md bg-surface-hover px-2 py-0.5 text-xs text-text-muted"
            >
              {ch}
            </span>
          ))}
        </div>
      )}
    </Link>
  );
}
