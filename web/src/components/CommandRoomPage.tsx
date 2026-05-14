import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  AlertCircle,
  Bot,
  CheckCircle2,
  Circle,
  FileText,
  GitPullRequest,
  Network,
  RadioTower,
  RefreshCw,
  SendHorizontal,
  Shield,
  Square,
} from "lucide-react";
import { fetchCommandRoom, fetchHierarchy, streamCommandRoomDispatch, type CommandRoomDispatchEvent } from "../lib/api";
import type {
  AgentInfo,
  CommandRoom,
  CommandRoomBoardItem,
  CommandRoomDevelopmentNotice,
  CommandRoomDevelopmentWorkItem,
  CommandRoomRuntimeSmoke,
  CommandRoomRuntime,
  CommandRoomZeroClawAgent,
  HierarchyTree,
  MeshAgentSummary,
  MeshNoticeSummary,
} from "../lib/types";
import { useData } from "../lib/DataContext";

type NodeTone = "command" | "captain" | "runtime" | "docs" | "work";

interface MapNode {
  id: string;
  label: string;
  sub: string;
  tone: NodeTone;
  status?: string;
  x: number;
  y: number;
}

interface RealZeroClawAgent {
  id: string;
  name: string;
  display_name: string;
  status: string;
  live_status?: string;
  hierarchy_role?: string;
  project_id?: string;
  parent_id?: string;
  port: number;
  workspace_path?: string;
  config_path?: string;
  health_status?: string;
  provenances: string[];
  alive: boolean;
}

type DispatchStatus = "sending" | "done" | "error" | "cancelled";

interface BoardDispatchState {
  status: DispatchStatus;
  agentName: string;
  sessionKey?: string;
  responsePreview?: string;
  error?: string;
}

function isOpenStatus(status?: string): boolean {
  switch ((status || "").toLowerCase()) {
    case "":
    case "open":
    case "pending":
    case "must_handle":
      return true;
    case "answered":
    case "acknowledged":
    case "routed":
    case "sent":
    case "closed":
    case "done":
    case "complete":
    case "completed":
    case "superseded":
    case "stale":
    case "info-only":
    case "imported":
    case "cancelled":
    case "canceled":
      return false;
    default:
      return true;
  }
}

function shortPath(path?: string): string {
  if (!path) return "-";
  return path
    .replace("/Users/dan/Documents/Personal/EyrieOps/", "EyrieOps/")
    .replace("/Users/dan/Documents/Personal/Commander/", "Commander/")
    .replace("/Users/natalie/Development/eyrie/", "Eyrie/")
    .replace("/Users/natalie/Development/Codex/", "Development/");
}

function toneClass(tone: NodeTone): string {
  switch (tone) {
    case "command":
      return "border-purple/70 bg-purple/10 shadow-[0_0_28px_rgba(167,139,250,0.16)]";
    case "captain":
      return "border-accent/70 bg-accent/10 shadow-[0_0_26px_rgba(0,208,132,0.14)]";
    case "runtime":
      return "border-blue/60 bg-blue/10";
    case "docs":
      return "border-yellow/60 bg-yellow/10";
    default:
      return "border-border bg-surface";
  }
}

function statusDot(status?: string): string {
  if (isOpenStatus(status)) return "bg-yellow";
  if ((status || "").toLowerCase().includes("error")) return "bg-red";
  if ((status || "").toLowerCase().includes("configured") || (status || "").toLowerCase() === "active") return "bg-green";
  return "bg-text-muted";
}

function priorityTone(priority?: string): string {
  switch ((priority || "").toLowerCase()) {
    case "high":
    case "urgent":
    case "must_handle":
      return "border-red/40 bg-red/10 text-red";
    case "normal":
      return "border-border bg-surface text-text-muted";
    default:
      return "border-yellow/40 bg-yellow/10 text-yellow";
  }
}

function provenanceTone(provenance?: string): string {
  const value = (provenance || "").toLowerCase();
  if (value.includes("runtime")) return "border-blue/40 bg-blue/10 text-blue";
  if (value.includes("eyrie")) return "border-purple/40 bg-purple/10 text-purple";
  return "border-green/40 bg-green/10 text-green";
}

function ProvenanceBadge({ value }: { value?: string }) {
  if (!value) return null;
  return (
    <span className={`shrink-0 rounded border px-1.5 py-0.5 text-[8px] uppercase ${provenanceTone(value)}`}>
      {value}
    </span>
  );
}

