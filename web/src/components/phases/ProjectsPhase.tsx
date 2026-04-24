// Phase 2: Projects. Single-page creation form with inline agent provisioning.
//
// Progress labels in the macro timeline (describe → team → goal → launch) are
// section markers, not separate wizard pages — everything is visible and
// editable at once. Captain is required, talons are optional, both can be
// an existing agent or provisioned new inline (framework + name, using the
// framework's default persona; persona-per-role picker is a follow-up).

import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { Plus, Trash2, Loader2, Crown, Feather, Briefcase } from "lucide-react";
import { useData } from "../../lib/DataContext";
import {
  createInstance,
  createProject,
  fetchFrameworks,
  updateProject,
} from "../../lib/api";
import type { AgentInstance, Framework } from "../../lib/types";
import { getFrameworkStatus } from "../../lib/frameworkStatus";

/** A team slot the user is configuring. New = to-be-provisioned, existing = use
 *  this AgentInstance already in the pool. */
type TeamSlot =
  | { id: string; kind: "existing"; instanceId: string }
  | { id: string; kind: "new"; name: string; framework: string };

function slotId() {
  return Math.random().toString(36).slice(2, 8);
}

function newSlot(framework: string): TeamSlot {
  return { id: slotId(), kind: "new", name: "", framework };
}

