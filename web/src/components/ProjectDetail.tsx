import { useState, useEffect, useCallback, useRef } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { ArrowLeft, Plus, Trash2, Briefcase, ChevronRight, Crown } from "lucide-react";
import { MessageSquare } from "lucide-react";
import type { AgentInstance } from "../lib/types";
import { fetchHierarchy, deleteProject, streamCaptainBriefing, agentAction, instanceAction } from "../lib/api";
import { useData } from "../lib/DataContext";
import { SetCaptainDialog } from "./SetCaptainDialog";
import { AddAgentDialog } from "./AddAgentDialog";
import { ProjectChat } from "./ProjectChat";

function InstanceRow({ instance, onClick }: { instance: AgentInstance; onClick: () => void }) {
  const isProvisioning = instance.status === "created" || instance.status === "provisioning" || instance.status === "starting";
  const statusColor = isProvisioning
    ? "bg-yellow-400"
    : instance.status === "running"
      ? "bg-green"
      : instance.status === "error"
        ? "bg-red"
        : "bg-text-muted";
  return (
    <button
      onClick={onClick}
      className={`flex w-full items-center gap-3 rounded border px-4 py-3 text-left text-xs transition-all ${
        isProvisioning
          ? "border-yellow-400/30 bg-yellow-400/5 hover:border-yellow-400/50 hover:bg-yellow-400/10"
          : "border-border bg-surface hover:border-accent/50 hover:bg-surface-hover/50"
      }`}
    >
      <span className={`h-1.5 w-1.5 rounded-full ${statusColor} ${isProvisioning ? "animate-pulse" : ""}`} />
      <div className="flex-1 min-w-0">
        <span className="font-medium text-text">{instance.display_name}</span>
        <span className="ml-2 text-text-muted">{instance.framework} · :{instance.port}</span>
        {isProvisioning && (
          <span className="ml-2 rounded bg-yellow-400/10 px-1.5 py-0.5 text-[10px] font-medium text-yellow-400">
            provisioning...
          </span>
        )}
        {!isProvisioning && instance.hierarchy_role && (
          <span className="ml-2 rounded bg-accent/10 px-1.5 py-0.5 text-[10px] font-medium text-accent">
            {instance.hierarchy_role}
          </span>
        )}
      </div>
      <ChevronRight className="h-3.5 w-3.5 text-text-muted" />
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
  const [tab, setTab] = useState<"team" | "chat">("chat");
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

  // Fetch hierarchy for commander info (not in context)
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

  useEffect(() => {
    refresh();
  }, [refresh]);

  // Cleanup pollRef on unmount
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

  if (loading && !project) {
    return <div className="py-20 text-center text-xs text-text-muted">loading project...</div>;
  }

  if (!project) {
    return (
      <div className="py-20 text-center text-xs text-text-muted">
        project not found
      </div>
    );
  }

  // Orchestrator can be a provisioned instance or a legacy agent name
  const captainInstance = instances.find((i) => i.id === project.orchestrator_id);
  const captainAgent = !captainInstance
    ? agents.find((a) => a.name === project.orchestrator_id)
    : null;
  const hasCaptain = captainInstance || captainAgent;
  const roleAgents = instances.filter((i) => i.hierarchy_role === "talon");

  return (
    <div className="space-y-6">
      <div className="text-xs text-text-muted">~/projects/{project.name}</div>

      {loadError && (
        <div className="rounded border border-red/30 bg-red/5 px-3 py-2 text-xs text-red">{loadError}</div>
      )}

      <div className="flex items-center gap-3">
        <button
          onClick={() => navigate("/projects")}
          className="rounded p-1 text-text-muted transition-colors hover:bg-surface-hover hover:text-text"
        >
          <ArrowLeft className="h-4 w-4" />
        </button>
        <div className="flex-1">
          <div className="flex items-center gap-2">
            <Briefcase className="h-4 w-4 text-green" />
            <h1 className="text-xl font-bold text-text">{project.name}</h1>
            <span className="rounded bg-green/10 px-1.5 py-0.5 text-[10px] font-medium text-green">
              {project.status}
            </span>
          </div>
          {project.description && (
            <p className="mt-1 text-xs text-text-muted">{project.description}</p>
          )}
          {project.goal && (
            <p className="mt-0.5 text-xs text-text-secondary">goal: {project.goal}</p>
          )}
        </div>
        <button
          onClick={async () => {
            if (confirm("delete this project?")) {
              try {
                await deleteProject(project.id);
                navigate("/projects");
              } catch (e) {
                setLoadError(e instanceof Error ? e.message : "Failed to delete project");
              }
            }
          }}
          className="rounded p-2 text-text-muted transition-colors hover:bg-red/10 hover:text-red"
          title="delete project"
        >
          <Trash2 className="h-3.5 w-3.5" />
        </button>
      </div>

      {/* Tabs */}
      <div className="flex gap-4 border-b border-border">
        <button
          onClick={() => setTab("chat")}
          className={`flex items-center gap-1.5 pb-2 text-xs font-medium transition-colors ${
            tab === "chat" ? "border-b-2 border-accent text-accent" : "text-text-muted hover:text-text"
          }`}
        >
          <MessageSquare className="h-3.5 w-3.5" /> chat
        </button>
        <button
          onClick={() => setTab("team")}
          className={`flex items-center gap-1.5 pb-2 text-xs font-medium transition-colors ${
            tab === "team" ? "border-b-2 border-accent text-accent" : "text-text-muted hover:text-text"
          }`}
        >
          <Crown className="h-3.5 w-3.5" /> team
        </button>
      </div>

      {/* Chat tab */}
      {tab === "chat" && (() => {
        if (!commanderName) {
          return (
            <div className="py-10 text-center space-y-3">
              <p className="text-xs text-text-muted">no commander set up yet</p>
              <button
                onClick={() => navigate("/hierarchy")}
                className="rounded bg-accent px-4 py-2 text-xs font-medium text-white hover:bg-accent/80"
              >
                set up commander
              </button>
            </div>
          );
        }

        if (!hasCaptain) {
          return (
            <div className="py-10 text-center space-y-3">
              <p className="text-xs text-text-muted">assign a captain to start the project chat</p>
              <button
                onClick={() => setTab("team")}
                className="rounded bg-accent px-4 py-2 text-xs font-medium text-white hover:bg-accent/80"
              >
                assign captain
              </button>
            </div>
          );
        }

        // Check which required agents are stopped
        const stoppedAgents: { name: string; role: string; isInstance: boolean; id: string }[] = [];
        if (commanderStatus !== "running") {
          const cmdInst = instances.find((i) => i.name === commanderName);
          stoppedAgents.push({ name: commanderName, role: "commander", isInstance: !!cmdInst, id: cmdInst?.id || commanderName });
        }
        if (captainInstance && captainInstance.status !== "running") {
          stoppedAgents.push({ name: captainInstance.display_name || captainInstance.name, role: "captain", isInstance: true, id: captainInstance.id });
        }
        if (captainAgent && !captainAgent.alive) {
          stoppedAgents.push({ name: captainAgent.name, role: "captain", isInstance: false, id: captainAgent.name });
        }

        if (stoppedAgents.length > 0) {
          const pollUntilRunning = () => {
            // Clear any existing poll to prevent overlapping intervals
            if (pollRef.current.interval) clearInterval(pollRef.current.interval);
            if (pollRef.current.timeout) clearTimeout(pollRef.current.timeout);

            const poll = setInterval(async () => {
              await refresh();
            }, 2000);
            pollRef.current.interval = poll;
            // Stop polling after 30s as a safety net
            const timeout = setTimeout(() => {
              clearInterval(poll);
              pollRef.current.interval = null;
              pollRef.current.timeout = null;
              setStartingAgent("");
            }, 30000);
            pollRef.current.timeout = timeout;
          };

          return (
            <div className="py-10 text-center space-y-4">
              <p className="text-xs text-text-muted">
                these agents need to be running before starting the chat
              </p>
              <div className="flex flex-col items-center gap-2">
                {stoppedAgents.map((a) => (
                  <div key={a.id} className="flex items-center gap-3 rounded border border-border bg-surface px-4 py-2 text-xs">
                    <span className={`h-1.5 w-1.5 rounded-full ${startingAgent === a.id || startingAgent === "all" ? "bg-yellow-400 animate-pulse" : "bg-text-muted"}`} />
                    <span className="font-medium text-text">{a.name}</span>
                    <span className="text-text-muted">{a.role}</span>
                    <button
                      disabled={!!startingAgent}
                      onClick={async () => {
                        setStartingAgent(a.id);
                        try {
                          if (a.isInstance) await instanceAction(a.id, "start");
                          else await agentAction(a.id, "start");
                          pollUntilRunning();
                        } catch (e) {
                          setLoadError(e instanceof Error ? e.message : "failed to start agent");
                          setStartingAgent("");
                        }
                      }}
                      className="rounded bg-green px-2 py-0.5 text-[10px] font-medium text-white hover:bg-green/80 disabled:opacity-50"
                    >
                      {startingAgent === a.id || startingAgent === "all" ? "starting..." : "start"}
                    </button>
                  </div>
                ))}
              </div>
              {stoppedAgents.length > 1 && (
                <button
                  disabled={!!startingAgent}
                  onClick={async () => {
                    setStartingAgent("all");
                    try {
                      for (const a of stoppedAgents) {
                        if (a.isInstance) await instanceAction(a.id, "start");
                        else await agentAction(a.id, "start");
                      }
                      pollUntilRunning();
                    } catch (e) {
                      setLoadError(e instanceof Error ? e.message : "failed to start agents");
                      setStartingAgent("");
                    }
                  }}
                  className="rounded bg-accent px-4 py-2 text-xs font-medium text-white hover:bg-accent/80 disabled:opacity-50"
                >
                  {startingAgent === "all" ? "starting..." : "start all"}
                </button>
              )}
            </div>
          );
        }

        return (
          <ProjectChat
            projectId={project.id}
            participants={[
              { name: commanderName, role: "commander" },
              ...(captainInstance ? [{ name: captainInstance.name, role: "captain" }] : []),
              ...(captainAgent ? [{ name: captainAgent.name, role: "captain" }] : []),
              ...roleAgents.map((a) => ({ name: a.name, role: "talon" })),
            ]}
          />
        );
      })()}

      {/* Team tab */}
      {tab !== "chat" && <>

      {/* Orchestrator */}
      <div>
        <div className="mb-2 flex items-center justify-between">
          <span className="text-[10px] font-medium uppercase tracking-wider text-text-muted">
            captain
          </span>
          {!hasCaptain && (
            <button
              onClick={() => setShowSetOrchestrator(true)}
              className="flex items-center gap-1 text-xs text-accent transition-colors hover:text-accent/80"
            >
              <Crown className="h-3 w-3" /> assign captain
            </button>
          )}
        </div>
        {captainInstance ? (
          <div className="space-y-1.5">
            <InstanceRow
              instance={captainInstance}
              onClick={() => navigate(`/agents/${captainInstance.name}/chat`)}
            />
            <button
              onClick={() => {
                const { sessionReady } = streamCaptainBriefing(project.id, (ev) => {
                  if (ev.type === "error") console.error("Captain briefing error:", ev.error);
                });
                sessionReady
                  .then(() => navigate(`/agents/${captainInstance.name}/chat?brief=captain`))
                  .catch((e) => console.error("Captain briefing session failed:", e));
              }}
              className="ml-4 text-xs text-green hover:text-green/80 transition-colors"
            >
              brief captain on project
            </button>
          </div>
        ) : captainAgent ? (
          <button
            onClick={() => navigate(`/agents/${captainAgent.name}`)}
            className="flex w-full items-center gap-3 rounded border border-border bg-surface px-4 py-3 text-left text-xs transition-all hover:border-accent/50 hover:bg-surface-hover/50"
          >
            <span className={`h-1.5 w-1.5 rounded-full ${captainAgent.alive ? "bg-green" : "bg-red"}`} />
            <div className="flex-1">
              <span className="font-medium text-text">{captainAgent.name}</span>
              <span className="ml-2 text-text-muted">{captainAgent.framework} · :{captainAgent.port}</span>
              <span className="ml-2 rounded bg-text-muted/10 px-1.5 py-0.5 text-[10px] text-text-muted">existing agent</span>
            </div>
            <ChevronRight className="h-3.5 w-3.5 text-text-muted" />
          </button>
        ) : (
          <div className="rounded border border-border bg-surface p-4 text-center text-xs text-text-muted">
            no captain assigned
          </div>
        )}
      </div>

      {showSetOrchestrator && (
        <SetCaptainDialog
          projectId={project.id}
          projectName={project.name}
          onDone={() => { setShowSetOrchestrator(false); refresh(); }}
          onClose={() => setShowSetOrchestrator(false)}
        />
      )}

      {/* Role Agents */}
      <div>
        <div className="mb-2 flex items-center justify-between">
          <span className="text-[10px] font-medium uppercase tracking-wider text-text-muted">
            role agents ({roleAgents.length})
          </span>
          <button
            onClick={() => setShowAddAgent(true)}
            className="flex items-center gap-1 text-xs text-accent transition-colors hover:text-accent/80"
          >
            <Plus className="h-3 w-3" /> add agent
          </button>
        </div>
        {roleAgents.length === 0 ? (
          <div className="rounded border border-border bg-surface p-6 text-center text-xs text-text-muted">
            no role agents yet — add one to start building your team
          </div>
        ) : (
          <div className="space-y-1.5">
            {roleAgents.map((agent) => (
              <InstanceRow
                key={agent.id}
                instance={agent}
                onClick={() => navigate(`/agents/${agent.name}`)}
              />
            ))}
          </div>
        )}
      </div>

      {showAddAgent && (
        <AddAgentDialog
          projectId={project.id}
          onCreated={refresh}
          onClose={() => setShowAddAgent(false)}
        />
      )}

      </>}
    </div>
  );
}