function appendDispatchPreview(existing: string | undefined, next: string | undefined): string | undefined {
  if (!next) return existing;
  return `${existing || ""}${next}`.slice(-900);
}

function NodeCard({ node }: { node: MapNode }) {
  return (
    <div
      className={`absolute min-h-[88px] w-[172px] -translate-x-1/2 -translate-y-1/2 rounded border px-3 py-2.5 ${toneClass(node.tone)}`}
      style={{ left: `${node.x}%`, top: `${node.y}%` }}
    >
      <div className="flex items-center gap-2">
        <span className={`h-2 w-2 shrink-0 rounded-full ${statusDot(node.status)}`} />
        <div className="min-w-0">
          <div className="truncate text-[12px] font-bold text-text">{node.label}</div>
          <div className="truncate text-[9px] text-text-muted">{node.sub}</div>
        </div>
      </div>
      {node.status && (
        <div className="mt-2 inline-flex max-w-full rounded border border-border bg-bg/70 px-2 py-0.5 text-[9px] text-text-secondary">
          <span className="truncate">{node.status}</span>
        </div>
      )}
    </div>
  );
}

function Metric({ label, value, tone }: { label: string; value: string; tone?: "green" | "yellow" | "red" }) {
  const toneClassName = tone === "green" ? "text-green" : tone === "yellow" ? "text-yellow" : tone === "red" ? "text-red" : "text-text";
  return (
    <div className="rounded border border-border bg-surface/80 px-3 py-2">
      <div className="text-[9px] uppercase tracking-wider text-text-muted">{label}</div>
      <div className={`mt-1 text-lg font-bold ${toneClassName}`}>{value}</div>
    </div>
  );
}

function NoticeStrip({ notice }: { notice: MeshNoticeSummary }) {
  return (
    <div className="rounded border border-border bg-bg/70 px-3 py-2">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="truncate text-[11px] font-semibold text-text">{notice.title || notice.id}</div>
          <div className="mt-1 truncate text-[9px] text-text-muted">{notice.from || "local mesh"} | {notice.id}</div>
        </div>
        <span className={`shrink-0 rounded border px-1.5 py-0.5 text-[9px] ${priorityTone(notice.priority)}`}>
          {notice.priority || "open"}
        </span>
      </div>
    </div>
  );
}

function BoardItem({
  item,
  agents,
  selectedAgent,
  dispatch,
  onSelectAgent,
  onDispatch,
  onCancelDispatch,
}: {
  item: CommandRoomBoardItem;
  agents: RealZeroClawAgent[];
  selectedAgent: string;
  dispatch?: BoardDispatchState;
  onSelectAgent: (itemId: string, agentName: string) => void;
  onDispatch: (item: CommandRoomBoardItem, agentName: string) => void;
  onCancelDispatch: (itemId: string) => void;
}) {
  const isSending = dispatch?.status === "sending";
  return (
    <div className="rounded border border-border bg-bg/70 p-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="truncate text-[11px] font-bold text-text">{item.title}</div>
          <div className="mt-1 truncate text-[9px] text-text-muted">{item.owner || "-"} | {item.lane || "-"}</div>
        </div>
        <span className={`shrink-0 rounded border px-1.5 py-0.5 text-[9px] ${priorityTone(item.priority)}`}>
          {item.status || "active"}
        </span>
      </div>
      {item.next_action && <p className="mt-2 line-clamp-2 text-[10px] leading-relaxed text-text-secondary">{item.next_action}</p>}
      <div className="mt-3 flex items-center gap-2">
        <select
          value={selectedAgent}
          onChange={(event) => onSelectAgent(item.id, event.target.value)}
          disabled={agents.length === 0 || isSending}
          className="min-w-0 flex-1 rounded border border-border bg-bg px-2 py-1 text-[10px] text-text outline-none transition-colors focus:border-accent disabled:opacity-50"
        >
          {agents.length === 0 ? (
            <option value="">no running ZeroClaw</option>
          ) : agents.map((agent) => (
            <option key={agent.name} value={agent.name}>{agent.display_name || agent.name}</option>
          ))}
        </select>
        {isSending ? (
          <button
            type="button"
            onClick={() => onCancelDispatch(item.id)}
            className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded border border-yellow/40 bg-yellow/10 text-yellow transition-colors hover:bg-yellow/15"
            title="Cancel dispatch"
          >
            <Square className="h-3 w-3" />
          </button>
        ) : (
          <button
            type="button"
            onClick={() => onDispatch(item, selectedAgent)}
            disabled={!selectedAgent || agents.length === 0}
            className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded border border-accent/40 bg-accent/10 text-accent transition-colors hover:bg-accent/15 disabled:cursor-not-allowed disabled:opacity-40"
            title="Dispatch to agent"
          >
            <SendHorizontal className="h-3 w-3" />
          </button>
        )}
      </div>
      {dispatch && (
        <div className={`mt-2 rounded border px-2 py-1.5 text-[9px] leading-snug ${
          dispatch.status === "error"
            ? "border-red/30 bg-red/5 text-red"
            : dispatch.status === "done"
              ? "border-green/30 bg-green/5 text-green"
              : dispatch.status === "cancelled"
                ? "border-yellow/30 bg-yellow/5 text-yellow"
                : "border-blue/30 bg-blue/5 text-blue"
        }`}>
          <div className="flex items-center justify-between gap-2">
            <span className="truncate">{dispatch.status} | {dispatch.agentName}</span>
            {dispatch.sessionKey && <span className="truncate text-text-muted">{dispatch.sessionKey}</span>}
          </div>
          {(dispatch.responsePreview || dispatch.error) && (
            <div className="mt-1 line-clamp-3 text-text-secondary">{dispatch.error || dispatch.responsePreview}</div>
          )}
        </div>
      )}
    </div>
  );
}