export default function ProjectsPhase() {
  const { projects, instances, backendDown, refresh: refreshData } = useData();
  const navigate = useNavigate();

  // Installed/ready frameworks (for the dropdowns).
  // Uses a cancellation guard so StrictMode's double-mount doesn't let a
  // stale promise's .catch nuke frameworks that a later fetch loaded.
  const [frameworks, setFrameworks] = useState<Framework[]>([]);
  const [fwLoading, setFwLoading] = useState(true);
  useEffect(() => {
    let cancelled = false;
    setFwLoading(true);
    fetchFrameworks()
      .then((list) => {
        if (cancelled) return;
        // Only offer frameworks that are installed — can't provision from a
        // framework we haven't set up.
        setFrameworks(list.filter((fw) => getFrameworkStatus(fw).isInstalled));
      })
      .catch(() => {
        // Don't clear frameworks on error — keep whatever we had.
      })
      .finally(() => {
        // Always clear loading for the non-cancelled invocation.
        // In StrictMode the first effect is cancelled before its fetch
        // resolves, so only the second (active) effect clears the flag.
        if (!cancelled) setFwLoading(false);
      });
    return () => { cancelled = true; };
  }, []);

  // Don't fall back to a hardcoded framework name — if nothing is installed,
  // the form shows the "install a framework first" message. Using "" here
  // means the captain slot starts with framework="" and the <select> shows
  // the first installed framework once frameworks load (synced by the
  // useEffect below).
  const defaultFramework = frameworks[0]?.id || "";

  // Show-or-hide the form. Starts open when there are no projects yet.
  const [formOpen, setFormOpen] = useState(projects.length === 0);
  useEffect(() => {
    if (projects.length === 0) setFormOpen(true);
  }, [projects.length]);

  // Form state
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [goal, setGoal] = useState("");
  const [framework, setFramework] = useState<string>(defaultFramework);
  useEffect(() => {
    // Keep framework in sync if the user hasn't picked yet and the default changes
    if (!framework && defaultFramework) setFramework(defaultFramework);
  }, [defaultFramework, framework]);

  const [captain, setCaptain] = useState<TeamSlot>(newSlot(defaultFramework));
  const [talons, setTalons] = useState<TeamSlot[]>([]);

  // When the installed framework list loads (defaultFramework transitions from
  // the hard-coded "zeroclaw" to the first *actually installed* framework),
  // sync the captain's framework as long as the user hasn't customised it
  // yet. An "uncustomised" new-kind slot is one with an empty name; once
  // they start typing or pick an existing captain, leave it alone.
  useEffect(() => {
    if (!defaultFramework) return;
    if (captain.kind !== "new") return;
    if (captain.framework === defaultFramework) return;
    if (captain.name.trim() !== "") return;
    setCaptain(newSlot(defaultFramework));
  }, [defaultFramework, captain]);

  // Filter pool to framework-compatible instances, and separate existing captains / talons
  const frameworkInstances = useMemo(
    () => instances.filter((i) => i.framework === framework),
    [instances, framework],
  );
  const existingCaptains = useMemo(
    () => frameworkInstances.filter((i) => i.hierarchy_role === "captain"),
    [frameworkInstances],
  );
  const existingTalons = useMemo(
    () => frameworkInstances.filter((i) => i.hierarchy_role === "talon"),
    [frameworkInstances],
  );

  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const resetForm = useCallback(() => {
    setName("");
    setDescription("");
    setGoal("");
    setCaptain(newSlot(defaultFramework));
    setTalons([]);
    setError(null);
  }, [defaultFramework]);

  const handleSubmit = async () => {
    setError(null);
    const projectName = name.trim() || "finance tracker";
    const projectDesc = description.trim();
    const defaultCaptainName = projectName.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "") + "-captain";
    const captainName = captain.kind === "new"
      ? (captain.name.trim() || defaultCaptainName)
      : "";
    if (captain.kind === "existing" && !captain.instanceId)
      return setError("pick a captain");

    setSubmitting(true);
    const provisionedIds: string[] = [];
    try {
      const proj = await createProject({
        name: projectName,
        description: projectDesc,
        goal: goal.trim() || undefined,
      });

      // Captain
      let captainId: string;
      if (captain.kind === "existing") {
        captainId = captain.instanceId;
      } else {
        const inst = await createInstance({
          name: captainName,
          framework: captain.framework,
          hierarchy_role: "captain",
          project_id: proj.id,
          auto_start: true,
        });
        captainId = inst.id;
        provisionedIds.push(inst.id);
      }
      await updateProject(proj.id, { orchestrator_id: captainId });

      // Talons (only new ones — existing-talon reassignment isn't supported
      // by the project update API, and the project page handles linking pool
      // agents after creation anyway).
      for (const t of talons) {
        if (t.kind === "new" && t.name.trim()) {
          const inst = await createInstance({
            name: t.name.trim(),
            framework: t.framework,
            hierarchy_role: "talon",
            project_id: proj.id,
            auto_start: true,
          });
          provisionedIds.push(inst.id);
        }
      }

      await refreshData(true);
      resetForm();
      navigate(`/projects/${proj.id}`);
    } catch (e) {
      // Roll back provisioned instances so the user doesn't have orphans.
      // The project itself we leave — they can fix + finish it from the project page.
      setError(e instanceof Error ? e.message : "failed to create project");
      if (provisionedIds.length > 0) {
        const { deleteInstance } = await import("../../lib/api");
        for (const id of provisionedIds) {
          try {
            await deleteInstance(id);
          } catch {
            /* best effort */
          }
        }
      }
    } finally {
      setSubmitting(false);
    }
  };

  if (fwLoading) {
    return (
      <div className="rounded border border-border bg-surface px-4 py-4 text-xs text-text-muted">
        Loading frameworks…
      </div>
    );
  }

  // No installed frameworks — can't create new projects. This also covers
  // the case where backendDown is true and frameworks never loaded: show
  // the guidance rather than falling through to a form with empty selects.
  const noFrameworks = frameworks.length === 0;

  return (
    <div className="space-y-4">
      {/* Existing projects list — always shown regardless of framework state */}
      {projects.length > 0 && (
        <div className="space-y-2">
          <h2 className="text-[10px] font-medium uppercase tracking-wider text-text-muted">
            your projects ({projects.length})
          </h2>
          <div className="grid gap-2 sm:grid-cols-2">
            {projects.map((p) => (
              <Link
                key={p.id}
                to={`/projects/${p.id}`}
                className="flex items-start gap-3 rounded border border-border bg-surface p-3 hover:border-accent/30 transition-colors"
              >
                <Briefcase className="h-4 w-4 text-text-muted mt-0.5 shrink-0" />
                <div className="min-w-0 flex-1">
                  <div className="text-xs font-medium text-text truncate">
                    {p.name}
                  </div>
                  <div className="text-[10px] text-text-muted truncate">
                    {p.description || "no description"}
                  </div>
                </div>
              </Link>
            ))}
          </div>
        </div>
      )}

      {/* No installed frameworks — can't create new projects */}
      {noFrameworks && (
        <div className="rounded border border-yellow/30 bg-yellow/5 px-4 py-4 text-xs text-text-secondary">
          {backendDown
            ? "Cannot reach the backend — check that Eyrie is running."
            : <>
                Install a framework first — you need an agent runtime before creating a project.{" "}
                <Link to="/?phase=frameworks" className="text-accent hover:text-accent/80 transition-colors">
                  go to frameworks &rarr;
                </Link>
              </>
          }
        </div>
      )}

      {/* Collapsed entry point after at least one project exists */}
      {!noFrameworks && projects.length > 0 && !formOpen && (
        <button
          onClick={() => setFormOpen(true)}
          className="flex items-center gap-1.5 rounded border border-dashed border-border px-4 py-3 text-xs text-text-muted hover:text-text hover:border-accent/30 transition-colors w-full justify-center"
        >
          <Plus className="h-3 w-3" /> new project
        </button>
      )}

      {/* Form */}
      {!noFrameworks && formOpen && (
        <div className="rounded border border-border bg-surface p-4 space-y-4">
          <div>
            <h2 className="text-sm font-semibold text-text">
              {projects.length === 0 ? "create your first project" : "new project"}
            </h2>
            <p className="text-xs text-text-muted mt-0.5">
              A project groups agents around a shared goal. Pick a captain,
              optionally assign talons, and let the commander coordinate.
            </p>
          </div>

          {/* Describe */}
          <Section label="describe" n={1}>
            <Field label="project name">
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="finance tracker"
                className="w-full rounded border border-border bg-bg px-2 py-1.5 text-xs text-text focus:border-accent focus:outline-none"
              />
            </Field>
            <Field label="description">
              <textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="Build a personal finance tracker with budget alerts."
                rows={2}
                className="w-full rounded border border-border bg-bg px-2 py-1.5 text-xs text-text focus:border-accent focus:outline-none"
              />
            </Field>
          </Section>

          {/* Team */}
          <Section label="team" n={2}>
            <p className="text-[10px] text-text-muted mb-2">
              commander: <span className="text-purple font-medium">eyrie</span>{" "}
              (always)
            </p>
            <div className="space-y-2">
              <TeamSlotEditor
                role="captain"
                slot={captain}
                setSlot={setCaptain}
                existing={existingCaptains}
                frameworks={frameworks}
                projectName={name.trim() || "finance tracker"}
              />
              {talons.map((t, i) => (
                <TeamSlotEditor
                  key={t.id}
                  role="talon"
                  slot={t}
                  setSlot={(next) =>
                    setTalons((arr) => {
                      const copy = [...arr];
                      copy[i] = next;
                      return copy;
                    })
                  }
                  onRemove={() =>
                    setTalons((arr) => arr.filter((_, idx) => idx !== i))
                  }
                  existing={existingTalons}
                  frameworks={frameworks}
                />
              ))}
              <button
                onClick={() => setTalons((arr) => [...arr, newSlot(framework)])}
                className="flex items-center gap-1.5 rounded border border-dashed border-border px-3 py-1.5 text-xs text-text-muted hover:text-text hover:border-accent/30 transition-colors w-full justify-center"
              >
                <Plus className="h-3 w-3" /> add talon
              </button>
            </div>
          </Section>

          {/* Goal */}
          <Section label="goal" n={3}>
            <Field label="shared objective (optional — briefs agents)">
              <textarea
                value={goal}
                onChange={(e) => setGoal(e.target.value)}
                placeholder="Get the user to a working MVP by end of week."
                rows={2}
                className="w-full rounded border border-border bg-bg px-2 py-1.5 text-xs text-text focus:border-accent focus:outline-none"
              />
            </Field>
          </Section>

          {error && (
            <div className="rounded border border-red/30 bg-red/5 px-3 py-2 text-xs text-red">
              {error}
            </div>
          )}

          {/* Launch */}
          <div className="flex items-center justify-end gap-2 pt-2 border-t border-border">
            {projects.length > 0 && (
              <button
                onClick={() => {
                  setFormOpen(false);
                  resetForm();
                }}
                disabled={submitting}
                className="text-xs text-text-muted hover:text-text transition-colors"
              >
                cancel
              </button>
            )}
            <button
              onClick={handleSubmit}
              disabled={submitting}
              className="flex items-center gap-1.5 rounded bg-accent px-4 py-1.5 text-xs font-medium text-white transition-colors hover:bg-accent-hover disabled:opacity-40"
            >
              {submitting && <Loader2 className="h-3 w-3 animate-spin" />}
              create project &rarr;
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

function Section({
  label,
  n,
  children,
}: {
  label: string;
  n: number;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <span className="flex h-4 w-4 items-center justify-center rounded-full bg-text-muted/20 text-[9px] font-bold text-text-muted">
          {n}
        </span>
        <h3 className="text-[10px] font-medium uppercase tracking-wider text-text-muted">
          {label}
        </h3>
      </div>
      <div className="space-y-2 pl-6">{children}</div>
    </div>
  );
}

function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <label className="block space-y-1">
      <span className="text-[10px] text-text-muted">{label}</span>
      {children}
    </label>
  );
}

function TeamSlotEditor({
  role,
  slot,
  setSlot,
  existing,
  frameworks,
  onRemove,
  projectName,
}: {
  role: "captain" | "talon";
  slot: TeamSlot;
  setSlot: (s: TeamSlot) => void;
  existing: AgentInstance[];
  frameworks: Framework[];
  onRemove?: () => void;
  projectName?: string;
}) {
  const Icon = role === "captain" ? Crown : Feather;
  const hasExisting = existing.length > 0;

  return (
    <div className="rounded border border-border bg-bg p-3 space-y-2">
      <div className="flex items-center gap-2 text-xs">
        <Icon className="h-3.5 w-3.5 text-purple" />
        <span className="font-medium text-text capitalize">{role}</span>
        <span className="text-[10px] text-text-muted">
          ({role === "captain" ? "coordinator" : "specialist"})
        </span>
        <div className="flex-1" />
        {onRemove && (
          <button
            onClick={onRemove}
            className="text-text-muted hover:text-red transition-colors"
            aria-label={`remove ${role}`}
          >
            <Trash2 className="h-3 w-3" />
          </button>
        )}
      </div>

      {/* Tab row: new / existing */}
      <div className="flex gap-3 text-[10px]">
        <button
          onClick={() =>
            setSlot({
              id: slot.id,
              kind: "new",
              name: slot.kind === "new" ? slot.name : "",
              framework:
                slot.kind === "new"
                  ? slot.framework
                  : frameworks[0]?.id || "",
            })
          }
          className={`pb-0.5 border-b-2 -mb-px ${
            slot.kind === "new"
              ? "border-accent text-text"
              : "border-transparent text-text-muted hover:text-text"
          }`}
        >
          + provision new
        </button>
        <button
          onClick={() => {
            if (!hasExisting) return;
            setSlot({ id: slot.id, kind: "existing", instanceId: existing[0].id });
          }}
          disabled={!hasExisting}
          className={`pb-0.5 border-b-2 -mb-px ${
            slot.kind === "existing"
              ? "border-accent text-text"
              : "border-transparent text-text-muted hover:text-text disabled:opacity-40 disabled:cursor-not-allowed"
          }`}
          title={!hasExisting ? "no existing agents available" : undefined}
        >
          use existing
        </button>
      </div>

      {slot.kind === "new" ? (
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
          <input
            value={slot.name}
            onChange={(e) => setSlot({ ...slot, name: e.target.value })}
            placeholder={projectName ? `${projectName.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "")}-${role}` : `${role}-name`}
            className="rounded border border-border bg-bg px-2 py-1.5 text-xs text-text focus:border-accent focus:outline-none"
          />
          <select
            value={slot.framework}
            onChange={(e) => setSlot({ ...slot, framework: e.target.value })}
            className="rounded border border-border bg-bg px-2 py-1.5 text-xs text-text focus:border-accent focus:outline-none"
          >
            {frameworks.map((fw) => (
              <option key={fw.id} value={fw.id}>
                {fw.name}
              </option>
            ))}
          </select>
        </div>
      ) : (
        <select
          value={slot.instanceId}
          onChange={(e) => setSlot({ ...slot, instanceId: e.target.value })}
          className="w-full rounded border border-border bg-bg px-2 py-1.5 text-xs text-text focus:border-accent focus:outline-none"
        >
          {existing.map((inst) => (
            <option key={inst.id} value={inst.id}>
              {inst.display_name || inst.name}
            </option>
          ))}
        </select>
      )}
    </div>
  );
}
