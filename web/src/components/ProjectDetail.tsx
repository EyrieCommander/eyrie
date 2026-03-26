import { useState, useEffect, useCallback, useRef } from "react";
import { useParams, useNavigate } from "react-router-dom";
import {
  ArrowLeft, Plus, Trash2, Briefcase, Crown,
  MessageSquare, Pause, Target,
} from "lucide-react";
import type { AgentInstance } from "../lib/types";
import { fetchHierarchy, deleteProject, agentAction, instanceAction } from "../lib/api";
import { useData } from "../lib/DataContext";
import { SetCaptainDialog } from "./SetCaptainDialog";
import { AddAgentDialog } from "./AddAgentDialog";
import { ProjectChat } from "./ProjectChat";

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
  const { agents, projects: ctxProjects, instances: ctxInstances, loading: ctxLoading, refresh: ctxRefresh } = useData();
  const [showAddAgent, setShowAddAgent] = useState(false);
  const [showSetOrchestrator, setShowSetOrchestrator] = useState(false);
  const [loadError, setLoadError] = useState("");
  const [commanderName, setCommanderName] = useState("");
  const [commanderStatus, setCommanderStatus] = useState("");
  const [startingAgent, setStartingAgent] = useState("");
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

  // Fetch hierarchy for commander info
  const refresh = useCallback(async () => {
    if (!id) return;
    try {
      setLoadError("");
      const [hierarchy] = await Promise.all([
        fetchHierarchy().catch(() => null),
        ctxRefresh(),
      ]);
      if (hierarchy?.commander) {
        setCommanderName(hierarchy.commander.name);
        setCommanderStatus(hierarchy.commander.status);
      }
      hasLoadedRef.current = true;
    } catch (err) {
      console.error("Failed to load project data:", err);
      setLoadError(err instanceof Error ? err.message : "Failed to load project");
    }
  }, [id, ctxRefresh]);

  useEffect(() => { refresh(); }, [refresh]);

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

  // Subscribe to project events for real-time updates
  useEffect(() => {
    if (!id) return;
    const evtSource = new EventSource(`/api/projects/${id}/events`);
    evtSource.onmessage = () => {
      // Any structural event → refresh data
      refresh();
    };
    return () => evtSource.close();
  }, [id, refresh]);

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
      // Poll until the agent shows as running
      const poll = setInterval(refresh, 2000);
      pollRef.current.interval = poll;
      pollRef.current.timeout = setTimeout(() => {
        clearInterval(poll);
        setStartingAgent("");
      }, 30000);
    } catch (e) {
      setLoadError(e instanceof Error ? e.message : "failed to start agent");
      setStartingAgent("");
    }
  };

  // Check if required agents are stopped
  const needsStart: { name: string; role: string; isInstance: boolean; id: string }[] = [];
  if (commanderName && commanderStatus !== "running") {
    const cmdInst = instances.find((i) => i.name === commanderName);
    needsStart.push({ name: commanderName, role: "commander", isInstance: !!cmdInst, id: cmdInst?.id || commanderName });
  }
  if (captainInstance && captainInstance.status !== "running") {
    needsStart.push({ name: captainInstance.display_name || captainInstance.name, role: "captain", isInstance: true, id: captainInstance.id });
  }
  if (captainAgent && !captainAgent.alive) {
    needsStart.push({ name: captainAgent.name, role: "captain", isInstance: false, id: captainAgent.name });
  }

  return (
    <div className="flex h-[calc(100vh-2rem)] flex-col">
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
        <div className="flex w-[280px] flex-shrink-0 flex-col gap-4 overflow-y-auto border-r border-border bg-surface p-4">
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
                  <div className="font-medium text-text">{captainAgent.name}</div>
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
                onClick={() => navigate(`/agents/${commanderName}/chat`)}
                className="flex flex-1 items-center justify-center gap-1.5 rounded bg-accent px-2 py-1.5 text-[10px] font-medium text-white hover:bg-accent/80"
              >
                <MessageSquare className="h-3 w-3" /> commander
              </button>
            </div>
          </div>
        </div>

        {/* Main workspace area */}
        <div className="flex flex-1 flex-col overflow-hidden">
          {!commanderName ? (
            <div className="flex flex-1 items-center justify-center">
              <div className="text-center space-y-3">
                <p className="text-xs text-text-muted">no commander set up yet</p>
                <button
                  onClick={() => navigate("/hierarchy")}
                  className="rounded bg-accent px-4 py-2 text-xs font-medium text-white hover:bg-accent/80"
                >
                  set up commander
                </button>
              </div>
            </div>
          ) : !hasCaptain ? (
            <div className="flex flex-1 items-center justify-center">
              <div className="text-center space-y-3">
                <p className="text-xs text-text-muted">assign a captain to start</p>
                <button
                  onClick={() => setShowSetOrchestrator(true)}
                  className="rounded bg-accent px-4 py-2 text-xs font-medium text-white hover:bg-accent/80"
                >
                  assign captain
                </button>
              </div>
            </div>
          ) : needsStart.length > 0 ? (
            <div className="flex flex-1 items-center justify-center">
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
              </div>
            </div>
          ) : (
            <ProjectChat
              projectId={project.id}
              participants={[
                { name: commanderName, role: "commander" },
                ...(captainInstance ? [{ name: captainInstance.name, role: "captain" }] : []),
                ...(captainAgent ? [{ name: captainAgent.name, role: "captain" }] : []),
                ...roleAgents.map((a) => ({ name: a.name, role: "talon" })),
              ]}
            />
          )}
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