function RuntimeRow({ runtime }: { runtime: CommandRoomRuntime }) {
  return (
    <div className="rounded border border-border bg-bg/70 px-3 py-2">
      <div className="flex items-center justify-between gap-3">
        <div className="min-w-0">
          <div className="truncate text-[11px] font-semibold text-text">{runtime.display_name || runtime.id}</div>
          <div className="mt-1 truncate text-[9px] text-text-muted">{runtime.parent_agent || "-"} | {runtime.framework || "-"}</div>
        </div>
        <span className="shrink-0 rounded border border-blue/30 bg-blue/10 px-1.5 py-0.5 text-[9px] text-blue">
          {runtime.transport || "file"}
        </span>
      </div>
      <div className="mt-2 truncate text-[9px] text-text-secondary">{runtime.status || "registered"}</div>
    </div>
  );
}

function DevelopmentAssignmentCard({ assignment }: { assignment: CommandRoomDevelopmentNotice }) {
  return (
    <div className="rounded border border-accent/30 bg-accent/5 p-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="truncate text-[11px] font-bold text-text">{assignment.title || assignment.id}</div>
          <div className="mt-1 truncate text-[9px] text-text-muted">{assignment.owner || assignment.from || "-"} | {assignment.worker || "-"}</div>
        </div>
        <span className={`shrink-0 rounded border px-1.5 py-0.5 text-[9px] ${priorityTone(assignment.priority)}`}>
          {assignment.status || "imported"}
        </span>
      </div>
      {assignment.summary && <p className="mt-2 line-clamp-3 text-[10px] leading-relaxed text-text-secondary">{assignment.summary}</p>}
      <div className="mt-2 flex flex-wrap items-center gap-1.5">
        <ProvenanceBadge value={assignment.provenance} />
        {assignment.response_path && (
          <span className="min-w-0 truncate rounded border border-border bg-bg/70 px-1.5 py-0.5 text-[8px] text-text-muted" title={assignment.response_path}>
            {shortPath(assignment.response_path)}
          </span>
        )}
      </div>
    </div>
  );
}

function DevelopmentWorkItemCard({ item }: { item: CommandRoomDevelopmentWorkItem }) {
  return (
    <div className="rounded border border-border bg-bg/70 p-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="truncate text-[11px] font-semibold text-text">{item.title || item.id}</div>
          <div className="mt-1 truncate text-[9px] text-text-muted">{item.owner || "-"} | {item.kind || item.lane || "work item"}</div>
        </div>
        <ProvenanceBadge value={item.provenance} />
      </div>
      {item.next_action && <p className="mt-2 line-clamp-2 text-[10px] leading-relaxed text-text-secondary">{item.next_action}</p>}
    </div>
  );
}

