import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  AlertCircle,
  CheckCircle2,
  Circle,
  FileText,
  Inbox,
  Link2,
  Network,
  RefreshCw,
  Send,
} from "lucide-react";
import type { MeshInboxSummary, MeshNoticeSummary, MeshStatus } from "../lib/types";
import { fetchMeshStatus } from "../lib/api";
import { useData } from "../lib/DataContext";

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
    .replace("/Users/natalie/Development/eyrie/", "")
    .replace(/^.*\/EyrieOps\//, "EyrieOps/")
    .replace("/Users/dan/Documents/Personal/Commander/", "Commander/");
}

function statusDot(status?: string): string {
  if (isOpenStatus(status)) return "bg-yellow";
  if ((status || "").toLowerCase() === "answered" || (status || "").toLowerCase() === "sent") return "bg-green";
  return "bg-text-muted";
}

function statusBadge(status?: string): string {
  if (isOpenStatus(status)) return "border-yellow/30 bg-yellow/10 text-yellow";
  if ((status || "").toLowerCase() === "answered" || (status || "").toLowerCase() === "sent") {
    return "border-green/30 bg-green/10 text-green";
  }
  return "border-border bg-surface text-text-muted";
}

function Metric({ label, value, sub, tone }: {
  label: string;
  value: string;
  sub?: string;
  tone?: "green" | "yellow" | "red";
}) {
  const valueClass = tone === "green" ? "text-green" : tone === "yellow" ? "text-yellow" : tone === "red" ? "text-red" : "text-text";
  return (
    <div className="rounded border border-border bg-surface p-4">
      <div className="text-[10px] font-medium uppercase tracking-wider text-text-muted">{label}</div>
      <div className={`mt-1.5 text-xl font-bold ${valueClass}`}>{value}</div>
      {sub && <div className="mt-1 text-[10px] text-text-muted">{sub}</div>}
    </div>
  );
}

function NoticeRow({ notice, inbox }: { notice: MeshNoticeSummary; inbox?: MeshInboxSummary }) {
  return (
    <div className="rounded border border-border bg-bg p-3">
      <div className="flex min-w-0 items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${statusDot(notice.status)}`} />
            <span className="truncate text-xs font-semibold text-text">{notice.title || notice.id}</span>
          </div>
          <div className="mt-1 flex flex-wrap items-center gap-2 text-[10px] text-text-muted">
            <span>{notice.id}</span>
            {inbox && <span>| {inbox.recipient}</span>}
            {notice.from && <span>| from {notice.from}</span>}
          </div>
        </div>
        <span className={`shrink-0 rounded border px-2 py-0.5 text-[10px] font-medium ${statusBadge(notice.status)}`}>
          {notice.status || "open"}
        </span>
      </div>
      {notice.summary && <p className="mt-2 text-xs leading-relaxed text-text-secondary">{notice.summary}</p>}
      {notice.response && (
        <div className="mt-2 truncate rounded border border-border bg-surface px-2 py-1 text-[10px] text-text-muted" title={notice.response}>
          response: {shortPath(notice.response)}
        </div>
      )}
    </div>
  );
}

