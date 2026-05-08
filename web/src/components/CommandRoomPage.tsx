import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  AlertCircle,
  CheckCircle2,
  Circle,
  FileText,
  Network,
  RefreshCw,
  Shield,
} from "lucide-react";
import { fetchCommandRoom, fetchHierarchy } from "../lib/api";
import type {
  CommandRoom,
  CommandRoomBoardItem,
  CommandRoomRuntime,
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

function BoardItem({ item }: { item: CommandRoomBoardItem }) {
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

export default function CommandRoomPage() {
  const { backendDown, agents, projects, instances } = useData();
  const [room, setRoom] = useState<CommandRoom | null>(null);
  const roomRef = useRef<CommandRoom | null>(null);
  const [hierarchy, setHierarchy] = useState<HierarchyTree | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

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
    return built;
  }, [mesh, runtimes]);

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
          </svg>

          <div className="absolute left-5 top-5 grid grid-cols-4 gap-3">
            <Metric label="projects" value={String(projects.length || hierarchy?.projects.length || 0)} />
            <Metric label="agents" value={String(agents.length)} tone={agents.some((a) => a.alive) ? "green" : undefined} />
            <Metric label="open mesh" value={String(openNotices.length)} tone={openNotices.length ? "yellow" : "green"} />
            <Metric label="runtimes" value={String(runtimes.length)} />
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
              <FileText className="h-3.5 w-3.5 text-accent" />
              <h2 className="text-xs font-bold uppercase tracking-wider text-text">captain board</h2>
            </div>
            <div className="mt-3 grid gap-2">
              {activeBoardItems.length === 0 ? (
                <div className="text-xs text-text-muted">no active board items loaded</div>
              ) : activeBoardItems.slice(0, 5).map((item) => <BoardItem key={item.id} item={item} />)}
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