function RuntimeSmokeCard({ smoke }: { smoke: CommandRoomRuntimeSmoke }) {
  return (
    <div className="rounded border border-blue/30 bg-blue/5 p-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="truncate text-[11px] font-bold text-text">{smoke.title || smoke.id}</div>
          <div className="mt-1 truncate text-[9px] text-text-muted">{shortPath(smoke.source_path)}</div>
        </div>
        <span className={`shrink-0 rounded border px-1.5 py-0.5 text-[9px] ${smoke.status === "warning" ? "border-yellow/40 bg-yellow/10 text-yellow" : "border-green/40 bg-green/10 text-green"}`}>
          {smoke.status || "observed"}
        </span>
      </div>

      <div className="mt-3 grid gap-2">
        {smoke.facts.slice(0, 8).map((fact) => (
          <div key={`${fact.label}:${fact.value}`} className="rounded border border-border bg-bg/70 px-2 py-1.5">
            <div className="flex items-center justify-between gap-2">
              <span className="text-[8px] uppercase tracking-wider text-text-muted">{fact.label}</span>
              <ProvenanceBadge value={fact.provenance} />
            </div>
            <div className="mt-1 break-words text-[9px] leading-snug text-text-secondary">{shortPath(fact.value)}</div>
          </div>
        ))}
      </div>

      {smoke.findings && smoke.findings.length > 0 && (
        <div className="mt-3 border-t border-border pt-2">
          {smoke.findings.slice(0, 3).map((finding) => (
            <div key={finding} className="mt-1 text-[9px] leading-snug text-text-muted">{finding}</div>
          ))}
        </div>
      )}
    </div>
  );
}

function combineZeroClawAgents(metadata: CommandRoomZeroClawAgent[], liveAgents: AgentInfo[]): RealZeroClawAgent[] {
  const byName = new Map<string, RealZeroClawAgent>();
  for (const agent of metadata) {
    byName.set(agent.name, {
      id: agent.id || agent.name,
      name: agent.name,
      display_name: agent.display_name || agent.name,
      status: agent.status || "registered",
      hierarchy_role: agent.hierarchy_role,
      project_id: agent.project_id,
      parent_id: agent.parent_id,
      port: agent.port,
      workspace_path: agent.workspace_path,
      config_path: agent.config_path,
      health_status: agent.health_status,
      provenances: [agent.provenance || "Eyrie instance metadata"],
      alive: false,
    });
  }
  for (const live of liveAgents) {
    if (live.framework !== "zeroclaw") continue;
    const existing = byName.get(live.name);
    if (existing) {
      existing.alive = live.alive;
      existing.live_status = live.alive ? "running" : "offline";
      existing.port = live.port || existing.port;
      existing.display_name = live.display_name || existing.display_name;
      if (!existing.provenances.includes("runtime discovery")) existing.provenances.push("runtime discovery");
    } else {
      byName.set(live.name, {
        id: live.name,
        name: live.name,
        display_name: live.display_name || live.name,
        status: live.alive ? "running" : "discovered",
        live_status: live.alive ? "running" : "offline",
        port: live.port,
        provenances: ["runtime discovery"],
        alive: live.alive,
      });
    }
  }
  return Array.from(byName.values()).sort((left, right) => {
    if (left.alive !== right.alive) return left.alive ? -1 : 1;
    return left.name.localeCompare(right.name);
  });
}

function ZeroClawAgentCard({ agent }: { agent: RealZeroClawAgent }) {
  const visibleStatus = agent.live_status || agent.status || "registered";
  return (
    <div className="rounded border border-blue/30 bg-bg/70 p-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <span className={`h-2 w-2 shrink-0 rounded-full ${agent.alive ? "bg-green" : statusDot(visibleStatus)}`} />
            <div className="truncate text-[11px] font-bold text-text">{agent.display_name || agent.name}</div>
          </div>
          <div className="mt-1 truncate text-[9px] text-text-muted">
            {agent.hierarchy_role || "zeroclaw"} | {agent.project_id || "unassigned"} | :{agent.port || "-"}
          </div>
        </div>
        <span className={`shrink-0 rounded border px-1.5 py-0.5 text-[9px] ${agent.alive ? "border-green/40 bg-green/10 text-green" : "border-yellow/40 bg-yellow/10 text-yellow"}`}>
          {visibleStatus}
        </span>
      </div>
      <div className="mt-2 grid gap-1.5">
        {agent.workspace_path && (
          <div className="truncate text-[9px] text-text-secondary" title={agent.workspace_path}>
            workspace: {shortPath(agent.workspace_path)}
          </div>
        )}
        {agent.config_path && (
          <div className="truncate text-[9px] text-text-muted" title={agent.config_path}>
            config: {shortPath(agent.config_path)}
          </div>
        )}
      </div>
      <div className="mt-2 flex flex-wrap gap-1.5">
        {agent.provenances.map((provenance) => <ProvenanceBadge key={provenance} value={provenance} />)}
      </div>
    </div>
  );
}