export default function MeshStatusPage() {
  const { backendDown } = useData();
  const [mesh, setMesh] = useState<MeshStatus | null>(null);
  const meshRef = useRef<MeshStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await fetchMeshStatus();
      setMesh(data);
      meshRef.current = data;
    } catch (e) {
      const msg = e instanceof Error ? e.message : "failed to load mesh status";
      if (meshRef.current === null) setError(msg);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (backendDown) return;
    refresh();
    const interval = setInterval(refresh, 15000);
    return () => clearInterval(interval);
  }, [refresh, backendDown]);

  const inboxes = mesh?.inboxes ?? [];
  const openNotices = useMemo(() => (
    inboxes.flatMap((inbox) =>
      inbox.notices.filter((notice) => isOpenStatus(notice.status)).map((notice) => ({ inbox, notice })),
    )
  ), [inboxes]);
  const totalNotices = inboxes.reduce((sum, inbox) => sum + inbox.total, 0);
  const pendingAcks = inboxes.reduce((sum, inbox) => sum + inbox.pending_acknowledgements, 0);
  const reports = mesh?.reports ?? [];
  const commanderRefs = mesh?.commander_refs ?? [];

  if (loading && !mesh) {
    return <div className="py-20 text-center text-xs text-text-muted">loading mesh...</div>;
  }

  if (error) {
    return (
      <div className="space-y-4">
        <div className="rounded border border-red/30 bg-red/5 px-4 py-3 text-xs text-red">{error}</div>
        <button onClick={refresh} disabled={loading} className="flex items-center gap-1.5 text-xs text-text-muted hover:text-text disabled:opacity-50">
          <RefreshCw className={`h-3 w-3 ${loading ? "animate-spin" : ""}`} />
          retry
        </button>
      </div>
    );
  }

  if (!mesh?.available) {
    return (
      <div className="space-y-6">
        <Header loading={loading} onRefresh={refresh} />
        <div className="rounded border border-yellow/30 bg-yellow/5 p-4 text-xs text-yellow">
          {mesh?.unavailable_text || "local mesh is not configured"}
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <Header loading={loading} onRefresh={refresh} />

      <div className="grid gap-4 md:grid-cols-4">
        <Metric label="open requests" value={String(openNotices.length)} tone={openNotices.length > 0 ? "yellow" : "green"} sub={`${totalNotices} notices`} />
        <Metric label="pending acks" value={String(pendingAcks)} tone={pendingAcks > 0 ? "yellow" : undefined} />
        <Metric label="reports" value={String(reports.length)} />
        <Metric label="status" value={mesh.status || "unknown"} tone={mesh.status === "provisional" ? "yellow" : undefined} sub={mesh.updated} />
      </div>

      <section className="rounded border border-border bg-surface">
        <div className="flex items-center gap-2 border-b border-border px-4 py-3">
          <Network className="h-3.5 w-3.5 text-accent" />
          <h2 className="text-xs font-bold uppercase tracking-wider text-text">mesh manifest</h2>
        </div>
        <div className="grid gap-4 p-4 md:grid-cols-2">
          <div>
            <div className="text-[10px] uppercase tracking-wider text-text-muted">owner</div>
            <div className="mt-1 text-sm font-semibold text-text">{mesh.owner || "-"}</div>
            <div className="mt-2 truncate text-[10px] text-text-muted" title={mesh.root}>{shortPath(mesh.root)}</div>
          </div>
          <div>
            <div className="text-[10px] uppercase tracking-wider text-text-muted">parent</div>
            <div className="mt-1 flex items-center gap-2 text-sm font-semibold text-text">
              <span>{mesh.parent_agent?.display_name || mesh.parent_agent?.id || "-"}</span>
              {mesh.parent_agent?.planned_framework && (
                <span className="rounded border border-border bg-bg px-2 py-0.5 text-[10px] font-medium text-text-muted">
                  {mesh.parent_agent.planned_framework}
                </span>
              )}
            </div>
            <div className="mt-2 text-[10px] text-text-muted">{mesh.parent_agent?.role || "-"}</div>
          </div>
        </div>
        {(mesh.subordinates ?? []).length > 0 && (
          <div className="border-t border-border px-4 py-3">
            <div className="mb-2 text-[10px] uppercase tracking-wider text-text-muted">subordinates</div>
            <div className="grid gap-2 md:grid-cols-2">
              {(mesh.subordinates ?? []).map((agent) => (
                <div key={agent.id} className="rounded border border-border bg-bg px-3 py-2">
                  <div className="flex items-center justify-between gap-2">
                    <span className="truncate text-xs font-medium text-text">{agent.display_name || agent.id}</span>
                    <span className="shrink-0 text-[10px] text-text-muted">{agent.planned_framework}</span>
                  </div>
                  <div className="mt-1 text-[10px] text-text-muted">{agent.role}</div>
                </div>
              ))}
            </div>
          </div>
        )}
      </section>

      <section className="grid gap-4 lg:grid-cols-[1.2fr_0.8fr]">
        <div className="rounded border border-border bg-surface">
          <div className="flex items-center gap-2 border-b border-border px-4 py-3">
            <Inbox className="h-3.5 w-3.5 text-accent" />
            <h2 className="text-xs font-bold uppercase tracking-wider text-text">open inbox requests</h2>
          </div>
          <div className="space-y-3 p-4">
            {openNotices.length === 0 ? (
              <div className="flex items-center gap-2 text-xs text-text-muted">
                <CheckCircle2 className="h-3.5 w-3.5 text-green" />
                no open requests
              </div>
            ) : openNotices.map(({ inbox, notice }) => (
              <NoticeRow key={`${inbox.recipient}:${notice.id}`} inbox={inbox} notice={notice} />
            ))}
          </div>
        </div>

        <div className="space-y-4">
          <section className="rounded border border-border bg-surface">
            <div className="flex items-center gap-2 border-b border-border px-4 py-3">
              <Send className="h-3.5 w-3.5 text-accent" />
              <h2 className="text-xs font-bold uppercase tracking-wider text-text">latest outbox</h2>
            </div>
            <div className="p-4">
              {mesh.latest_outbox ? (
                <NoticeRow notice={mesh.latest_outbox} />
              ) : (
                <div className="text-xs text-text-muted">no outbox entries</div>
              )}
            </div>
          </section>

          <section className="rounded border border-border bg-surface">
            <div className="flex items-center gap-2 border-b border-border px-4 py-3">
              <Circle className="h-3.5 w-3.5 text-accent" />
              <h2 className="text-xs font-bold uppercase tracking-wider text-text">inboxes</h2>
            </div>
            <div className="divide-y divide-border">
              {inboxes.map((inbox) => (
                <div key={inbox.path} className="flex items-center justify-between gap-3 px-4 py-3 text-xs">
                  <div className="min-w-0">
                    <div className="font-medium text-text">{inbox.recipient}</div>
                    <div className="mt-0.5 truncate text-[10px] text-text-muted" title={inbox.path}>{shortPath(inbox.path)}</div>
                  </div>
                  <div className="shrink-0 text-right text-[10px] text-text-muted">
                    <div>{inbox.open} open</div>
                    <div>{inbox.total} total</div>
                  </div>
                </div>
              ))}
            </div>
          </section>
        </div>
      </section>

      <section className="grid gap-4 lg:grid-cols-2">
        <div className="rounded border border-border bg-surface">
          <div className="flex items-center gap-2 border-b border-border px-4 py-3">
            <FileText className="h-3.5 w-3.5 text-accent" />
            <h2 className="text-xs font-bold uppercase tracking-wider text-text">reports</h2>
          </div>
          <div className="divide-y divide-border">
            {reports.length === 0 ? (
              <div className="px-4 py-3 text-xs text-text-muted">no reports</div>
            ) : reports.map((report) => (
              <div key={report.path} className="px-4 py-3">
                <div className="truncate text-xs font-medium text-text">{report.title}</div>
                <div className="mt-1 truncate text-[10px] text-text-muted" title={report.path}>{shortPath(report.path)}</div>
              </div>
            ))}
          </div>
        </div>

        <div className="rounded border border-border bg-surface">
          <div className="flex items-center gap-2 border-b border-border px-4 py-3">
            <Link2 className="h-3.5 w-3.5 text-accent" />
            <h2 className="text-xs font-bold uppercase tracking-wider text-text">Commander refs</h2>
          </div>
          <div className="divide-y divide-border">
            {commanderRefs.length === 0 ? (
              <div className="px-4 py-3 text-xs text-text-muted">no Commander notice refs</div>
            ) : commanderRefs.map((ref) => (
              <div key={`${ref.path}:${ref.notice || ""}`} className="px-4 py-3">
                <div className="truncate text-xs font-medium text-text">{ref.notice || shortPath(ref.path)}</div>
                <div className="mt-1 truncate text-[10px] text-text-muted" title={ref.path}>{shortPath(ref.path)}</div>
              </div>
            ))}
          </div>
        </div>
      </section>
    </div>
  );
}

function Header({ loading, onRefresh }: { loading: boolean; onRefresh: () => void }) {
  return (
    <div className="flex items-center justify-between">
      <div>
        <h1 className="text-xl font-bold"><span className="text-accent">&gt;</span> mesh_status</h1>
        <p className="mt-1 flex items-center gap-1.5 text-xs text-text-muted">
          <AlertCircle className="h-3 w-3" />
          read-only local mesh
        </p>
      </div>
      <button
        onClick={onRefresh}
        disabled={loading}
        className="flex items-center gap-1.5 rounded border border-border px-3 py-1.5 text-xs text-text-muted transition-colors hover:text-text disabled:opacity-50"
      >
        <RefreshCw className={`h-3 w-3 ${loading ? "animate-spin" : ""}`} />
        refresh
      </button>
    </div>
  );
}
