import { useState, useEffect, useCallback } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { ArrowLeft, Plus, Trash2, Briefcase, ChevronRight, Crown } from "lucide-react";
import type { Project, AgentInstance, AgentInfo, Persona } from "../lib/types";
import { fetchProjects, fetchInstances, fetchAgents, fetchPersonas, createInstance, deleteProject, updateProject, streamCaptainBriefing } from "../lib/api";

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

function SetCaptainDialog({
  projectId,
  projectName,
  onDone,
  onClose,
}: {
  projectId: string;
  projectName: string;
  onDone: () => void;
  onClose: () => void;
}) {
  const [mode, setMode] = useState<"create" | "existing">("create");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  // Create new form — default name derived from project
  const defaultName = `captain-${projectName.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "")}`;
  const [name, setName] = useState("");
  const [framework, setFramework] = useState("openclaw");
  const [captainInstances, setCaptainInstances] = useState<AgentInstance[]>([]);

  useEffect(() => {
    fetchInstances().then((all) => {
      setCaptainInstances(all.filter((i) => i.hierarchy_role === "captain" && !i.project_id));
    }).catch(() => {});
  }, []);

  const handleCreate = async () => {
    const effectiveName = name.trim() || defaultName;
    setSaving(true);
    setError("");
    try {
      const inst = await createInstance({
        name: effectiveName,
        framework,
        hierarchy_role: "captain",
        project_id: projectId,
        auto_start: true,
      });
      await updateProject(projectId, { orchestrator_id: inst.id });
      onDone();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to create captain");
    } finally {
      setSaving(false);
    }
  };

  const handleSelectExisting = async (agentName: string) => {
    setSaving(true);
    setError("");
    try {
      await updateProject(projectId, { orchestrator_id: agentName });
      onDone();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to set captain");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={onClose}>
      <div className="w-full max-w-md rounded border border-border bg-bg p-6 space-y-4" onClick={(e) => e.stopPropagation()}>
        <h2 className="text-sm font-bold text-text">assign captain</h2>

        {error && (
          <div className="rounded border border-red/30 bg-red/5 px-3 py-2 text-xs text-red">{error}</div>
        )}

        {mode === "create" ? (
          <div className="space-y-3">
            <p className="text-xs text-text-muted">create a dedicated captain agent for this project</p>
            <div>
              <label className="block text-xs font-medium text-text-secondary mb-1">name</label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="w-full rounded border border-border bg-surface px-3 py-2 text-xs text-text focus:border-accent focus:outline-none"
                placeholder={defaultName}
                autoFocus
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-text-secondary mb-1">framework</label>
              <select
                value={framework}
                onChange={(e) => setFramework(e.target.value)}
                className="w-full rounded border border-border bg-surface px-3 py-2 text-xs text-text focus:border-accent focus:outline-none"
              >
                <option value="openclaw">OpenClaw</option>
                <option value="zeroclaw">ZeroClaw</option>
                <option value="hermes">Hermes</option>
              </select>
            </div>
            <p className="text-[10px] text-text-muted">
              the captain will use the built-in project manager identity
            </p>
            <div className="flex items-center justify-between">
              <button
                onClick={() => setMode("existing")}
                className="text-xs text-green hover:text-green/80 transition-colors"
              >
                use existing captain
              </button>
              <div className="flex gap-2">
                <button onClick={onClose} className="rounded border border-border px-3 py-1.5 text-xs text-text-secondary hover:bg-surface-hover">
                  cancel
                </button>
                <button
                  onClick={handleCreate}
                  disabled={saving}
                  className="rounded bg-accent px-3 py-1.5 text-xs font-medium text-white hover:bg-accent/80 disabled:opacity-50"
                >
                  {saving ? "creating..." : "create captain"}
                </button>
              </div>
            </div>
          </div>
        ) : (
          <div className="space-y-3">
            <p className="text-xs text-text-muted">select an existing captain instance</p>
            <div className="space-y-1.5 max-h-64 overflow-y-auto">
              {captainInstances.length === 0 ? (
                <div className="rounded border border-border bg-surface p-4 text-center text-xs text-text-muted">
                  no captain instances available
                </div>
              ) : (
                captainInstances.map((inst) => (
                  <button
                    key={inst.id}
                    onClick={() => handleSelectExisting(inst.id)}
                    disabled={saving}
                    className="flex w-full items-center gap-3 rounded border border-border bg-surface px-4 py-3 text-left text-xs transition-all hover:border-green/50 hover:bg-surface-hover/50 disabled:opacity-50"
                  >
                    <span className={`h-1.5 w-1.5 rounded-full ${inst.status === "running" ? "bg-green" : "bg-text-muted"}`} />
                    <div className="flex-1">
                      <span className="font-medium text-text">{inst.display_name}</span>
                      <span className="ml-2 text-text-muted">{inst.framework} · :{inst.port}</span>
                    </div>
                  </button>
                ))
              )}
            </div>
            <div className="flex items-center justify-between">
              <button
                onClick={() => setMode("create")}
                className="text-xs text-green hover:text-green/80 transition-colors"
              >
                &larr; create new instead
              </button>
              <button onClick={onClose} className="rounded border border-border px-3 py-1.5 text-xs text-text-secondary hover:bg-surface-hover">
                cancel
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function AddAgentDialog({
  projectId,
  onCreated,
  onClose,
}: {
  projectId: string;
  onCreated: () => void;
  onClose: () => void;
}) {
  const [name, setName] = useState("");
  const [framework, setFramework] = useState("openclaw");
  const [personaId, setPersonaId] = useState("");
  const [personas, setPersonas] = useState<Persona[]>([]);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    fetchPersonas().then(setPersonas).catch((err) => {
      console.error("Failed to load personas:", err);
      setPersonas([]);
    });
  }, []);

  const handleCreate = async () => {
    const trimmedName = name.trim();
    if (!trimmedName) {
      setError("Name cannot be blank");
      return;
    }
    setCreating(true);
    setError("");
    try {
      await createInstance({
        name: trimmedName,
        framework,
        persona_id: personaId || undefined,
        hierarchy_role: "talon",
        project_id: projectId,
        auto_start: true,
      });
      onCreated();
      onClose();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to create agent");
    } finally {
      setCreating(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={onClose}>
      <div className="w-full max-w-md rounded border border-border bg-bg p-6 space-y-4" onClick={(e) => e.stopPropagation()}>
        <h2 className="text-sm font-bold text-text">add agent to project</h2>

        <div>
          <label className="block text-xs font-medium text-text-secondary mb-1">name</label>
          <input
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="w-full rounded border border-border bg-surface px-3 py-2 text-xs text-text focus:border-accent focus:outline-none"
            placeholder="researcher-riley"
            autoFocus
          />
        </div>

        <div>
          <label className="block text-xs font-medium text-text-secondary mb-1">framework</label>
          <select
            value={framework}
            onChange={(e) => setFramework(e.target.value)}
            className="w-full rounded border border-border bg-surface px-3 py-2 text-xs text-text focus:border-accent focus:outline-none"
          >
            <option value="zeroclaw">ZeroClaw</option>
            <option value="openclaw">OpenClaw</option>
            <option value="hermes">Hermes</option>
          </select>
        </div>

        <div>
          <label className="block text-xs font-medium text-text-secondary mb-1">persona (optional)</label>
          <select
            value={personaId}
            onChange={(e) => setPersonaId(e.target.value)}
            className="w-full rounded border border-border bg-surface px-3 py-2 text-xs text-text focus:border-accent focus:outline-none"
          >
            <option value="">none</option>
            {personas.map((p) => (
              <option key={p.id} value={p.id}>{p.icon} {p.name} — {p.role}</option>
            ))}
          </select>
        </div>

        {error && (
          <div className="rounded border border-red/30 bg-red/5 px-3 py-2 text-xs text-red">{error}</div>
        )}

        <div className="flex justify-end gap-2">
          <button onClick={onClose} className="rounded border border-border px-3 py-1.5 text-xs text-text-secondary hover:bg-surface-hover">
            cancel
          </button>
          <button
            onClick={handleCreate}
            disabled={creating || !name.trim()}
            className="rounded bg-accent px-3 py-1.5 text-xs font-medium text-white hover:bg-accent/80 disabled:opacity-50"
          >
            {creating ? "creating..." : "create agent"}
          </button>
        </div>
      </div>
    </div>
  );
}

export default function ProjectDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [project, setProject] = useState<Project | null>(null);
  const [instances, setInstances] = useState<AgentInstance[]>([]);
  const [agents, setAgents] = useState<AgentInfo[]>([]);
  const [showAddAgent, setShowAddAgent] = useState(false);
  const [showSetOrchestrator, setShowSetOrchestrator] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");

  const refresh = useCallback(async () => {
    if (!id) return;
    try {
      setLoading(true);
      setLoadError("");
      const [projects, allInstances, allAgents] = await Promise.all([
        fetchProjects(),
        fetchInstances(),
        fetchAgents(),
      ]);
      const p = projects.find((p) => p.id === id);
      setProject(p ?? null);
      setAgents(allAgents);
      if (p) {
        const projectInstances = allInstances.filter(
          (inst) =>
            inst.project_id === id ||
            p.orchestrator_id === inst.id ||
            p.role_agent_ids?.includes(inst.id),
        );
        setInstances(projectInstances);
      }
    } catch (err) {
      console.error("Failed to load project data:", err);
      setLoadError(err instanceof Error ? err.message : "Failed to load project");
    } finally {
      setLoading(false);
    }
  }, [id]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  // Poll while any instance is provisioning
  useEffect(() => {
    const hasProvisioning = instances.some((i) => i.status === "created" || i.status === "provisioning");
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
                const { sessionReady } = streamCaptainBriefing(project.id, () => {});
                sessionReady.then(() => {
                  navigate(`/agents/${captainInstance.name}/chat?brief=captain`);
                });
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
    </div>
  );
}
