// ProjectDetail.tsx — Project workspace with live split view.
//
// WHY always-mount for ProjectChat:
//   ProjectChat is ALWAYS rendered (never conditionally unmounted). Setup
//   prompts (assign captain) render as absolute overlays
//   ON TOP of the chat, not as replacements. This preserves:
//   - Active SSE streaming connections (AbortController, event handlers)
//   - In-flight optimistic messages
//   - Scroll position and message history state
//   If ProjectChat were conditionally rendered, any parent re-render that
//   briefly removed it would kill the SSE connection and drop events (the
//   mountedRef anti-pattern — see ProjectChat.tsx header comment).
//
// WHY chatKey increment for reset:
//   When the user clicks "reset project", we clear the chat on the server and
//   increment `chatKey`. React uses the key to force a clean remount of
//   ProjectChat — fresh state, new SSE connection, no stale messages. This
//   avoids a full page reload while ensuring clean state.
//
// WHY overlays instead of conditional rendering:
//   Setup prompts (no captain) use absolute positioning to
//   overlay the chat area. This means the chat component stays mounted
//   underneath. When the user completes setup, the overlay disappears and
//   the chat is immediately ready — no mount delay, no lost state.

import { useState, useEffect, useCallback, useRef } from "react";
import { useParams, useNavigate } from "react-router-dom";
import {
  ArrowLeft, Plus, Trash2, Briefcase, Crown,
  MessageSquare, Pause, Target, ListTodo, Network,
} from "lucide-react";
import type { AgentInstance, ReviewTask, ReviewTaskKind, ReviewArtifact } from "../lib/types";
import { deleteProject, resetProject, agentAction, instanceAction, createReviewTask, fetchReviewTasks, runReviewTask, fetchReviewTaskArtifacts } from "../lib/api";
import { useData } from "../lib/DataContext";
import { SetCaptainDialog } from "./SetCaptainDialog";
import { AddAgentDialog } from "./AddAgentDialog";
import { ProjectChat } from "./ProjectChat";
import { ProjectHierarchy } from "./ProjectHierarchy";

// Status dot color based on instance status
function statusDotClass(status: string): string {
  if (status === "created" || status === "provisioning" || status === "starting")
    return "bg-yellow-400 animate-pulse";
  if (status === "running") return "bg-green";
  if (status === "error") return "bg-red";
  return "bg-text-muted";
}

function AgentCard({
  instance,
  onClick,
}: {
  instance: AgentInstance;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      className="flex w-full items-center gap-2.5 rounded border border-border bg-transparent px-3 py-2.5 text-left text-xs transition-all hover:border-accent/30 hover:bg-surface-hover/50"
    >
      <span className={`h-1.5 w-1.5 flex-shrink-0 rounded-full ${statusDotClass(instance.status)}`} />
      <div className="flex-1 min-w-0">
        <div className="font-medium text-text truncate">{instance.display_name || instance.name}</div>
        <div className="text-text-muted truncate">
          {instance.framework} · :{instance.port}
        </div>
      </div>
      <MessageSquare className="h-3 w-3 flex-shrink-0 text-purple-400 opacity-50 hover:opacity-100" />
    </button>
  );
}

