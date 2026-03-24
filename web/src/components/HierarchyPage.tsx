import { useState, useEffect, useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { Plus, RefreshCw, Crown, Briefcase, User, ChevronRight } from "lucide-react";
import type { HierarchyTree, AgentInstance, CommanderInfo, ProjectTree, Persona, AgentInfo } from "../lib/types";
import { fetchHierarchy, fetchPersonas, fetchFrameworks, fetchAgents, createInstance, setCommander } from "../lib/api";
import type { Framework } from "../lib/types";

function StatusDot({ status }: { status: string }) {
  const color = status === "running" ? "bg-green" : status === "error" ? "bg-red" : "bg-text-muted";
  return <span className={`inline-block h-1.5 w-1.5 rounded-full ${color}`} />;
}

function RoleBadge({ role }: { role?: string }) {
  switch (role) {
    case "commander":
      return (
        <span className="inline-flex items-center gap-1 rounded bg-accent/10 px-1.5 py-0.5 text-[10px] font-medium text-accent">
          <Crown className="h-2.5 w-2.5" /> commander
        </span>
      );
    case "captain":
      return (
        <span className="inline-flex items-center gap-1 rounded bg-green/10 px-1.5 py-0.5 text-[10px] font-medium text-green">
          <Briefcase className="h-2.5 w-2.5" /> captain
        </span>
      );
    case "talon":
      return (
        <span className="inline-flex items-center gap-1 rounded bg-text-muted/10 px-1.5 py-0.5 text-[10px] font-medium text-text-secondary">
          <User className="h-2.5 w-2.5" /> talon
        </span>
      );
    default:
      return null;
  }
}

function InstanceCard({ instance, onClick }: { instance: AgentInstance | CommanderInfo; onClick: () => void }) {
  const displayName = instance.display_name || instance.name;
  const role = instance.hierarchy_role || undefined;
  return (
    <button
      onClick={onClick}
      className="flex w-full items-center gap-3 rounded border border-border bg-surface px-4 py-3 text-left text-xs transition-all hover:border-accent/50 hover:bg-surface-hover/50"
    >
      <StatusDot status={instance.status} />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="font-medium text-text">{displayName}</span>
          <RoleBadge role={role} />
          {"legacy" in instance && instance.legacy && (
            <span className="rounded bg-text-muted/10 px-1.5 py-0.5 text-[10px] text-text-muted">existing agent</span>
          )}
        </div>
        <div className="mt-0.5 text-text-muted truncate">
          {instance.framework} · :{instance.port}
        </div>
      </div>
      <ChevronRight className="h-3.5 w-3.5 text-text-muted" />
    </button>
  );
}

function ProjectSection({ tree, onInstanceClick, onProjectClick }: { tree: ProjectTree; onInstanceClick: (name: string) => void; onProjectClick: (id: string) => void }) {
  return (
    <div className="rounded border border-border bg-surface/50 p-4 space-y-3">
      <button
        onClick={() => onProjectClick(tree.project.id)}
        className="w-full text-left group"
      >
        <div className="flex items-center gap-2">
          <Briefcase className="h-3.5 w-3.5 text-green" />
          <span className="font-medium text-text text-sm group-hover:text-accent transition-colors">{tree.project.name}</span>
          <span className="rounded bg-green/10 px-1.5 py-0.5 text-[10px] font-medium text-green">
            {tree.project.status}
          </span>
          <ChevronRight className="ml-auto h-3.5 w-3.5 text-text-muted opacity-0 group-hover:opacity-100 transition-opacity" />
        </div>
        {tree.project.description && (
          <p className="mt-1 text-xs text-text-muted ml-5">{tree.project.description}</p>
        )}
      </button>

      {/* Captain */}
      <div className="ml-5">
        <span className="text-[10px] font-medium uppercase tracking-wider text-text-muted">captain</span>
        {tree.captain ? (
          <div className="mt-1">
            <InstanceCard
              instance={tree.captain}
              onClick={() => onInstanceClick(tree.captain!.name)}
            />
          </div>
        ) : (
          <p className="mt-1 text-xs text-text-muted italic">none assigned</p>
        )}
      </div>

      {/* Talons */}
      <div className="ml-5">
        <span className="text-[10px] font-medium uppercase tracking-wider text-text-muted">
          talons {tree.talons.length > 0 && `(${tree.talons.length})`}
        </span>
        {tree.talons.length > 0 ? (
          <div className="mt-1 ml-5 space-y-1.5">
            {tree.talons.map((agent) => (
              <InstanceCard
                key={agent.id}
                instance={agent}
                onClick={() => onInstanceClick(agent.name)}
              />
            ))}
          </div>
        ) : (
          <p className="mt-1 text-xs text-text-muted italic">none assigned</p>
        )}
      </div>
    </div>
  );
}

// --- Coordinator Setup ---

function CommanderSetup({ onCreated }: { onCreated: () => void }) {
  const navigate = useNavigate();
  const [mode, setMode] = useState<"choose" | "existing" | "new">("choose");
  const [agents, setAgents] = useState<AgentInfo[]>([]);
  const [frameworks, setFrameworks] = useState<Framework[]>([]);
  const [personas, setPersonas] = useState<Persona[]>([]);
  const [saving, setSaving] = useState(false);
  const [savingStatus, setSavingStatus] = useState("");
  const [error, setError] = useState("");
  const [loadError, setLoadError] = useState<string | null>(null);
  const [loadingAgents, setLoadingAgents] = useState(true);

  // "new" form state
  const [name, setName] = useState("atlas");
  const [framework, setFramework] = useState("openclaw");
  const [personaId, setPersonaId] = useState("");

  const loadData = useCallback(async () => {
    setLoadError(null);
    const [agentsResult, frameworksResult, personasResult] = await Promise.allSettled([
      fetchAgents(),
      fetchFrameworks(),
      fetchPersonas(),
    ]);

    if (agentsResult.status === "fulfilled") setAgents(agentsResult.value);
    if (frameworksResult.status === "fulfilled") setFrameworks(frameworksResult.value);
    if (personasResult.status === "fulfilled") setPersonas(personasResult.value);

    const errors: string[] = [];
    if (agentsResult.status === "rejected") {
      console.error("Failed to discover agents:", agentsResult.reason);
      errors.push(agentsResult.reason instanceof Error ? agentsResult.reason.message : "Failed to discover agents");
    }
    if (frameworksResult.status === "rejected") {
      console.error("Failed to fetch frameworks:", frameworksResult.reason);
      errors.push(frameworksResult.reason instanceof Error ? frameworksResult.reason.message : "Failed to fetch frameworks");
    }
    if (personasResult.status === "rejected") {
      console.error("Failed to fetch personas:", personasResult.reason);
      errors.push(personasResult.reason instanceof Error ? personasResult.reason.message : "Failed to fetch personas");
    }

    setLoadError(errors.length > 0 ? errors.join("; ") : null);
    setLoadingAgents(false);
  }, []);

  useEffect(() => {
    loadData();
  }, [loadData]);

  // Sync default framework selection to an actually-installed framework
  useEffect(() => {
    const installedIds = frameworks.filter((f) => f.installed).map((f) => f.id);
    if (installedIds.length > 0 && !installedIds.includes(framework)) {
      setFramework(installedIds[0]);
    }
  }, [frameworks, framework]);

  const handleSelectExisting = async (agentName: string) => {
    setSaving(true);
    setSavingStatus("setting commander...");
    setError("");
    try {
      await setCommander({ agentName });
      onCreated();
      // Navigate to chat with briefing flag — the chat page will send the briefing
      navigate(`/agents/${agentName}/chat?brief=commander`);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to set commander");
      setSaving(false);
    }
  };

  const handleCreateNew = async () => {
    setSaving(true);
    setSavingStatus("creating agent instance...");
    setError("");
    try {
      const inst = await createInstance({
        name,
        framework,
        persona_id: personaId || undefined,
        hierarchy_role: "commander",
        auto_start: true,
      });
      await setCommander({ instanceId: inst.id });
      onCreated();
      navigate(`/agents/${inst.name}/chat?brief=commander`);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to create commander");
      setSaving(false);
    }
  };

  const runningAgents = agents.filter((a) => a.alive);
  const installedFrameworks = frameworks.filter((f) => f.installed);

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-lg font-bold text-text">
          <span className="text-accent">&gt;</span> setup commander
        </h2>
        <p className="mt-1 text-xs text-text-muted">
          the commander is your master agent — it manages all your projects and helps you grow your agent team
        </p>
      </div>

      {error && (
        <div className="rounded border border-red/30 bg-red/5 px-3 py-2 text-xs text-red">
          {error}
        </div>
      )}

      {loadError && (
        <div className="rounded border border-red/30 bg-red/5 px-3 py-2 text-xs text-red flex items-center justify-between">
          <span>{loadError}</span>
          <button
            onClick={loadData}
            className="ml-3 shrink-0 rounded bg-red/10 px-2 py-1 text-[10px] font-medium text-red hover:bg-red/20 transition-colors"
          >
            retry
          </button>
        </div>
      )}

      {mode === "choose" && (
        <div className="space-y-3">
          <div
            onClick={() => runningAgents.length > 0 && setMode("existing")}
            role={runningAgents.length > 0 ? "button" : undefined}
            tabIndex={runningAgents.length > 0 ? 0 : undefined}
            onKeyDown={runningAgents.length > 0 ? (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); setMode("existing"); } } : undefined}
            className={`flex w-full items-center gap-4 rounded border border-border bg-surface p-5 text-left transition-all ${
              runningAgents.length > 0 ? "hover:border-accent/50 hover:bg-surface-hover/50 cursor-pointer" : "opacity-50"
            }`}
          >
            <div className="flex h-10 w-10 items-center justify-center rounded-full bg-green/10">
              <Crown className="h-5 w-5 text-green" />
            </div>
            <div>
              <div className="text-sm font-medium text-text">use an existing agent</div>
              <div className="mt-0.5 text-xs text-text-muted">
                {runningAgents.length > 0
                  ? `promote one of your ${runningAgents.length} running agent${runningAgents.length !== 1 ? "s" : ""} to commander`
                  : loadError ? "failed to discover agents" : loadingAgents ? "discovering running agents..." : "no running agents found"}
              </div>
            </div>
            {runningAgents.length > 0 ? (
              <ChevronRight className="ml-auto h-4 w-4 text-text-muted" />
            ) : loadError ? (
              <button
                onClick={(e) => { e.stopPropagation(); loadData(); }}
                className="ml-auto text-[10px] text-red hover:text-red/80 cursor-pointer"
              >
                retry
              </button>
            ) : (
              <span className="ml-auto h-3 w-3 animate-spin rounded-full border-2 border-text-muted/30 border-t-text-muted" />
            )}
          </div>

          <button
            onClick={() => setMode("new")}
            className="flex w-full items-center gap-4 rounded border border-border bg-surface p-5 text-left transition-all hover:border-accent/50 hover:bg-surface-hover/50"
          >
            <div className="flex h-10 w-10 items-center justify-center rounded-full bg-accent/10">
              <Plus className="h-5 w-5 text-accent" />
            </div>
            <div>
              <div className="text-sm font-medium text-text">create a new agent</div>
              <div className="mt-0.5 text-xs text-text-muted">
                provision a dedicated commander with its own identity and workspace
              </div>
            </div>
            <ChevronRight className="ml-auto h-4 w-4 text-text-muted" />
          </button>
        </div>
      )}

      {mode === "existing" && (
        <div className="space-y-3">
          <button
            onClick={() => setMode("choose")}
            className="text-xs text-text-muted hover:text-text"
          >
            &larr; back
          </button>
          <div className="text-xs font-medium text-text-secondary">select an agent to be your commander</div>
          <div className="space-y-1.5">
            {agents.length === 0 && (
              loadError ? (
                <div className="flex items-center justify-between rounded border border-red/30 bg-red/5 px-4 py-3">
                  <span className="text-xs text-red">failed to discover agents</span>
                  <button
                    onClick={loadData}
                    className="rounded bg-red/10 px-2 py-1 text-[10px] font-medium text-red hover:bg-red/20 transition-colors"
                  >
                    retry
                  </button>
                </div>
              ) : loadingAgents ? (
                <div className="flex items-center gap-3 rounded border border-border bg-surface px-4 py-3 opacity-50">
                  <span className="h-1.5 w-1.5 rounded-full bg-text-muted animate-pulse" />
                  <span className="text-xs text-text-muted">discovering agents...</span>
                </div>
              ) : (
                <div className="rounded border border-border bg-surface px-4 py-3 text-center text-xs text-text-muted">
                  no running agents found
                </div>
              )
            )}
            {runningAgents.map((agent) => {
              const canBeCommander = agent.commander_capable;
              return (
                <button
                  key={agent.name}
                  onClick={() => canBeCommander && handleSelectExisting(agent.name)}
                  disabled={saving || !canBeCommander}
                  className={`flex w-full items-center gap-3 rounded border border-border bg-surface px-4 py-3 text-left text-xs transition-all disabled:opacity-50 ${
                    canBeCommander ? "hover:border-green/50 hover:bg-surface-hover/50" : "cursor-not-allowed"
                  }`}
                >
                  <span className="h-1.5 w-1.5 rounded-full bg-green" />
                  <div className="flex-1">
                    <span className="font-medium text-text">{agent.name}</span>
                    <span className="ml-2 text-text-muted">{agent.framework} · :{agent.port}</span>
                  </div>
                  {canBeCommander ? (
                    <span className="rounded bg-green/10 px-1.5 py-0.5 text-[10px] font-medium text-green">running</span>
                  ) : (
                    <span className="rounded bg-text-muted/10 px-1.5 py-0.5 text-[10px] text-text-muted">talon only</span>
                  )}
                </button>
              );
            })}
          </div>
          {saving && savingStatus && (
            <div className="flex items-center gap-2 rounded border border-purple/20 bg-purple/5 px-3 py-2 text-xs text-purple">
              <span className="inline-block h-3 w-3 animate-spin rounded-full border-2 border-purple/30 border-t-purple" />
              {savingStatus}
            </div>
          )}
        </div>
      )}

      {mode === "new" && (
        <div className="space-y-4">
          <button
            onClick={() => setMode("choose")}
            className="text-xs text-text-muted hover:text-text"
          >
            &larr; back
          </button>

          <div className="space-y-4 rounded border border-border bg-surface p-6">
            <div>
              <label className="block text-xs font-medium text-text-secondary mb-1.5">name</label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="w-full rounded border border-border bg-bg px-3 py-2 text-xs text-text focus:border-accent focus:outline-none"
                placeholder="atlas"
              />
            </div>

            <div>
              <label className="block text-xs font-medium text-text-secondary mb-1.5">framework</label>
              <select
                value={framework}
                onChange={(e) => setFramework(e.target.value)}
                className="w-full rounded border border-border bg-bg px-3 py-2 text-xs text-text focus:border-accent focus:outline-none"
              >
                {installedFrameworks.length > 0
                  ? installedFrameworks.map((f) => (
                      <option key={f.id} value={f.id}>{f.name}</option>
                    ))
                  : (
                    <option value="" disabled>no frameworks installed</option>
                  )}
              </select>
            </div>

            <div>
              <label className="block text-xs font-medium text-text-secondary mb-1.5">persona (optional)</label>
              <select
                value={personaId}
                onChange={(e) => setPersonaId(e.target.value)}
                className="w-full rounded border border-border bg-bg px-3 py-2 text-xs text-text focus:border-accent focus:outline-none"
              >
                <option value="">default commander</option>
                {personas.map((p) => (
                  <option key={p.id} value={p.id}>{p.icon} {p.name}</option>
                ))}
              </select>
            </div>

            <button
              onClick={handleCreateNew}
              disabled={saving || !name || installedFrameworks.length === 0}
              className="rounded bg-accent px-4 py-2 text-xs font-medium text-white transition-colors hover:bg-accent/80 disabled:opacity-50"
            >
              {saving ? "creating..." : "create commander"}
            </button>
          </div>
        </div>
      )}

    </div>
  );
}

