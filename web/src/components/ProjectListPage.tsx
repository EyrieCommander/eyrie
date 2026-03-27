import { useState, useEffect, useRef } from "react";
import { useNavigate } from "react-router-dom";
import { Plus, RefreshCw, Briefcase, ChevronRight } from "lucide-react";
import type { AgentInstance } from "../lib/types";
import { fetchInstances, createProject, createInstance, updateProject, instanceAction, deleteInstance } from "../lib/api";
import { useData } from "../lib/DataContext";

function CreateProjectDialog({ onCreated, onClose }: { onCreated: () => void; onClose: () => void }) {
  const dialogRef = useRef<HTMLDivElement>(null);
  const navigate = useNavigate();

  // Step 1: project details, Step 2: captain assignment
  const [step, setStep] = useState<1 | 2>(1);

  // Project fields
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [goal, setGoal] = useState("");

  // Captain fields
  const [captainMode, setCaptainMode] = useState<"create" | "existing">("create");
  const [captainName, setCaptainName] = useState("");
  const [captainFramework, setCaptainFramework] = useState("zeroclaw");
  const [existingCaptains, setExistingCaptains] = useState<AgentInstance[]>([]);
  const [selectedCaptainId, setSelectedCaptainId] = useState("");
  const [startingCaptain, setStartingCaptain] = useState("");

  const [creating, setCreating] = useState(false);
  const [error, setError] = useState("");

  // Derived default captain name from project name
  const defaultCaptainName = `captain-${name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "")}`;

  const [fetchCaptainError, setFetchCaptainError] = useState("");

  // Load all captain instances when entering step 2.
  // Assignment/stopped-state filtering is applied in the UI (disabled prop).
  useEffect(() => {
    if (step === 2) {
      setFetchCaptainError("");
      fetchInstances().then((all) => {

        setExistingCaptains(all.filter((i) => i.hierarchy_role === "captain"));
      }).catch((err) => {

        console.error("Failed to fetch instances:", err);
        setFetchCaptainError(err instanceof Error ? err.message : "Failed to load captain instances");
        setExistingCaptains([]);
      });
    }
  }, [step]);

  const handleCreate = async () => {
    setCreating(true);
    setError("");
    try {
      // Create the project
      const proj = await createProject({ name: name.trim(), description, goal: goal || undefined });

      // Create or assign captain
      if (captainMode === "create") {
        const effectiveName = captainName.trim() || defaultCaptainName;
        const inst = await createInstance({
          name: effectiveName,
          framework: captainFramework,
          hierarchy_role: "captain",
          project_id: proj.id,
          auto_start: true,
        });
        try {
          await updateProject(proj.id, { orchestrator_id: inst.id });
        } catch (updateErr) {
          // Clean up orphaned captain instance
          try { await deleteInstance(inst.id); } catch { /* best effort */ }
          throw updateErr;
        }
      } else if (selectedCaptainId) {
        await updateProject(proj.id, { orchestrator_id: selectedCaptainId });
      }

      onClose();
      onCreated();
      navigate(`/projects/${proj.id}`);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to create project");
    } finally {
      setCreating(false);
    }
  };

  const handleStartCaptain = async (id: string) => {
    setStartingCaptain(id);
    try {
      await instanceAction(id, "start");
      // Poll until instance status updates (max 15s)
      for (let i = 0; i < 15; i++) {

        await new Promise((r) => setTimeout(r, 1000));

        const all = await fetchInstances();

        const target = all.find((inst) => inst.id === id);
        if (target && target.status === "running") {
          setExistingCaptains(all.filter((inst) => inst.hierarchy_role === "captain"));
          setStartingCaptain("");
          return;
        }
      }
      // Timeout — refresh anyway
      setError("Captain may still be starting — check status and retry if needed");
      const all = await fetchInstances();
      setExistingCaptains(all.filter((inst) => inst.hierarchy_role === "captain"));
      setStartingCaptain("");
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to start captain");
      setStartingCaptain("");
    }
  };

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", handleKeyDown);
    dialogRef.current?.focus();
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [onClose]);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" role="dialog" aria-modal="true" onClick={onClose}>
      <div ref={dialogRef} tabIndex={-1} className="w-full max-w-md rounded border border-border bg-bg p-6 space-y-4 outline-none" onClick={(e) => e.stopPropagation()}>
        <h2 className="text-sm font-bold text-text">create project</h2>
        <div className="flex gap-2">
          <span className={`text-[10px] ${step === 1 ? "text-accent font-bold" : "text-text-muted"}`}>1. details</span>
          <span className="text-[10px] text-text-muted">&rarr;</span>
          <span className={`text-[10px] ${step === 2 ? "text-accent font-bold" : "text-text-muted"}`}>2. captain</span>
        </div>

        {error && (
          <div className="rounded border border-red/30 bg-red/5 px-3 py-2 text-xs text-red">{error}</div>
        )}

        {step === 1 && (
          <>
            <div>
              <label className="block text-xs font-medium text-text-secondary mb-1">name</label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="w-full rounded border border-border bg-surface px-3 py-2 text-xs text-text focus:border-accent focus:outline-none"
                placeholder="launch my SaaS"
                autoFocus
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-text-secondary mb-1">description</label>
              <textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                rows={2}
                className="w-full rounded border border-border bg-surface px-3 py-2 text-xs text-text focus:border-accent focus:outline-none resize-none"
                placeholder="what is this project about?"
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-text-secondary mb-1">goal (optional)</label>
              <input
                type="text"
                value={goal}
                onChange={(e) => setGoal(e.target.value)}
                className="w-full rounded border border-border bg-surface px-3 py-2 text-xs text-text focus:border-accent focus:outline-none"
                placeholder="the desired outcome"
              />
            </div>
            <div className="flex justify-end gap-2">
              <button
                onClick={onClose}
                className="rounded border border-border px-3 py-1.5 text-xs text-text-secondary transition-colors hover:bg-surface-hover"
              >
                cancel
              </button>
              <button
                onClick={() => setStep(2)}
                disabled={!name.trim()}
                className="rounded bg-accent px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-accent/80 disabled:opacity-50"
              >
                next
              </button>
            </div>
          </>
        )}

        {step === 2 && (
          <>
            {captainMode === "create" ? (
              <div className="space-y-3">
                <p className="text-xs text-text-muted">create a captain to lead this project</p>
                <div>
                  <label className="block text-xs font-medium text-text-secondary mb-1">captain name</label>
                  <input
                    type="text"
                    value={captainName}
                    onChange={(e) => setCaptainName(e.target.value)}
                    className="w-full rounded border border-border bg-surface px-3 py-2 text-xs text-text focus:border-accent focus:outline-none"
                    placeholder={defaultCaptainName}
                    autoFocus
                  />
                </div>
                <div>
                  <label className="block text-xs font-medium text-text-secondary mb-1">framework</label>
                  <select
                    value={captainFramework}
                    onChange={(e) => setCaptainFramework(e.target.value)}
                    className="w-full rounded border border-border bg-surface px-3 py-2 text-xs text-text focus:border-accent focus:outline-none"
                  >
                    <option value="zeroclaw">ZeroClaw</option>
                    <option value="openclaw">OpenClaw</option>
                    <option value="hermes">Hermes</option>
                  </select>
                </div>
                <button
                  onClick={() => setCaptainMode("existing")}
                  className="text-xs text-green hover:text-green/80 transition-colors"
                >
                  use existing captain instead
                </button>
              </div>
            ) : (
              <div className="space-y-3">
                <p className="text-xs text-text-muted">select an existing captain</p>
                {fetchCaptainError && (
                  <div className="rounded border border-red/30 bg-red/5 px-3 py-2 text-xs text-red">{fetchCaptainError}</div>
                )}
                <div className="space-y-1.5 max-h-48 overflow-y-auto">
                  {existingCaptains.length === 0 ? (
                    <div className="rounded border border-border bg-surface p-4 text-center text-xs text-text-muted">
                      no captain instances found
                    </div>
                  ) : (
                    existingCaptains.map((inst) => {
                      const isStopped = inst.status !== "running";
                      const isSelected = selectedCaptainId === inst.id;
                      return (
                        <div
                          key={inst.id}
                          className={`flex items-center gap-3 rounded border px-4 py-2.5 text-xs transition-all ${
                            isSelected
                              ? "border-green bg-green/5"
                              : "border-border bg-surface hover:border-green/30"
                          }`}
                        >
                          <button
                            onClick={() => setSelectedCaptainId(inst.id)}
                            className="flex flex-1 items-center gap-3 text-left"
                          >
                            <span className={`h-1.5 w-1.5 rounded-full ${isStopped ? "bg-text-muted" : "bg-green"}`} />
                            <span className="font-medium text-text">{inst.display_name}</span>
                            <span className="text-text-muted">{inst.framework}</span>
                            {inst.project_id && (
                              <span className="text-[10px] text-text-muted">(shared)</span>
                            )}
                          </button>
                          {isStopped && (
                            <button
                              disabled={startingCaptain === inst.id}
                              onClick={() => handleStartCaptain(inst.id)}
                              className="rounded bg-green px-2 py-0.5 text-[10px] font-medium text-white hover:bg-green/80 disabled:opacity-50"
                            >
                              {startingCaptain === inst.id ? "starting..." : "start"}
                            </button>
                          )}
                        </div>
                      );
                    })
                  )}
                </div>
                <button
                  onClick={() => { setCaptainMode("create"); setSelectedCaptainId(""); }}
                  className="text-xs text-green hover:text-green/80 transition-colors"
                >
                  &larr; create new instead
                </button>
              </div>
            )}

            <div className="flex justify-between">
              <button
                onClick={() => setStep(1)}
                className="text-xs text-text-muted hover:text-text transition-colors"
              >
                &larr; back
              </button>
              <div className="flex gap-2">
                <button
                  onClick={onClose}
                  className="rounded border border-border px-3 py-1.5 text-xs text-text-secondary transition-colors hover:bg-surface-hover"
                >
                  cancel
                </button>
                <button
                  onClick={handleCreate}
                  disabled={creating || (captainMode === "existing" && !selectedCaptainId)}
                  className="rounded bg-accent px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-accent/80 disabled:opacity-50"
                >
                  {creating ? "creating..." : "create project"}
                </button>
              </div>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

export default function ProjectListPage() {
  const navigate = useNavigate();
  const { projects, loading, error, refresh } = useData();
  const [showCreate, setShowCreate] = useState(false);

  return (
    <div className="space-y-6">
      <div className="text-xs text-text-muted">~/projects</div>

      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-bold">
            <span className="text-accent">&gt;</span> projects
          </h1>
          <p className="mt-1 text-xs text-text-muted">
            organize your work into projects with dedicated agent teams
          </p>
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={() => refresh()}
            disabled={loading}
            className="flex items-center gap-2 text-xs text-text-muted transition-colors hover:text-text disabled:opacity-50"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} />
          </button>
          <button
            onClick={() => setShowCreate(true)}
            className="flex items-center gap-1.5 rounded bg-accent px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-accent/80"
          >
            <Plus className="h-3 w-3" /> new project
          </button>
        </div>
      </div>

      {error && (
        <div className="rounded border border-red/30 bg-red/5 px-3 py-2 text-xs text-red">{error}</div>
      )}

      {loading && projects.length === 0 ? (
        <div className="py-12 text-center text-xs text-text-muted">loading projects...</div>
      ) : !error && projects.length === 0 ? (
        <div className="rounded border border-border bg-surface p-8 text-center space-y-3">
          <Briefcase className="mx-auto h-8 w-8 text-text-muted" />
          <p className="text-xs text-text-muted">
            no projects yet — create one to start building your agent team
          </p>
          <button
            onClick={() => setShowCreate(true)}
            className="rounded bg-accent px-4 py-2 text-xs font-medium text-white transition-colors hover:bg-accent/80"
          >
            create your first project
          </button>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          {projects.map((project) => (
            <button
              key={project.id}
              onClick={() => navigate(`/projects/${project.id}`)}
              className="flex items-center gap-3 rounded border border-border bg-surface p-4 text-left text-xs transition-all hover:border-accent/50 hover:bg-surface-hover/50"
            >
              <Briefcase className="h-5 w-5 shrink-0 text-green" />
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2">
                  <span className="font-medium text-text">{project.name}</span>
                  <span className="rounded bg-green/10 px-1.5 py-0.5 text-[10px] font-medium text-green">
                    {project.status}
                  </span>
                </div>
                {project.description && (
                  <p className="mt-0.5 text-text-muted truncate">{project.description}</p>
                )}
                <p className="mt-1 text-text-muted">
                  {(project.role_agent_ids?.length ?? 0)} agent{(project.role_agent_ids?.length ?? 0) !== 1 ? "s" : ""}
                </p>
              </div>
              <ChevronRight className="h-3.5 w-3.5 text-text-muted" />
            </button>
          ))}
        </div>
      )}

      {showCreate && (
        <CreateProjectDialog
          onCreated={refresh}
          onClose={() => setShowCreate(false)}
        />
      )}
    </div>
  );
}