export default function ProjectDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { agents, projects: ctxProjects, instances: ctxInstances, loading: ctxLoading, refresh: ctxRefresh, backendDown } = useData();
  const [showAddAgent, setShowAddAgent] = useState(false);
  const [showSetOrchestrator, setShowSetOrchestrator] = useState(false);
  const [loadError, setLoadError] = useState("");
  const [startingAgent, setStartingAgent] = useState("");
  const [chatKey, setChatKey] = useState(0); // increment to remount ProjectChat
  const [activeTab, setActiveTab] = useState<"chat" | "tasks" | "hierarchy">("chat");
  const [reviewTasks, setReviewTasks] = useState<ReviewTask[]>([]);
  const [reviewKind, setReviewKind] = useState<ReviewTaskKind>("review_pr");
  const [reviewRepo, setReviewRepo] = useState("zeroclaw-labs/zeroclaw");
  const [reviewTarget, setReviewTarget] = useState(1);
  const [selectedTaskID, setSelectedTaskID] = useState("");
  const [selectedArtifacts, setSelectedArtifacts] = useState<ReviewArtifact[]>([]);
  const [viewedArtifactIdx, setViewedArtifactIdx] = useState<number>(-1);
  const hasLoadedRef = useRef(false);
  const pollRef = useRef<{ interval: ReturnType<typeof setInterval> | null; timeout: ReturnType<typeof setTimeout> | null }>({ interval: null, timeout: null });

  // Derive project and instances from context
  const project = ctxProjects.find((p) => p.id === id) ?? null;
  const instances = project
    ? ctxInstances.filter(
        (inst) =>
          inst.project_id === id ||
          project.orchestrator_id === inst.id ||
          project.role_agent_ids?.includes(inst.id),
      )
    : [];
  const loading = ctxLoading && !hasLoadedRef.current;

  // Commander is always Eyrie (built-in) — no agent lookup needed.

  const refresh = useCallback(async () => {
    if (!id) return;
    try {
      setLoadError("");
      await ctxRefresh();
      hasLoadedRef.current = true;
    } catch (err) {
      console.error("Failed to load project data:", err);
      setLoadError(err instanceof Error ? err.message : "Failed to load project");
    }
  }, [id, ctxRefresh]);

  useEffect(() => { refresh(); }, [refresh]);

  const refreshReviewTasks = useCallback(async () => {
    if (!id) return;
    try {
      const tasks = await fetchReviewTasks(id);
      setReviewTasks(tasks);
      if (!selectedTaskID && tasks.length > 0) {
        setSelectedTaskID(tasks[0].id);
      }
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : "Failed to load review tasks");
    }
  }, [id, selectedTaskID]);

  useEffect(() => {
    refreshReviewTasks();
  }, [refreshReviewTasks]);

  useEffect(() => {
    if (!selectedTaskID) return;
    fetchReviewTaskArtifacts(selectedTaskID)
      .then((arts) => {
        setSelectedArtifacts(arts);
        setViewedArtifactIdx(arts.length > 0 ? arts.length - 1 : -1);
      })
      .catch((err) => setLoadError(err instanceof Error ? err.message : "Failed to load artifacts"));
  }, [selectedTaskID]);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (pollRef.current.interval) clearInterval(pollRef.current.interval);
      if (pollRef.current.timeout) clearTimeout(pollRef.current.timeout);
    };
  }, []);

  // Poll while any instance is provisioning
  useEffect(() => {
    const hasProvisioning = instances.some((i) => i.status === "created" || i.status === "provisioning" || i.status === "starting");
    if (!hasProvisioning) return;
    const interval = setInterval(refresh, 3000);
    return () => clearInterval(interval);
  }, [instances, refresh]);

  // Subscribe to project events for real-time updates.
  // Only refresh the agent roster, NOT the full context (which would
  // cause ProjectChat to unmount/remount and lose its state).
  // WHY backendDown pause: When the backend is down, every EventSource
  // attempt generates a browser-level ERR_CONNECTION_REFUSED that can't
  // be caught. Pausing reconnection eliminates the noise; DataContext
  // will re-enable when it successfully polls again.
  useEffect(() => {
    if (!id || backendDown) return;
    let cancelled = false;
    let reconnectDelay = 2000; // 2s initial, doubles up to 30s
    let timeoutId: ReturnType<typeof setTimeout>;
    let es: EventSource;

    const connect = () => {
      if (cancelled) return;
      es = new EventSource(`/api/projects/${encodeURIComponent(id)}/events`);
      es.onmessage = () => {
        reconnectDelay = 2000; // reset backoff on success
        refresh();
      };
      es.onerror = () => {
        es.close(); // stop browser's aggressive ~3s auto-reconnect
        if (!cancelled) {
          timeoutId = setTimeout(connect, reconnectDelay);
          reconnectDelay = Math.min(reconnectDelay * 2, 30000);
        }
      };
    };
    connect();

    return () => {
      cancelled = true;
      clearTimeout(timeoutId);
      es?.close();
    };
  }, [id, refresh, backendDown]);

  if (loading && !project) {
    return <div className="py-20 text-center text-xs text-text-muted">loading project...</div>;
  }
  if (!project) {
    return <div className="py-20 text-center text-xs text-text-muted">project not found</div>;
  }

  const captainInstance = instances.find((i) => i.id === project.orchestrator_id);
  const captainAgent = !captainInstance ? agents.find((a) => a.name === project.orchestrator_id) : null;
  const hasCaptain = captainInstance || captainAgent;
  const roleAgents = instances.filter((i) => i.hierarchy_role === "talon");

  // Helpers for starting stopped agents
  const startAgent = async (agentId: string, isInstance: boolean) => {
    setStartingAgent(agentId);
    try {
      if (isInstance) await instanceAction(agentId, "start");
      else await agentAction(agentId, "start");
      // Clear any prior interval AND timeout before scheduling new ones
      // so an older timer can't fire mid-poll and stop the new one.
      if (pollRef.current.interval) clearInterval(pollRef.current.interval);
      if (pollRef.current.timeout) clearTimeout(pollRef.current.timeout);
      // Poll until the agent shows as running
      const poll = setInterval(refresh, 2000);
      pollRef.current.interval = poll;
      pollRef.current.timeout = setTimeout(() => {
        clearInterval(poll);
        pollRef.current.timeout = null;
        setStartingAgent("");
      }, 30000);
    } catch (e) {
      setLoadError(e instanceof Error ? e.message : "failed to start agent");
      setStartingAgent("");
    }
  };

  // Check if required agents are stopped (only after initial load)
  const needsStart: { name: string; role: string; isInstance: boolean; id: string }[] = [];
  // WHY no commander check: Commander is a system-level agent, not required
  // for project chat. Only the captain and talons need to be running.
  // Skip transient states ("starting", "created", "provisioning") so we
  // don't prompt the user to start a captain that's already booting —
  // mirrors the talon loop logic below.
  if (
    captainInstance &&
    captainInstance.status !== "running" &&
    captainInstance.status !== "starting" &&
    captainInstance.status !== "created" &&
    captainInstance.status !== "provisioning"
  ) {
    needsStart.push({ name: captainInstance.display_name || captainInstance.name, role: "captain", isInstance: true, id: captainInstance.id });
  }
  if (captainAgent && !captainAgent.alive) {
    needsStart.push({ name: captainAgent.name, role: "captain", isInstance: false, id: captainAgent.name });
  }
  for (const talon of roleAgents) {
    // Skip "starting" — the talon was just created and is booting up.
    // Only show talons that are definitively stopped or errored.
    if (talon.status !== "running" && talon.status !== "starting" && talon.status !== "created" && talon.status !== "provisioning") {
      needsStart.push({ name: talon.display_name || talon.name, role: "talon", isInstance: true, id: talon.id });
    }
  }

  return (
    <div className="flex h-full flex-col">
      {/* Compact header */}
      <div className="flex items-center gap-3 border-b border-border px-4 py-3">
        <button
          onClick={() => navigate("/projects")}
          className="rounded p-1 text-text-muted transition-colors hover:bg-surface-hover hover:text-text"
        >
          <ArrowLeft className="h-4 w-4" />
        </button>
        <Briefcase className="h-4 w-4 text-green" />
        <h1 className="text-sm font-bold text-text">{project.name}</h1>
        <span className="rounded bg-green/10 px-1.5 py-0.5 text-[10px] font-medium text-green">
          {project.status}
        </span>
        {project.goal && (
          <span className="ml-2 flex items-center gap-1 text-[11px] text-green">
            <Target className="h-3 w-3" /> {project.goal}
          </span>
        )}
        <div className="flex-1" />
        <button
          onClick={async () => {
            if (confirm("delete this project?")) {
              try {
                await deleteProject(project.id);
                await ctxRefresh(false);
                navigate("/projects");
              } catch (e) {
                setLoadError(e instanceof Error ? e.message : "Failed to delete");
              }
            }
          }}
          className="rounded p-1.5 text-text-muted transition-colors hover:bg-red/10 hover:text-red"
          title="delete project"
        >
          <Trash2 className="h-3.5 w-3.5" />
        </button>
      </div>

      {loadError && (
        <div className="border-b border-red/30 bg-red/5 px-4 py-2 text-xs text-red">{loadError}</div>
      )}

      {/* Split workspace: sidebar + chat */}
      <div className="flex flex-1 overflow-hidden">
        {/* Agent roster sidebar */}
        <div className="flex w-[260px] flex-shrink-0 flex-col gap-2.5 overflow-y-auto border-r border-border bg-surface px-3 py-3">
          {/* Project info */}
          {project.description && (
            <p className="text-[11px] text-text-muted">{project.description}</p>
          )}

          {/* Progress */}
          {(project as any).progress > 0 && (
            <div className="space-y-1">
              <div className="flex items-center justify-between text-[10px]">
                <span className="font-medium text-text-muted">// progress</span>
                <span className="font-semibold text-green">{(project as any).progress}%</span>
              </div>
              <div className="h-1 w-full rounded-full bg-border">
                <div
                  className="h-1 rounded-full bg-green transition-all"
                  style={{ width: `${(project as any).progress}%` }}
                />
              </div>
            </div>
          )}

          <div className="h-px w-full bg-border" />

          {/* Commander — always Eyrie (built-in) */}
          <div>
            <div className="mb-2">
              <span className="text-[10px] font-medium text-text-muted">// commander</span>
            </div>
            <div className="flex items-center gap-2.5 rounded border border-border px-3 py-2.5 text-xs">
              <span className="h-1.5 w-1.5 flex-shrink-0 rounded-full bg-green" />
              <div className="flex-1 min-w-0">
                <div className="font-medium text-text">Eyrie</div>
                <div className="text-text-muted">built-in commander</div>
              </div>
            </div>
          </div>

          <div className="h-px w-full bg-border" />

          {/* Captain */}
          <div>
            <div className="mb-2 flex items-center justify-between">
              <span className="text-[10px] font-medium text-text-muted">// captain</span>
              {!hasCaptain && (
                <button
                  onClick={() => setShowSetOrchestrator(true)}
                  className="flex items-center gap-1 text-[10px] text-accent hover:text-accent/80"
                >
                  <Crown className="h-2.5 w-2.5" /> assign
                </button>
              )}
            </div>
            {captainInstance ? (
              <AgentCard
                instance={captainInstance}
                onClick={() => navigate(`/agents/${captainInstance.name}/chat`)}
              />
            ) : captainAgent ? (
              <button
                onClick={() => navigate(`/agents/${captainAgent.name}`)}
                className="flex w-full items-center gap-2.5 rounded border border-border px-3 py-2.5 text-left text-xs hover:bg-surface-hover/50"
              >
                <span className={`h-1.5 w-1.5 rounded-full ${captainAgent.alive ? "bg-green" : "bg-text-muted"}`} />
                <div className="flex-1">
                  <div className="font-medium text-text">{captainAgent.display_name || captainAgent.name}</div>
                  <div className="text-text-muted">{captainAgent.framework} · :{captainAgent.port}</div>
                </div>
              </button>
            ) : (
              <div className="rounded border border-dashed border-border px-3 py-3 text-center text-[10px] text-text-muted">
                no captain assigned
              </div>
            )}
          </div>

          <div className="h-px w-full bg-border" />

          {/* Talons */}
          <div>
            <div className="mb-2 flex items-center justify-between">
              <span className="text-[10px] font-medium text-text-muted">// talons ({roleAgents.length})</span>
              <button
                onClick={() => setShowAddAgent(true)}
                className="flex items-center gap-1 text-[10px] text-accent hover:text-accent/80"
              >
                <Plus className="h-2.5 w-2.5" /> add
              </button>
            </div>
            {roleAgents.length === 0 ? (
              <div className="rounded border border-dashed border-border px-3 py-3 text-center text-[10px] text-text-muted">
                no talons yet
              </div>
            ) : (
              <div className="space-y-1.5">
                {roleAgents.map((agent) => (
                  <AgentCard
                    key={agent.id}
                    instance={agent}
                    onClick={() => navigate(`/agents/${agent.name}/chat`)}
                  />
                ))}
              </div>
            )}
          </div>

          <div className="h-px w-full bg-border" />

          {/* Actions */}
          <div className="space-y-2">
            <span className="text-[10px] font-medium text-text-muted">// actions</span>
            <div className="flex gap-2">
              <button className="flex flex-1 items-center justify-center gap-1.5 rounded border border-border px-2 py-1.5 text-[10px] text-text-muted hover:bg-surface-hover">
                <Pause className="h-3 w-3" /> pause
              </button>
              <button
                disabled
                className="flex flex-1 items-center justify-center gap-1.5 rounded bg-accent px-2 py-1.5 text-[10px] font-medium text-white opacity-50 cursor-not-allowed"
                title="coming soon"
              >
                <Target className="h-3 w-3" /> review
              </button>
            </div>
            <button
              onClick={async () => {
                const talonCount = roleAgents.length;
                const msg = talonCount > 0
                  ? `reset project? this will clear chat, reset agent sessions, and destroy ${talonCount} talon${talonCount !== 1 ? "s" : ""}.`
                  : "reset project chat and all agent sessions?";
                if (!confirm(msg)) return;
                try {
                  await resetProject(project.id);
                  await ctxRefresh(false);
                  setChatKey((k) => k + 1); // remount ProjectChat with fresh state
                } catch (e) {
                  setLoadError(e instanceof Error ? e.message : "reset failed");
                }
              }}
              className="flex w-full items-center justify-center gap-1.5 rounded border border-red/30 px-2 py-1.5 text-[10px] text-red/70 hover:bg-red/10 hover:text-red"
            >
              <Trash2 className="h-3 w-3" /> reset project
            </button>
          </div>
        </div>

        {/* Main workspace area — tabbed layout */}
        <div className="relative flex flex-1 flex-col overflow-hidden">
          {/* Setup overlays — commander is NOT required for project chat */}
          {hasLoadedRef.current && !hasCaptain && (
            <div className="absolute inset-0 z-10 flex items-center justify-center bg-bg/90">
              <div className="text-center space-y-3">
                <p className="text-xs text-text-muted">assign a captain to start</p>
                <button onClick={() => setShowSetOrchestrator(true)} className="rounded bg-accent px-4 py-2 text-xs font-medium text-white hover:bg-accent/80">
                  assign captain
                </button>
              </div>
            </div>
          )}
          {hasLoadedRef.current && hasCaptain && needsStart.length > 0 && (
            <div className="absolute inset-0 z-10 flex items-center justify-center bg-bg/90">
              <div className="text-center space-y-4">
                <p className="text-xs text-text-muted">agents need to be running</p>
                <div className="flex flex-col items-center gap-2">
                  {needsStart.map((a) => (
                    <div key={a.id} className="flex items-center gap-3 rounded border border-border px-4 py-2 text-xs">
                      <span className={`h-1.5 w-1.5 rounded-full ${startingAgent === a.id ? "bg-yellow-400 animate-pulse" : "bg-text-muted"}`} />
                      <span className="font-medium text-text">{a.name}</span>
                      <span className="text-text-muted">{a.role}</span>
                      <button
                        disabled={!!startingAgent}
                        onClick={() => startAgent(a.id, a.isInstance)}
                        className="rounded bg-green px-2 py-0.5 text-[10px] font-medium text-white hover:bg-green/80 disabled:opacity-50"
                      >
                        {startingAgent === a.id ? "starting..." : "start"}
                      </button>
                    </div>
                  ))}
                </div>
                {needsStart.length > 1 && (
                  <button
                    disabled={!!startingAgent}
                    onClick={async () => {
                      setStartingAgent("all");
                      const results = await Promise.allSettled(
                        needsStart.map((a) =>
                          a.isInstance ? instanceAction(a.id, "start") : agentAction(a.id, "start"),
                        ),
                      );
                      const failures = results
                        .map((r, i) => ({ r, a: needsStart[i] }))
                        .filter(({ r }) => r.status === "rejected");
                      if (failures.length > 0) {
                        const msg = failures
                          .map(({ r, a }) => {
                            const reason = (r as PromiseRejectedResult).reason;
                            const txt = reason instanceof Error ? reason.message : String(reason);
                            return `${a.name}: ${txt}`;
                          })
                          .join("; ");
                        setLoadError(`failed to start ${failures.length}/${needsStart.length} agent${failures.length === 1 ? "" : "s"}: ${msg}`);
                      }
                      if (failures.length === needsStart.length) {
                        setStartingAgent("");
                        return;
                      }
                      if (pollRef.current.interval) clearInterval(pollRef.current.interval);
                      if (pollRef.current.timeout) clearTimeout(pollRef.current.timeout);
                      const poll = setInterval(refresh, 2000);
                      pollRef.current.interval = poll;
                      pollRef.current.timeout = setTimeout(() => {
                        clearInterval(poll);
                        pollRef.current.timeout = null;
                        setStartingAgent("");
                      }, 30000);
                    }}
                    className="rounded bg-accent px-4 py-1.5 text-xs font-medium text-white hover:bg-accent/80 disabled:opacity-50"
                  >
                    start all
                  </button>
                )}
              </div>
            </div>
          )}

          {/* Tab bar */}
          <div className="flex items-center gap-1 border-b border-border bg-surface px-3 py-1">
            <button
              onClick={() => setActiveTab("chat")}
              className={`flex items-center gap-1.5 rounded-t px-3 py-1.5 text-[11px] font-medium transition-colors ${activeTab === "chat" ? "border-b-2 border-accent text-accent" : "text-text-muted hover:text-text"}`}
            >
              <MessageSquare className="h-3 w-3" /> Chat
            </button>
            <button
              onClick={() => setActiveTab("tasks")}
              className={`flex items-center gap-1.5 rounded-t px-3 py-1.5 text-[11px] font-medium transition-colors ${activeTab === "tasks" ? "border-b-2 border-accent text-accent" : "text-text-muted hover:text-text"}`}
            >
              <ListTodo className="h-3 w-3" /> Tasks
            </button>
            <button
              onClick={() => setActiveTab("hierarchy")}
              className={`flex items-center gap-1.5 rounded-t px-3 py-1.5 text-[11px] font-medium transition-colors ${activeTab === "hierarchy" ? "border-b-2 border-accent text-accent" : "text-text-muted hover:text-text"}`}
            >
              <Network className="h-3 w-3" /> Hierarchy
            </button>
          </div>

          {/* Tab content — Chat is ALWAYS mounted (CSS-hidden when inactive)
              to preserve SSE connections and streaming state. */}
          <div className="relative flex-1 overflow-hidden">
            {/* Chat tab — always mounted, hidden via CSS */}
            <div
              className="absolute inset-0 flex flex-col"
              style={{ visibility: activeTab === "chat" ? "visible" : "hidden" }}
            >
              <ProjectChat
                key={chatKey}
                projectId={project.id}
                participants={[
                  ...(captainInstance ? [{ name: captainInstance.name, role: "captain" }] : []),
                  ...(captainAgent ? [{ name: captainAgent.name, role: "captain" }] : []),
                  ...roleAgents.map((a) => ({ name: a.name, role: "talon" })),
                ]}
              />
            </div>

            {/* Tasks tab */}
            {activeTab === "tasks" && (
              <div className="absolute inset-0 overflow-y-auto p-4">
                <div className="mx-auto max-w-3xl space-y-4">
                  <div className="flex items-center justify-between">
                    <h2 className="text-sm font-bold text-text">Review Tasks</h2>
                  </div>

                  {/* Create task form */}
                  <div className="rounded border border-border bg-surface p-3 space-y-2">
                    <span className="text-[10px] font-medium text-text-muted">// create task</span>
                    <div className="flex gap-2">
                      <select
                        value={reviewKind}
                        onChange={(e) => setReviewKind(e.target.value as ReviewTaskKind)}
                        className="rounded border border-border bg-bg px-2 py-1 text-[11px] text-text"
                      >
                        <option value="triage_issue">triage_issue</option>
                        <option value="review_pr">review_pr</option>
                        <option value="rereview_pr">rereview_pr</option>
                        <option value="respond_reviewer">respond_reviewer</option>
                      </select>
                      <input
                        value={reviewRepo}
                        onChange={(e) => setReviewRepo(e.target.value)}
                        placeholder="owner/repo"
                        className="flex-1 rounded border border-border bg-bg px-2 py-1 text-[11px] text-text"
                      />
                      <input
                        type="number"
                        min={1}
                        value={reviewTarget}
                        onChange={(e) => setReviewTarget(Number(e.target.value))}
                        placeholder="#"
                        className="w-20 rounded border border-border bg-bg px-2 py-1 text-[11px] text-text"
                      />
                      <button
                        onClick={async () => {
                          if (!id) return;
                          try {
                            const created = await createReviewTask({
                              project_id: id,
                              domain: "github",
                              kind: reviewKind,
                              repo: reviewRepo,
                              target_number: reviewTarget,
                            });
                            setSelectedTaskID(created.id);
                            await refreshReviewTasks();
                          } catch (err) {
                            setLoadError(err instanceof Error ? err.message : "Failed to create review task");
                          }
                        }}
                        className="rounded bg-accent px-3 py-1 text-[11px] font-medium text-white hover:bg-accent/80"
                      >
                        create
                      </button>
                    </div>
                  </div>

                  {/* Task list */}
                  <div className="space-y-1">
                    {reviewTasks.length === 0 ? (
                      <div className="rounded border border-dashed border-border px-4 py-6 text-center text-xs text-text-muted">
                        no tasks yet — create one above
                      </div>
                    ) : (
                      reviewTasks.map((task) => (
                        <button
                          key={task.id}
                          onClick={() => setSelectedTaskID(task.id)}
                          className={`w-full rounded border px-3 py-2 text-left text-xs transition-colors ${selectedTaskID === task.id ? "border-accent/50 bg-accent/5" : "border-border hover:bg-surface-hover/50"}`}
                        >
                          <div className="flex items-center justify-between">
                            <span className="font-medium text-text">{task.kind} #{task.target_number}</span>
                            <span className={`rounded px-1.5 py-0.5 text-[10px] ${task.status === "draft_ready" ? "bg-green/10 text-green" : task.status === "running" ? "bg-yellow-400/10 text-yellow-400" : task.status === "failed" ? "bg-red/10 text-red" : "bg-surface-hover text-text-muted"}`}>
                              {task.status}
                            </span>
                          </div>
                          <div className="text-text-muted">{task.repo}</div>
                        </button>
                      ))
                    )}
                  </div>

                  {/* Selected task details */}
                  {selectedTaskID && (
                    <div className="rounded border border-border bg-surface p-3 space-y-3">
                      <div className="flex items-center justify-between">
                        <span className="text-[10px] font-medium text-text-muted">// selected task</span>
                        <button
                          onClick={async () => {
                            try {
                              await runReviewTask(selectedTaskID);
                              await refreshReviewTasks();
                              const arts = await fetchReviewTaskArtifacts(selectedTaskID);
                              setSelectedArtifacts(arts);
                              setViewedArtifactIdx(arts.length > 0 ? arts.length - 1 : -1);
                            } catch (err) {
                              setLoadError(err instanceof Error ? err.message : "Failed to run task");
                            }
                          }}
                          className="rounded border border-border px-2 py-1 text-[10px] text-text-muted hover:bg-surface-hover"
                        >
                          run task
                        </button>
                      </div>

                      {/* Artifact selector */}
                      {selectedArtifacts.length > 0 && (
                        <div className="space-y-2">
                          <div className="flex gap-1">
                            {selectedArtifacts.map((art, idx) => (
                              <button
                                key={art.id}
                                onClick={() => setViewedArtifactIdx(idx)}
                                className={`rounded px-2 py-1 text-[10px] ${viewedArtifactIdx === idx ? "bg-accent/20 text-accent font-medium" : "bg-surface-hover text-text-muted hover:text-text"}`}
                              >
                                {art.kind === "source_context" ? "context" : art.kind === "task_result" ? "result" : "draft"}
                              </button>
                            ))}
                          </div>

                          {/* TaskResult summary for task_result artifacts */}
                          {viewedArtifactIdx >= 0 && viewedArtifactIdx < selectedArtifacts.length && selectedArtifacts[viewedArtifactIdx].kind === "task_result" && (() => {
                            try {
                              const tr = JSON.parse(selectedArtifacts[viewedArtifactIdx].content);
                              return (
                                <div className="rounded border border-border bg-bg p-3 space-y-2 text-xs">
                                  <div className="flex items-center gap-2">
                                    <span className="font-medium text-text">{tr.summary}</span>
                                    <span className={`rounded px-1.5 py-0.5 text-[10px] ${tr.severity === "critical" ? "bg-red/10 text-red" : tr.severity === "high" ? "bg-orange-400/10 text-orange-400" : tr.severity === "medium" ? "bg-yellow-400/10 text-yellow-400" : "bg-surface-hover text-text-muted"}`}>
                                      {tr.severity}
                                    </span>
                                    <span className="text-text-muted text-[10px]">
                                      confidence: {Math.round((tr.confidence || 0) * 100)}%
                                    </span>
                                  </div>
                                  {tr.requires_human_review && (
                                    <div className="text-[10px] text-yellow-400">requires human review</div>
                                  )}
                                  {tr.proposed_actions && tr.proposed_actions.length > 0 && (
                                    <div className="space-y-1">
                                      <span className="text-[10px] text-text-muted">proposed actions:</span>
                                      {tr.proposed_actions.map((a: any, i: number) => (
                                        <div key={i} className="flex items-center gap-2 rounded bg-surface-hover px-2 py-1 text-[10px]">
                                          <span className="font-mono text-accent">{a.kind}</span>
                                          <span className="text-text-muted">{a.description}</span>
                                        </div>
                                      ))}
                                    </div>
                                  )}
                                  {tr.notes && (
                                    <div className="text-[10px] text-text-muted">{tr.notes}</div>
                                  )}
                                </div>
                              );
                            } catch {
                              return null;
                            }
                          })()}

                          {/* Raw artifact content for non-task_result types */}
                          {viewedArtifactIdx >= 0 && viewedArtifactIdx < selectedArtifacts.length && selectedArtifacts[viewedArtifactIdx].kind !== "task_result" && (
                            <pre className="max-h-96 overflow-auto whitespace-pre-wrap rounded border border-border bg-bg p-3 text-[11px] text-text-muted">
                              {selectedArtifacts[viewedArtifactIdx].content}
                            </pre>
                          )}
                        </div>
                      )}
                    </div>
                  )}
                </div>
              </div>
            )}

            {/* Hierarchy tab */}
            {activeTab === "hierarchy" && (
              <div className="absolute inset-0 overflow-y-auto p-4">
                <div className="mx-auto max-w-3xl">
                  <h2 className="mb-4 text-sm font-bold text-text">Project Hierarchy</h2>
                  <ProjectHierarchy
                    commander={{
                      name: "Eyrie",
                      role: "commander",
                      status: "running",
                    }}
                    captain={captainInstance ? {
                      name: captainInstance.display_name || captainInstance.name,
                      role: "captain",
                      status: captainInstance.status as any,
                      onClick: () => navigate(`/agents/${captainInstance.name}/chat`),
                    } : captainAgent ? {
                      name: captainAgent.name,
                      role: "captain",
                      status: captainAgent.alive ? "running" : "stopped",
                      onClick: () => navigate(`/agents/${captainAgent.name}/chat`),
                    } : null}
                    talons={roleAgents.map((a) => ({
                      name: a.display_name || a.name,
                      role: "talon" as const,
                      status: a.status as any,
                      onClick: () => navigate(`/agents/${a.name}/chat`),
                    }))}
                  />
                </div>
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Dialogs */}
      {showSetOrchestrator && (
        <SetCaptainDialog
          projectId={project.id}
          projectName={project.name}
          onDone={() => { setShowSetOrchestrator(false); refresh(); }}
          onClose={() => setShowSetOrchestrator(false)}
        />
      )}
      {showAddAgent && (
        <AddAgentDialog
          projectId={project.id}
          onCreated={refresh}
          onClose={() => setShowAddAgent(false)}
        />
      )}
    </div>
  );
}