// --- Main Page ---

export default function HierarchyPage() {
  const navigate = useNavigate();
  const [hierarchy, setHierarchy] = useState<HierarchyTree | null>(null);
  const [loading, setLoading] = useState(true);
  const [fetchError, setFetchError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      setFetchError(null);
      setLoading(true);
      const data = await fetchHierarchy();
      setHierarchy(data);
    } catch (e) {
      const msg = e instanceof Error ? e.message : "Failed to fetch hierarchy";
      setHierarchy((prev) => {
        if (prev === null) setFetchError(msg);
        return prev;
      });
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
    const interval = setInterval(refresh, 15000);
    return () => clearInterval(interval);
  }, [refresh]);

  const handleInstanceClick = (name: string) => {
    navigate(`/agents/${name}`);
  };

  if (loading && !hierarchy) {
    return (
      <div className="py-20 text-center text-xs text-text-muted">
        loading hierarchy...
      </div>
    );
  }

  if (fetchError) {
    return (
      <div className="py-20 text-center space-y-3">
        <div className="rounded border border-red/30 bg-red/5 px-4 py-3 text-xs text-red inline-block">
          {fetchError}
        </div>
        <div>
          <button
            onClick={refresh}
            disabled={loading}
            className="text-xs text-text-muted hover:text-text transition-colors disabled:opacity-50"
          >
            <RefreshCw className={`inline h-3 w-3 mr-1 ${loading ? "animate-spin" : ""}`} />
            retry
          </button>
        </div>
      </div>
    );
  }

  // No commander set up yet — show setup wizard
  if (!hierarchy?.commander) {
    return <CommanderSetup onCreated={refresh} />;
  }

  return (
    <div className="space-y-6">
      <div className="text-xs text-text-muted">~/hierarchy</div>

      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-bold">
            <span className="text-accent">&gt;</span> agent hierarchy
          </h1>
          <p className="mt-1 text-xs text-text-muted">
            commander &rarr; captains &rarr; talons
          </p>
        </div>
        <button
          onClick={refresh}
          disabled={loading}
          className="flex items-center gap-2 text-xs text-text-muted transition-colors hover:text-text disabled:opacity-50"
        >
          <RefreshCw className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} />
          $ refresh
        </button>
      </div>

      {/* Role descriptions */}
      <div className="grid grid-cols-3 gap-3">
        <div className="rounded border border-accent/20 bg-accent/5 p-3 space-y-1">
          <div className="flex items-center gap-1.5">
            <Crown className="h-3 w-3 text-accent" />
            <span className="text-[10px] font-bold text-accent uppercase tracking-wider">commander</span>
          </div>
          <p className="text-[10px] text-text-muted leading-relaxed">
            your executive. creates projects, assigns captains, tracks cross-project progress. your primary point of contact.
          </p>
        </div>
        <div className="rounded border border-green/20 bg-green/5 p-3 space-y-1">
          <div className="flex items-center gap-1.5">
            <Briefcase className="h-3 w-3 text-green" />
            <span className="text-[10px] font-bold text-green uppercase tracking-wider">captain</span>
          </div>
          <p className="text-[10px] text-text-muted leading-relaxed">
            your tech lead. owns a project end-to-end — plans work, creates talons, coordinates the team. reports to the commander.
          </p>
        </div>
        <div className="rounded border border-border bg-surface/50 p-3 space-y-1">
          <div className="flex items-center gap-1.5">
            <User className="h-3 w-3 text-text-secondary" />
            <span className="text-[10px] font-bold text-text-secondary uppercase tracking-wider">talon</span>
          </div>
          <p className="text-[10px] text-text-muted leading-relaxed">
            a specialist. focused on one role — researcher, developer, writer, etc. created and managed by the captain.
          </p>
        </div>
      </div>

      {/* Commander */}
      <div>
        <div className="mb-2 text-[10px] font-medium uppercase tracking-wider text-text-muted">
          commander
        </div>
        <InstanceCard
          instance={hierarchy.commander}
          onClick={() => navigate(`/agents/${hierarchy.commander!.name}/chat`)}
        />
      </div>

      {/* Captains */}
      {(() => {
        const allCaptains = hierarchy.projects
          .filter((t) => t.captain)
          .map((t) => ({ instance: t.captain!, projectName: t.project.name }));
        return (
          <div>
            <div className="mb-2 text-[10px] font-medium uppercase tracking-wider text-text-muted">
              captains ({allCaptains.length})
            </div>
            {allCaptains.length === 0 ? (
              <div className="rounded border border-border bg-surface p-4 text-center text-xs text-text-muted">
                no captains yet — assign one to a project
              </div>
            ) : (
              <div className="space-y-1.5">
                {allCaptains.map(({ instance, projectName }) => (
                  <div key={instance.id} className="flex items-center gap-2">
                    <div className="flex-1">
                      <InstanceCard
                        instance={instance}
                        onClick={() => handleInstanceClick(instance.name)}
                      />
                    </div>
                    <span className="shrink-0 text-[10px] text-text-muted">{projectName}</span>
                  </div>
                ))}
              </div>
            )}
          </div>
        );
      })()}

      {/* Talons */}
      {(() => {
        const allTalons = hierarchy.projects.flatMap((t) =>
          t.talons.map((talon) => ({ instance: talon, projectName: t.project.name }))
        );
        return (
          <div>
            <div className="mb-2 text-[10px] font-medium uppercase tracking-wider text-text-muted">
              talons ({allTalons.length})
            </div>
            {allTalons.length === 0 ? (
              <div className="rounded border border-border bg-surface p-4 text-center text-xs text-text-muted">
                no talons yet — add agents to your projects
              </div>
            ) : (
              <div className="space-y-1.5">
                {allTalons.map(({ instance, projectName }) => (
                  <div key={instance.id} className="flex items-center gap-2">
                    <div className="flex-1">
                      <InstanceCard
                        instance={instance}
                        onClick={() => handleInstanceClick(instance.name)}
                      />
                    </div>
                    <span className="shrink-0 text-[10px] text-text-muted">{projectName}</span>
                  </div>
                ))}
              </div>
            )}
          </div>
        );
      })()}

      {/* Projects */}
      <div>
        <div className="mb-2 flex items-center justify-between">
          <span className="text-[10px] font-medium uppercase tracking-wider text-text-muted">
            projects ({hierarchy.projects.length})
          </span>
          <button
            onClick={() => navigate(`/agents/${hierarchy.commander!.name}/chat`)}
            className="flex items-center gap-1 text-xs text-accent transition-colors hover:text-accent/80"
            title="start a conversation with your commander to plan a new project"
          >
            <Plus className="h-3 w-3" /> new project
          </button>
        </div>

        {hierarchy.projects.length === 0 ? (
          <div className="rounded border border-border bg-surface p-6 text-center text-xs text-text-muted space-y-3">
            <p>no projects yet — talk to your commander to plan one</p>
            <button
              onClick={() => navigate(`/agents/${hierarchy.commander!.name}/chat`)}
              className="rounded bg-accent px-4 py-2 text-xs font-medium text-white transition-colors hover:bg-accent/80"
            >
              chat with {hierarchy.commander!.display_name || hierarchy.commander!.name}
            </button>
          </div>
        ) : (
          <div className="space-y-3">
            {hierarchy.projects.map((tree) => (
              <ProjectSection
                key={tree.project.id}
                tree={tree}
                onInstanceClick={handleInstanceClick}
                onProjectClick={(id) => navigate(`/projects/${id}`)}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
