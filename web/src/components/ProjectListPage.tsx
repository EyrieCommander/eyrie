import { useState, useEffect, useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { Plus, RefreshCw, Briefcase, ChevronRight } from "lucide-react";
import type { Project } from "../lib/types";
import { fetchProjects, createProject } from "../lib/api";

function CreateProjectDialog({ onCreated, onClose }: { onCreated: () => void; onClose: () => void }) {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [goal, setGoal] = useState("");
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState("");

  const handleCreate = async () => {
    setCreating(true);
    setError("");
    try {
      await createProject({ name, description, goal: goal || undefined });
      onClose();
      onCreated();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to create project");
    } finally {
      setCreating(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={onClose}>
      <div className="w-full max-w-md rounded border border-border bg-bg p-6 space-y-4" onClick={(e) => e.stopPropagation()}>
        <h2 className="text-sm font-bold text-text">create project</h2>

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

        {error && (
          <div className="rounded border border-red/30 bg-red/5 px-3 py-2 text-xs text-red">
            {error}
          </div>
        )}

        <div className="flex justify-end gap-2">
          <button
            onClick={onClose}
            className="rounded border border-border px-3 py-1.5 text-xs text-text-secondary transition-colors hover:bg-surface-hover"
          >
            cancel
          </button>
          <button
            onClick={handleCreate}
            disabled={creating || !name}
            className="rounded bg-accent px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-accent/80 disabled:opacity-50"
          >
            {creating ? "creating..." : "create"}
          </button>
        </div>
      </div>
    </div>
  );
}

export default function ProjectListPage() {
  const navigate = useNavigate();
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [error, setError] = useState("");

  const refresh = useCallback(async () => {
    try {
      setLoading(true);
      setError("");
      const data = await fetchProjects();
      setProjects(data);
    } catch (err) {
      console.error("Failed to fetch projects:", err);
      setError(err instanceof Error ? err.message : "Failed to load projects");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

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
            onClick={refresh}
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
