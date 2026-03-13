import { useEffect, useState, useCallback } from "react";
import {
  ArrowLeft,
  Play,
  Square,
  RotateCcw,
  ScrollText,
  Settings,
} from "lucide-react";
import type { AgentInfo, LogEntry } from "../lib/types";
import { agentAction, fetchAgentConfig, streamLogs } from "../lib/api";

interface AgentDetailProps {
  agent: AgentInfo;
  onBack: () => void;
}

type Tab = "overview" | "logs" | "config";

export default function AgentDetail({ agent, onBack }: AgentDetailProps) {
  const [tab, setTab] = useState<Tab>("overview");
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [config, setConfig] = useState<string | null>(null);
  const [actionPending, setActionPending] = useState(false);

  const handleAction = useCallback(
    async (action: "start" | "stop" | "restart") => {
      setActionPending(true);
      try {
        await agentAction(agent.name, action);
      } catch (e) {
        console.error(e);
      } finally {
        setActionPending(false);
      }
    },
    [agent.name],
  );

  useEffect(() => {
    if (tab === "logs" && agent.alive) {
      const close = streamLogs(agent.name, (entry) => {
        setLogs((prev) => [...prev.slice(-200), entry]);
      });
      return close;
    }
  }, [tab, agent.name, agent.alive]);

  useEffect(() => {
    if (tab === "config" && agent.alive) {
      fetchAgentConfig(agent.name).then(setConfig).catch(console.error);
    }
  }, [tab, agent.name, agent.alive]);

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <button
          onClick={onBack}
          className="flex items-center gap-2 text-text-muted transition-colors hover:text-text"
        >
          <ArrowLeft className="h-4 w-4" />
          Back
        </button>

        <div className="flex gap-2">
          {!agent.alive ? (
            <ActionButton
              icon={<Play className="h-4 w-4" />}
              label="Start"
              onClick={() => handleAction("start")}
              disabled={actionPending}
            />
          ) : (
            <>
              <ActionButton
                icon={<RotateCcw className="h-4 w-4" />}
                label="Restart"
                onClick={() => handleAction("restart")}
                disabled={actionPending}
              />
              <ActionButton
                icon={<Square className="h-4 w-4" />}
                label="Stop"
                onClick={() => handleAction("stop")}
                disabled={actionPending}
                variant="danger"
              />
            </>
          )}
        </div>
      </div>

      <div>
        <div className="flex items-center gap-3">
          <div
            className={`h-3 w-3 rounded-full ${agent.alive ? "bg-green" : "bg-red"}`}
          />
          <h2 className="text-2xl font-bold">{agent.name}</h2>
          <span className="rounded-md bg-surface-hover px-2 py-1 text-sm text-text-muted">
            {agent.framework}
          </span>
        </div>
        <p className="mt-1 text-text-muted">
          Gateway: {agent.host}:{agent.port}
        </p>
      </div>

      <div className="flex gap-1 border-b border-border">
        <TabButton
          active={tab === "overview"}
          onClick={() => setTab("overview")}
        >
          Overview
        </TabButton>
        <TabButton active={tab === "logs"} onClick={() => setTab("logs")}>
          <ScrollText className="mr-1.5 inline h-4 w-4" />
          Logs
        </TabButton>
        <TabButton active={tab === "config"} onClick={() => setTab("config")}>
          <Settings className="mr-1.5 inline h-4 w-4" />
          Config
        </TabButton>
      </div>

      {tab === "overview" && <OverviewTab agent={agent} />}
      {tab === "logs" && <LogsTab logs={logs} alive={agent.alive} />}
      {tab === "config" && <ConfigTab config={config} alive={agent.alive} />}
    </div>
  );
}