export default function CommandRoomPage() {
  const { backendDown, agents, projects, instances } = useData();
  const [room, setRoom] = useState<CommandRoom | null>(null);
  const roomRef = useRef<CommandRoom | null>(null);
  const [hierarchy, setHierarchy] = useState<HierarchyTree | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [dispatchTargets, setDispatchTargets] = useState<Record<string, string>>({});
  const [dispatches, setDispatches] = useState<Record<string, BoardDispatchState>>({});
  const dispatchControllersRef = useRef<Record<string, AbortController>>({});

  const refresh = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const [roomData, hierarchyData] = await Promise.all([
        fetchCommandRoom(),
        fetchHierarchy(),
      ]);
      setRoom(roomData);
      roomRef.current = roomData;
      setHierarchy(hierarchyData);
    } catch (e) {
      const message = e instanceof Error ? e.message : "failed to load command room";
      if (roomRef.current === null) setError(message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (backendDown) return;
    refresh();
    const interval = setInterval(refresh, 15000);
    return () => clearInterval(interval);
  }, [backendDown, refresh]);

  const mesh = room?.mesh;
  const inboxes = mesh?.inboxes ?? [];
  const openNotices = useMemo(() => (
    inboxes.flatMap((inbox) => inbox.notices.filter((notice) => isOpenStatus(notice.status)))
  ), [inboxes]);
  const reports = mesh?.reports ?? [];
  const boardItems = room?.board?.items ?? [];
  const activeBoardItems = boardItems.filter((item) => isOpenStatus(item.status) || ["active", "waiting", "capture"].includes((item.status || "").toLowerCase()));
  const runtimes = room?.runtime_registry ?? [];
  const developmentMesh = room?.development_mesh;
  const developmentAssignments = developmentMesh?.assignments ?? [];
  const developmentWorkItems = developmentMesh?.work_items ?? [];
  const runtimeSmokes = developmentMesh?.runtime_smokes ?? [];
  const zeroClawAgents = useMemo(() => combineZeroClawAgents(room?.zeroclaw_agents ?? [], agents), [agents, room?.zeroclaw_agents]);
  const dispatchableZeroClawAgents = useMemo(() => zeroClawAgents.filter((agent) => agent.alive), [zeroClawAgents]);

  useEffect(() => {
    return () => {
      Object.values(dispatchControllersRef.current).forEach((controller) => controller.abort());
      dispatchControllersRef.current = {};
    };
  }, []);

  const handleSelectDispatchAgent = useCallback((itemId: string, agentName: string) => {
    setDispatchTargets((prev) => ({ ...prev, [itemId]: agentName }));
  }, []);

  const handleCancelDispatch = useCallback((itemId: string) => {
    dispatchControllersRef.current[itemId]?.abort();
    delete dispatchControllersRef.current[itemId];
    setDispatches((prev) => {
      const existing = prev[itemId];
      if (!existing || existing.status !== "sending") return prev;
      return {
        ...prev,
        [itemId]: {
          ...existing,
          status: "cancelled",
        },
      };
    });
  }, []);

  const handleDispatchBoardItem = useCallback((item: CommandRoomBoardItem, agentName: string) => {
    if (!agentName) return;
    dispatchControllersRef.current[item.id]?.abort();
    setDispatches((prev) => ({
      ...prev,
      [item.id]: {
        status: "sending",
        agentName,
      },
    }));

    const controller = streamCommandRoomDispatch(agentName, item, "", (event: CommandRoomDispatchEvent) => {
      setDispatches((prev) => {
        const current = prev[item.id] ?? { status: "sending", agentName };
        if (event.type === "dispatch") {
          return {
            ...prev,
            [item.id]: {
              ...current,
              status: "sending",
              agentName: event.agent,
              sessionKey: event.session_key,
            },
          };
        }
        if (event.type === "delta") {
          return {
            ...prev,
            [item.id]: {
              ...current,
              status: "sending",
              responsePreview: appendDispatchPreview(current.responsePreview, event.content),
            },
          };
        }
        if (event.type === "tool_start") {
          return {
            ...prev,
            [item.id]: {
              ...current,
              status: "sending",
              responsePreview: appendDispatchPreview(current.responsePreview, `\n[tool] ${event.tool || "tool"}`),
            },
          };
        }
        if (event.type === "done") {
          delete dispatchControllersRef.current[item.id];
          return {
            ...prev,
            [item.id]: {
              ...current,
              status: "done",
              responsePreview: event.content || current.responsePreview || "response complete",
            },
          };
        }
        if (event.type === "error") {
          delete dispatchControllersRef.current[item.id];
          return {
            ...prev,
            [item.id]: {
              ...current,
              status: "error",
              error: event.error || "dispatch failed",
            },
          };
        }
        return prev;
      });
    });
    dispatchControllersRef.current[item.id] = controller;
  }, []);

  const nodes = useMemo<MapNode[]>(() => {
    const built: MapNode[] = [
      { id: "vega", label: "Vega", sub: "system command", tone: "command", status: "command", x: 50, y: 12 },
      {
        id: "magnus",
        label: mesh?.parent_agent?.display_name || "Magnus",
        sub: mesh?.parent_agent?.role || "Eyrie captain",
        tone: "captain",
        status: mesh?.status || "mesh",
        x: 50,
        y: 33,
      },
    ];
    const subordinates = mesh?.subordinates ?? [];
    const positions = [
      { x: 22, y: 58 },
      { x: 50, y: 64 },
      { x: 78, y: 58 },
    ];
    subordinates.slice(0, 3).forEach((agent: MeshAgentSummary, index) => {
      built.push({
        id: agent.id,
        label: agent.display_name || agent.id,
        sub: agent.role || agent.planned_framework,
        tone: agent.id.includes("docs") || agent.role.includes("documentation") ? "docs" : agent.planned_framework === "hermes" ? "runtime" : "work",
        status: agent.planned_framework,
        x: positions[index].x,
        y: positions[index].y,
      });
    });
    runtimes.slice(0, 2).forEach((runtime, index) => {
      built.push({
        id: runtime.id,
        label: runtime.display_name || runtime.id,
        sub: runtime.role || runtime.framework,
        tone: "runtime",
        status: runtime.status,
        x: index === 0 ? 30 : 70,
        y: 84,
      });
    });
    if (developmentMesh) {
      built.push({
        id: "development-mesh",
        label: "Development",
        sub: developmentMesh.scope || "external mesh",
        tone: "work",
        status: developmentMesh.status,
        x: 84,
        y: 27,
      });
    }
    if (runtimeSmokes.length > 0) {
      built.push({
        id: "runtime-smoke",
        label: "PR #6398 smoke",
        sub: runtimeSmokes[0].status || "observed",
        tone: "runtime",
        status: runtimeSmokes[0].status,
        x: 84,
        y: 74,
      });
    }
    if (zeroClawAgents.length > 0) {
      built.push({
        id: "real-zeroclaw-agents",
        label: "ZeroClaw",
        sub: `${zeroClawAgents.length} real agent${zeroClawAgents.length === 1 ? "" : "s"}`,
        tone: "runtime",
        status: zeroClawAgents.some((agent) => agent.alive) ? "running" : "registered",
        x: 16,
        y: 76,
      });
    }
    return built;
  }, [developmentMesh, mesh, runtimeSmokes, runtimes, zeroClawAgents]);

  if (loading && !room) {
    return <div className="flex h-full items-center justify-center text-xs text-text-muted">loading command room...</div>;
  }

  if (error) {
    return (
      <div className="p-6">
        <div className="rounded border border-red/30 bg-red/5 px-4 py-3 text-xs text-red">{error}</div>
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col bg-bg">
      <div className="flex items-center justify-between border-b border-border px-5 py-3">
        <div>
          <h1 className="text-sm font-bold text-text"><span className="text-accent">&gt;</span> command room</h1>
          <div className="mt-1 text-[10px] text-text-muted">{shortPath(mesh?.root)} | {room?.generated_at || "-"}</div>
        </div>
        <button
          onClick={() => refresh()}
          disabled={loading}
          className="flex items-center gap-1.5 text-xs text-text-muted transition-colors hover:text-text disabled:opacity-50"
        >
          <RefreshCw className={`h-3 w-3 ${loading ? "animate-spin" : ""}`} />
          refresh
        </button>
      </div>

      <div className="grid min-h-0 flex-1 grid-cols-[1fr_360px] overflow-hidden">
        <div className="relative overflow-hidden border-r border-border">
          <div className="absolute inset-0 opacity-30 [background-image:linear-gradient(var(--color-border)_1px,transparent_1px),linear-gradient(90deg,var(--color-border)_1px,transparent_1px)] [background-size:42px_42px]" />
          <svg className="absolute inset-0 h-full w-full" viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">
            <path d="M50 16 C50 22 50 27 50 31" stroke="var(--color-purple)" strokeWidth="0.25" fill="none" />
            <path d="M50 38 C40 47 30 52 22 56" stroke="var(--color-accent)" strokeWidth="0.22" fill="none" />
            <path d="M50 38 C50 48 50 55 50 61" stroke="var(--color-accent)" strokeWidth="0.22" fill="none" />
            <path d="M50 38 C60 47 70 52 78 56" stroke="var(--color-accent)" strokeWidth="0.22" fill="none" />
            <path d="M50 70 C43 76 36 80 30 83" stroke="var(--color-blue)" strokeWidth="0.18" fill="none" strokeDasharray="1 1" />
            <path d="M50 70 C57 76 64 80 70 83" stroke="var(--color-blue)" strokeWidth="0.18" fill="none" strokeDasharray="1 1" />
            {developmentMesh && <path d="M58 34 C67 29 75 27 84 27" stroke="var(--color-yellow)" strokeWidth="0.2" fill="none" strokeDasharray="0.8 0.8" />}
            {runtimeSmokes.length > 0 && <path d="M84 35 C86 48 86 61 84 70" stroke="var(--color-blue)" strokeWidth="0.18" fill="none" strokeDasharray="0.8 0.8" />}
            {zeroClawAgents.length > 0 && <path d="M30 83 C24 82 19 80 16 77" stroke="var(--color-blue)" strokeWidth="0.18" fill="none" strokeDasharray="0.8 0.8" />}
          </svg>

          <div className="absolute left-5 top-5 grid grid-cols-5 gap-3">
            <Metric label="projects" value={String(projects.length || hierarchy?.projects.length || 0)} />
            <Metric label="agents" value={String(agents.length)} tone={agents.some((a) => a.alive) ? "green" : undefined} />
            <Metric label="open mesh" value={String(openNotices.length)} tone={openNotices.length ? "yellow" : "green"} />
            <Metric label="runtimes" value={String(runtimes.length)} />
            <Metric label="zeroclaw" value={String(zeroClawAgents.length)} tone={zeroClawAgents.some((agent) => agent.alive) ? "green" : undefined} />
          </div>

          <div className="absolute bottom-5 left-5 right-5 grid grid-cols-4 gap-3">
            {(room?.data_sources ?? []).map((source) => (
              <div key={`${source.label}:${source.path}`} className="rounded border border-border bg-surface/85 px-3 py-2">
                <div className="flex items-center gap-2">
                  {source.status === "available" ? <CheckCircle2 className="h-3 w-3 text-green" /> : <AlertCircle className="h-3 w-3 text-yellow" />}
                  <span className="text-[10px] font-semibold uppercase tracking-wider text-text">{source.label}</span>
                </div>
                <div className="mt-1 truncate text-[9px] text-text-muted" title={source.path}>{shortPath(source.path)}</div>
              </div>
            ))}
          </div>

          {nodes.map((node) => <NodeCard key={node.id} node={node} />)}
        </div>

        <aside className="min-h-0 overflow-y-auto bg-bg-sidebar">
          <section className="border-b border-border p-4">
            <div className="flex items-center gap-2">
              <Shield className="h-3.5 w-3.5 text-accent" />
              <h2 className="text-xs font-bold uppercase tracking-wider text-text">approval boundary</h2>
            </div>
            <div className="mt-3 grid gap-2">
              {(room?.approval_boundary ?? []).map((item) => (
                <div key={item} className="flex items-center gap-2 text-[10px] text-text-secondary">
                  <Circle className="h-2 w-2 text-accent" />
                  <span>{item}</span>
                </div>
              ))}
            </div>
          </section>

          <section className="border-b border-border p-4">
            <div className="flex items-center gap-2">
              <Network className="h-3.5 w-3.5 text-accent" />
              <h2 className="text-xs font-bold uppercase tracking-wider text-text">open mesh traffic</h2>
            </div>
            <div className="mt-3 grid gap-2">
              {openNotices.length === 0 ? (
                <div className="text-xs text-text-muted">no open local mesh requests</div>
              ) : openNotices.slice(0, 5).map((notice) => <NoticeStrip key={notice.id} notice={notice} />)}
            </div>
          </section>

          <section className="border-b border-border p-4">
            <div className="flex items-center gap-2">
              <GitPullRequest className="h-3.5 w-3.5 text-accent" />
              <h2 className="text-xs font-bold uppercase tracking-wider text-text">development import</h2>
            </div>
            <div className="mt-1 truncate text-[9px] text-text-muted">{developmentMesh?.scope || "zeroclaw-labs/zeroclaw#6398"} | {shortPath(developmentMesh?.root)}</div>
            <div className="mt-3 grid gap-2">
              {developmentAssignments.length === 0 && developmentWorkItems.length === 0 ? (
                <div className="text-xs text-text-muted">no scoped Development mesh items loaded</div>
              ) : (
                <>
                  {developmentAssignments.slice(0, 2).map((assignment) => <DevelopmentAssignmentCard key={assignment.id} assignment={assignment} />)}
                  {developmentWorkItems.slice(0, 2).map((item) => <DevelopmentWorkItemCard key={item.id} item={item} />)}
                </>
              )}
            </div>
          </section>

          <section className="border-b border-border p-4">
            <div className="flex items-center gap-2">
              <RadioTower className="h-3.5 w-3.5 text-blue" />
              <h2 className="text-xs font-bold uppercase tracking-wider text-text">runtime smoke</h2>
            </div>
            <div className="mt-3 grid gap-2">
              {runtimeSmokes.length === 0 ? (
                <div className="text-xs text-text-muted">no scoped runtime smoke loaded</div>
              ) : runtimeSmokes.map((smoke) => <RuntimeSmokeCard key={smoke.id} smoke={smoke} />)}
            </div>
          </section>

          <section className="border-b border-border p-4">
            <div className="flex items-center gap-2">
              <Bot className="h-3.5 w-3.5 text-blue" />
              <h2 className="text-xs font-bold uppercase tracking-wider text-text">zeroclaw agents</h2>
            </div>
            <div className="mt-3 grid gap-2">
              {zeroClawAgents.length === 0 ? (
                <div className="text-xs text-text-muted">no real ZeroClaw agents discovered or provisioned</div>
              ) : zeroClawAgents.map((agent) => <ZeroClawAgentCard key={agent.id} agent={agent} />)}
            </div>
          </section>

          <section className="border-b border-border p-4">
            <div className="flex items-center gap-2">
              <FileText className="h-3.5 w-3.5 text-accent" />
              <h2 className="text-xs font-bold uppercase tracking-wider text-text">captain board</h2>
            </div>
            <div className="mt-3 grid gap-2">
              {activeBoardItems.length === 0 ? (
                <div className="text-xs text-text-muted">no active board items loaded</div>
              ) : activeBoardItems.slice(0, 5).map((item) => {
                const selectedAgent = dispatchTargets[item.id] || dispatchableZeroClawAgents[0]?.name || "";
                return (
                  <BoardItem
                    key={item.id}
                    item={item}
                    agents={dispatchableZeroClawAgents}
                    selectedAgent={selectedAgent}
                    dispatch={dispatches[item.id]}
                    onSelectAgent={handleSelectDispatchAgent}
                    onDispatch={handleDispatchBoardItem}
                    onCancelDispatch={handleCancelDispatch}
                  />
                );
              })}
            </div>
          </section>

          <section className="p-4">
            <div className="flex items-center gap-2">
              <Network className="h-3.5 w-3.5 text-accent" />
              <h2 className="text-xs font-bold uppercase tracking-wider text-text">runtime registry</h2>
            </div>
            <div className="mt-3 grid gap-2">
              {runtimes.length === 0 ? (
                <div className="text-xs text-text-muted">no runtime registry entries loaded</div>
              ) : runtimes.map((runtime) => <RuntimeRow key={runtime.id} runtime={runtime} />)}
            </div>
          </section>

          <section className="p-4 pt-0">
            <div className="rounded border border-border bg-bg/70 px-3 py-2 text-[10px] text-text-muted">
              {instances.length} provisioned instance{instances.length === 1 ? "" : "s"} | {reports.length} local report{reports.length === 1 ? "" : "s"}
            </div>
          </section>
        </aside>
      </div>
    </div>
  );
}
