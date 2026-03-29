import { useState, useEffect, useCallback, useRef } from "react";
import { useNavigate } from "react-router-dom";
import { ArrowLeft, Crown, User, Bot, RefreshCw } from "lucide-react";
import type { HierarchyTree } from "../lib/types";
import { fetchHierarchy } from "../lib/api";
import { useData } from "../lib/DataContext";
import { CommanderSetup } from "./CommanderSetup";

// ─── Agent Card ───

function AgentCard({ displayName, role, roleBadgeColor, roleIcon: RoleIcon, status, framework, project, onClick }: {
  displayName: string;
  role: string;
  roleBadgeColor: string;
  roleIcon: React.ComponentType<{ className?: string }>;
  status: string;
  framework: string;
  project?: string;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      className="flex w-full items-center gap-4 rounded border border-border p-4 text-left text-xs transition-all hover:border-accent/30 hover:bg-surface-hover/30"
    >
      <div className={`flex h-9 w-9 items-center justify-center rounded-full shrink-0 ${
        role === "commander" ? "bg-purple-500/20" : role === "captain" ? "bg-accent/10" : "bg-text-muted/10"
      }`}>
        <RoleIcon className={`h-4 w-4 ${
          role === "commander" ? "text-purple-400" : role === "captain" ? "text-accent" : "text-text-muted"
        }`} />
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className={`h-1.5 w-1.5 rounded-full shrink-0 ${status === "running" ? "bg-green" : "bg-text-muted"}`} />
          <span className="font-medium text-text truncate">{displayName}</span>
          <span className={`rounded px-1.5 py-0.5 text-[9px] font-medium shrink-0 ${roleBadgeColor}`}>{role}</span>
        </div>
        <div className="flex items-center gap-2 mt-1 text-text-muted">
          <span>{framework}</span>
          {project && <><span className="text-border">|</span><span className="truncate">{project}</span></>}
          <span className="text-border">|</span>
          <span>{status}</span>
        </div>
      </div>
    </button>
  );
}

// ─── Main Page ───

export default function AgentsPage() {
  const navigate = useNavigate();
  const { backendDown } = useData();
  const [hierarchy, setHierarchy] = useState<HierarchyTree | null>(null);
  const hierarchyRef = useRef<HierarchyTree | null>(null);
  const [loading, setLoading] = useState(true);
  const [changingCommander, setChangingCommander] = useState(false);

  const refresh = useCallback(async () => {
    try {
      setLoading(true);
      const data = await fetchHierarchy();
      setHierarchy(data);
      hierarchyRef.current = data;
    } catch {
      // silent — keep stale data
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

  if (loading && !hierarchy) {
    return <div className="py-20 text-center text-xs text-text-muted">loading agents...</div>;
  }

  if (!hierarchy?.commander) {
    return (
      <div className="space-y-6">
        <button onClick={() => navigate("/mission-control")} className="flex items-center gap-1.5 text-xs text-text-muted hover:text-text">
          <ArrowLeft className="h-3 w-3" /> mission control
        </button>
        <h2 className="text-lg font-bold text-text"><span className="text-accent">&gt;</span> setup commander</h2>
        <p className="text-xs text-text-muted">the commander is your master agent — it manages all your projects and helps you grow your agent team</p>
        <CommanderSetup onCreated={refresh} />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <button onClick={() => navigate("/mission-control")} className="flex items-center gap-1.5 text-xs text-text-muted hover:text-text">
        <ArrowLeft className="h-3 w-3" /> mission control
      </button>

      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-bold"><span className="text-accent">&gt;</span> agents</h1>
          <p className="mt-1 text-xs text-text-muted">// manage your agent hierarchy</p>
        </div>
        <button onClick={() => refresh()} disabled={loading}
          className="flex items-center gap-1.5 text-xs text-text-muted hover:text-text disabled:opacity-50">
          <RefreshCw className={`h-3 w-3 ${loading ? "animate-spin" : ""}`} /> refresh
        </button>
      </div>

      {/* Commander section */}
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <span className="text-[10px] font-medium uppercase tracking-wider text-text-muted">// commander</span>
          <button onClick={() => setChangingCommander(!changingCommander)}
            className="text-[10px] text-purple-400 hover:text-purple-300 transition-colors">
            {changingCommander ? "cancel" : "change commander"}
          </button>
        </div>

        {changingCommander ? (
          <div className="rounded border border-purple-400/30 bg-purple-400/5 p-4">
            <CommanderSetup onCreated={() => { setChangingCommander(false); refresh(); }} />
          </div>
        ) : (
          <AgentCard

            displayName={hierarchy.commander.display_name || hierarchy.commander.name}
            role="commander"
            roleBadgeColor="bg-purple-400/10 text-purple-400"
            roleIcon={Crown}
            status={hierarchy.commander.status}
            framework={hierarchy.commander.framework}
            onClick={() => navigate(`/agents/${hierarchy.commander!.name}`)}
          />
        )}
      </div>

      {/* Captains + Talons grouped by project */}
      {hierarchy.projects.length > 0 && (
        <div className="space-y-3">
          <span className="text-[10px] font-medium uppercase tracking-wider text-text-muted">
            // project agents ({hierarchy.projects.reduce((n, t) => n + (t.captain ? 1 : 0) + t.talons.length, 0)})
          </span>

          {hierarchy.projects.map((tree) => (
            <div key={tree.project.id} className="space-y-2">
              {/* Project label */}
              <div className="flex items-center gap-2 px-1">
                <span className={`h-1.5 w-1.5 rounded-full ${tree.project.status === "active" ? "bg-green" : "bg-text-muted"}`} />
                <button onClick={() => navigate(`/projects/${tree.project.id}`)}
                  className="text-xs font-medium text-text hover:text-accent transition-colors">
                  {tree.project.name}
                </button>
                {tree.project.goal && <span className="text-[10px] text-text-muted truncate">— {tree.project.goal}</span>}
              </div>

              {tree.captain && (
                <AgentCard

                  displayName={tree.captain.display_name || tree.captain.name}
                  role="captain"
                  roleBadgeColor="bg-accent/10 text-accent"
                  roleIcon={User}
                  status={tree.captain.status}
                  framework={tree.captain.framework}
                  project={tree.project.name}
                  onClick={() => navigate(`/agents/${tree.captain!.name}`)}
                />
              )}

              {tree.talons.map((talon) => (
                <div key={talon.id} className="ml-6">
                  <AgentCard

                    displayName={talon.display_name || talon.name}
                    role="talon"
                    roleBadgeColor="bg-text-muted/10 text-text-muted"
                    roleIcon={Bot}
                    status={talon.status}
                    framework={talon.framework}
                    project={tree.project.name}
                    onClick={() => navigate(`/agents/${talon.name}`)}
                  />
                </div>
              ))}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