function OverviewTab({ agent }: { agent: AgentInfo }) {
  const health = agent.health;
  const status = agent.status;

  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <InfoCard label="Status" value={agent.alive ? "Running" : "Stopped"} />
      <InfoCard label="PID" value={health?.pid?.toString() ?? "-"} />
      <InfoCard
        label="Uptime"
        value={
          health?.uptime
            ? `${Math.floor(health.uptime / 3600)}h ${Math.floor((health.uptime % 3600) / 60)}m`
            : "-"
        }
      />
      <InfoCard label="Provider" value={status?.provider ?? "-"} />
      <InfoCard label="Model" value={status?.model ?? "-"} />
      <InfoCard
        label="Channels"
        value={status?.channels?.join(", ") || "-"}
      />

      {health?.components && Object.keys(health.components).length > 0 && (
        <div className="col-span-full">
          <h3 className="mb-2 text-sm font-medium text-text-muted">
            Components
          </h3>
          <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
            {Object.entries(health.components).map(([name, comp]) => (
              <div
                key={name}
                className="rounded-md border border-border bg-surface p-3"
              >
                <div className="flex items-center justify-between">
                  <span className="text-sm font-medium">{name}</span>
                  <span
                    className={`text-xs ${comp.status === "ok" ? "text-green" : "text-red"}`}
                  >
                    {comp.status}
                  </span>
                </div>
                {comp.restart_count > 0 && (
                  <p className="mt-1 text-xs text-yellow">
                    Restarts: {comp.restart_count}
                  </p>
                )}
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function LogsTab({ logs, alive }: { logs: LogEntry[]; alive: boolean }) {
  if (!alive) {
    return (
      <p className="text-text-muted">Agent is not running. Start it to see logs.</p>
    );
  }

  return (
    <div className="max-h-[60vh] overflow-y-auto rounded-lg border border-border bg-surface p-4 font-mono text-sm">
      {logs.length === 0 ? (
        <p className="text-text-muted">Waiting for log entries...</p>
      ) : (
        logs.map((entry, i) => (
          <div key={i} className="py-0.5">
            <span className="text-text-muted">
              {new Date(entry.timestamp).toLocaleTimeString()}
            </span>{" "}
            <span
              className={
                entry.level === "error"
                  ? "text-red"
                  : entry.level === "warn"
                    ? "text-yellow"
                    : "text-text-muted"
              }
            >
              [{entry.level}]
            </span>{" "}
            <span className="text-text">{entry.message}</span>
          </div>
        ))
      )}
    </div>
  );
}

function ConfigTab({
  config,
  alive,
}: {
  config: string | null;
  alive: boolean;
}) {
  if (!alive) {
    return (
      <p className="text-text-muted">
        Agent is not running. Start it to view config.
      </p>
    );
  }

  return (
    <pre className="max-h-[60vh] overflow-auto rounded-lg border border-border bg-surface p-4 font-mono text-sm text-text">
      {config ?? "Loading..."}
    </pre>
  );
}

function InfoCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-border bg-surface p-4">
      <p className="text-xs font-medium uppercase tracking-wider text-text-muted">
        {label}
      </p>
      <p className="mt-1 text-lg font-semibold">{value}</p>
    </div>
  );
}

function ActionButton({
  icon,
  label,
  onClick,
  disabled,
  variant = "default",
}: {
  icon: React.ReactNode;
  label: string;
  onClick: () => void;
  disabled: boolean;
  variant?: "default" | "danger";
}) {
  const base =
    "flex items-center gap-1.5 rounded-md px-3 py-1.5 text-sm font-medium transition-colors disabled:opacity-50";
  const styles =
    variant === "danger"
      ? "border border-red/30 text-red hover:bg-red/10"
      : "border border-border text-text hover:bg-surface-hover";

  return (
    <button onClick={onClick} disabled={disabled} className={`${base} ${styles}`}>
      {icon}
      {label}
    </button>
  );
}

function TabButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      onClick={onClick}
      className={`flex items-center px-4 py-2.5 text-sm font-medium transition-colors ${
        active
          ? "border-b-2 border-accent text-accent"
          : "text-text-muted hover:text-text"
      }`}
    >
      {children}
    </button>
  );
}
