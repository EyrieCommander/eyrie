import { useEffect, useState, useCallback, useRef } from "react";
import { useParams, Link } from "react-router-dom";
import {
  Play,
  Square,
  RotateCcw,
} from "lucide-react";
import type {
  AgentInfo,
  LogEntry,
  ActivityEvent,
} from "../lib/types";
import {
  agentAction,
  fetchAgentConfig,
  streamLogs,
  streamActivity,
} from "../lib/api";

interface AgentDetailProps {
  agent: AgentInfo;
}

const validTabs = ["overview", "activity", "logs", "config"] as const;
type Tab = (typeof validTabs)[number];

export default function AgentDetail({ agent }: AgentDetailProps) {
  const { tab: tabParam } = useParams<{ tab?: string }>();

  const tab: Tab = validTabs.includes(tabParam as Tab)
    ? (tabParam as Tab)
    : "overview";

  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [activity, setActivity] = useState<ActivityEvent[]>([]);
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
      setLogs([]);
      const close = streamLogs(agent.name, (entry) => {
        setLogs((prev) => [...prev.slice(-200), entry]);
      });
      return close;
    }
  }, [tab, agent.name, agent.alive]);

  useEffect(() => {
    if (tab === "activity" && agent.alive) {
      setActivity([]);
      const close = streamActivity(agent.name, (event) => {
        setActivity((prev) => [...prev.slice(-200), event]);
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
        <Link
          to="/agents/overview"
          className="text-xs text-text-muted transition-colors hover:text-text"
        >
          &lt; back
        </Link>

        <div className="flex gap-2">
          {!agent.alive ? (
            <ActionButton
              icon={<Play className="h-3.5 w-3.5" />}
              label="start"
              onClick={() => handleAction("start")}
              disabled={actionPending}
            />
          ) : (
            <>
              <ActionButton
                icon={<RotateCcw className="h-3.5 w-3.5" />}
                label="restart"
                onClick={() => handleAction("restart")}
                disabled={actionPending}
              />
              <ActionButton
                icon={<Square className="h-3.5 w-3.5" />}
                label="stop"
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
          <span
            className={`h-3 w-3 rounded-full ${agent.alive ? "bg-green" : "bg-red"}`}
          />
          <h2 className="text-xl font-bold">{agent.name}</h2>
          <span className="rounded border border-border-strong bg-surface-hover px-2 py-0.5 text-[11px] text-text-secondary">
            {agent.framework}
          </span>
        </div>
        <p className="mt-1 text-xs text-text-muted">
          // gateway: {agent.host}:{agent.port}
        </p>
      </div>

      <div className="flex border-b border-border">
        {validTabs.map((t) => (
          <TabLink
            key={t}
            to={`/agents/${agent.name}/${t}`}
            active={tab === t}
          >
            {t}
          </TabLink>
        ))}
      </div>

      {tab === "overview" && <OverviewTab agent={agent} />}
      {tab === "activity" && (
        <ActivityTab events={activity} alive={agent.alive} framework={agent.framework} />
      )}
      {tab === "logs" && <LogsTab logs={logs} alive={agent.alive} />}
      {tab === "config" && <ConfigTab config={config} alive={agent.alive} />}
    </div>
  );
}

function OverviewTab({ agent }: { agent: AgentInfo }) {
  const health = agent.health;
  const status = agent.status;

  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <InfoCard
          label="STATUS"
          value={agent.alive ? "running" : "stopped"}
          highlight={agent.alive}
        />
        <InfoCard label="PID" value={health?.pid?.toString() ?? "-"} />
        <InfoCard
          label="UPTIME"
          value={
            health?.uptime
              ? `${Math.floor(health.uptime / 3600)}h ${Math.floor((health.uptime % 3600) / 60)}m`
              : "-"
          }
        />
        <InfoCard label="PROVIDER" value={status?.provider ?? "-"} />
        <InfoCard label="MODEL" value={status?.model ?? "-"} />
        <InfoCard
          label="CHANNELS"
          value={status?.channels?.join(", ") || "-"}
        />
      </div>

      {health?.components && Object.keys(health.components).length > 0 && (
        <div>
          <h3 className="mb-2 text-[10px] font-medium uppercase tracking-wider text-text-muted">
            Components
          </h3>
          <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
            {Object.entries(health.components).map(([name, comp]) => (
              <div
                key={name}
                className="rounded border border-border bg-surface p-3"
              >
                <div className="flex items-center justify-between">
                  <span className="text-xs font-medium">{name}</span>
                  <span
                    className={`text-[10px] ${comp.status === "ok" ? "text-green" : "text-red"}`}
                  >
                    {comp.status}
                  </span>
                </div>
                {comp.restart_count > 0 && (
                  <p className="mt-1 text-[10px] text-yellow">
                    restarts: {comp.restart_count}
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
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [logs.length]);

  if (!alive) {
    return (
      <p className="text-xs text-text-muted">
        Agent is not running. Start it to see logs.
      </p>
    );
  }

  return (
    <div
      ref={scrollRef}
      className="max-h-[calc(100vh-320px)] overflow-y-auto rounded border border-border bg-surface p-4 text-xs"
    >
      {logs.length === 0 ? (
        <p className="text-text-muted">Waiting for log entries...</p>
      ) : (
        logs.map((entry, i) => (
          <div key={i} className="py-0.5">
            <span className="text-text-muted">
              {new Date(entry.timestamp).toLocaleTimeString()}
            </span>{" "}
            <span
              className={`font-medium ${
                entry.level === "error"
                  ? "text-red"
                  : entry.level === "warn"
                    ? "text-yellow"
                    : "text-green"
              }`}
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
      <p className="text-xs text-text-muted">
        Agent is not running. Start it to view config.
      </p>
    );
  }

  return (
    <pre className="max-h-[calc(100vh-320px)] overflow-auto rounded border border-border bg-surface p-5 text-xs text-text leading-relaxed">
      {config ?? "Loading..."}
    </pre>
  );
}

const activityTypeColors: Record<string, string> = {
  agent_start: "text-green",
  agent_end: "text-green",
  tool_call: "text-accent",
  tool_call_start: "text-accent",
  llm_request: "text-yellow",
  error: "text-red",
  chat: "text-purple",
};

function ActivityTab({
  events,
  alive,
  framework,
}: {
  events: ActivityEvent[];
  alive: boolean;
  framework: string;
}) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const [expandedIndex, setExpandedIndex] = useState<number | null>(null);

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [events.length]);

  if (!alive) {
    return (
      <p className="text-xs text-text-muted">
        Agent is not running. Start it to see activity.
      </p>
    );
  }

  const hasOnlyUserChat =
    events.length > 0 &&
    events.some((e) => e.type === "chat" && e.summary.startsWith("[user]")) &&
    !events.some((e) => e.type === "chat" && e.summary.startsWith("[assistant]"));

  return (
    <div className="space-y-3">
      <div
        ref={scrollRef}
        className="max-h-[calc(100vh-320px)] overflow-y-auto rounded border border-border bg-surface p-4 text-xs"
      >
        {events.length === 0 ? (
          <p className="text-text-muted">
            Waiting for activity events... Conversation history and live events
            will appear here.
          </p>
        ) : (
          events.map((event, i) =>
            event.type === "separator" ? (
              <SeparatorRow key={i} label={event.summary} />
            ) : event.type === "chat" ? (
              <ChatEventRow
                key={i}
                event={event}
                expanded={expandedIndex === i}
                onToggle={() => setExpandedIndex(expandedIndex === i ? null : i)}
              />
            ) : (
              <div key={i} className="py-0.5">
                <span className="text-text-muted">
                  {new Date(event.timestamp).toLocaleTimeString()}
                </span>{" "}
                <span
                  className={`font-medium ${activityTypeColors[event.type] ?? "text-text-muted"}`}
                >
                  [{event.type}]
                </span>{" "}
                <span className="text-text">{event.summary}</span>
              </div>
            ),
          )
        )}
      </div>
      {hasOnlyUserChat && framework === "zeroclaw" && (
        <p className="text-[10px] text-text-muted">
          Only user messages are shown. To see bot replies, ensure{" "}
          <code className="rounded bg-surface-hover px-1">memory.auto_save = true</code>{" "}
          in the ZeroClaw config and restart the agent.
        </p>
      )}
    </div>
  );
}

function SeparatorRow({ label }: { label: string }) {
  return (
    <div className="flex items-center gap-3 py-2">
      <div className="h-px flex-1 bg-border" />
      <span className="text-[10px] text-text-muted whitespace-nowrap">{label}</span>
      <div className="h-px flex-1 bg-border" />
    </div>
  );
}

function ChatEventRow({
  event,
  expanded,
  onToggle,
}: {
  event: ActivityEvent;
  expanded: boolean;
  onToggle: () => void;
}) {
  const summary = event.summary;
  const isUser = summary.startsWith("[user]");
  const isAssistant = summary.startsWith("[assistant]");
  const truncatedContent = summary.replace(/^\[(user|assistant)\]\s*/, "");
  const hasFullContent = !!event.full_content;

  const fullText = event.full_content
    ? event.full_content.replace(/^\[(user|assistant)\]\s*/, "")
    : truncatedContent;

  return (
    <div
      className={`py-0.5 ${hasFullContent ? "cursor-pointer hover:bg-surface-hover/50 rounded px-1 -mx-1" : ""}`}
      onClick={hasFullContent ? onToggle : undefined}
    >
      <div>
        <span className="text-text-muted">
          {new Date(event.timestamp).toLocaleTimeString()}
        </span>{" "}
        <span
          className={`font-medium ${isUser ? "text-green" : isAssistant ? "text-purple" : "text-text-muted"}`}
        >
          {isUser ? "user:" : isAssistant ? "assistant:" : "chat:"}
        </span>{" "}
        <span className="text-text">
          {expanded ? fullText : truncatedContent}
        </span>
        {hasFullContent && !expanded && (
          <span className="ml-1 text-text-muted">▸</span>
        )}
      </div>
      {expanded && (
        <div className="mt-1 whitespace-pre-wrap border-l-2 border-border pl-3 text-text-muted text-[11px] leading-relaxed">
          {fullText}
        </div>
      )}
    </div>
  );
}

function InfoCard({
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
        className={`mt-1.5 text-lg font-semibold ${highlight ? "text-accent" : "text-text"}`}
      >
        {value}
      </p>
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
    "flex items-center gap-1.5 rounded px-3 py-1.5 text-xs font-medium transition-colors disabled:opacity-50";
  const styles =
    variant === "danger"
      ? "border border-red/30 text-red hover:bg-red/10"
      : "border border-border text-text-secondary hover:bg-surface-hover hover:text-text";

  return (
    <button onClick={onClick} disabled={disabled} className={`${base} ${styles}`}>
      {icon}
      $ {label}
    </button>
  );
}

function TabLink({
  to,
  active,
  children,
}: {
  to: string;
  active: boolean;
  children: React.ReactNode;
}) {
  return (
    <Link
      to={to}
      replace
      className={`px-5 py-2.5 text-xs font-medium transition-colors ${
        active
          ? "border-b-2 border-accent text-accent"
          : "text-text-secondary hover:text-text"
      }`}
    >
      {children}
    </Link>
  );
}
