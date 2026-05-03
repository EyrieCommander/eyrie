import { useState, useEffect, useMemo, useRef, useCallback } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { BarChart3, Bird, Briefcase, Bot, ChevronDown, ChevronRight, Crown, LayoutDashboard, Layers, Network, Settings, Users, Wind } from "lucide-react";
import { useData } from "../lib/DataContext";
import { FRAMEWORK_EMOJI } from "../lib/types";
import { fetchFrameworks } from "../lib/api";
import { useZoom } from "../lib/useZoom";
import ZoomSlider from "./ZoomSlider";

function useSortOrder<T extends { id: string }>(key: string, items: T[]) {
  const [order, setOrder] = useState<string[]>(() => {
    try { const s = localStorage.getItem(key); return s ? JSON.parse(s) : []; } catch { return []; }
  });

  const sorted = useMemo(() => {
    const pos = new Map(order.map((id, i) => [id, i]));
    return [...items].sort((a, b) => {
      const pa = pos.get(a.id) ?? Infinity;
      const pb = pos.get(b.id) ?? Infinity;
      return pa - pb;
    });
  }, [items, order]);

  const reorder = useCallback((fromId: string, toId: string) => {
    if (fromId === toId) return;
    const ids = sorted.map((i) => i.id);
    const fromIdx = ids.indexOf(fromId);
    const toIdx = ids.indexOf(toId);
    if (fromIdx < 0 || toIdx < 0) return;
    ids.splice(fromIdx, 1);
    ids.splice(toIdx, 0, fromId);
    setOrder(ids);
    try { localStorage.setItem(key, JSON.stringify(ids)); } catch {}
  }, [key, sorted]);

  return { sorted, reorder };
}


function parseAgentRoute(pathname: string) {
  const match = pathname.match(/^\/agents\/([^/]+?)(?:\/(status|chat|logs|config))?$/);
  if (!match || match[1] === "overview") return null;
  return match[1];
}

function parseProjectRoute(pathname: string) {
  const match = pathname.match(/^\/projects\/([^/]+)/);
  return match ? match[1] : null;
}

export default function Sidebar() {
  const { agents, projects, instances, pendingActions } = useData();
  const { pathname } = useLocation();
  const navigate = useNavigate();
  const { zoom, setZoom, reset: resetZoom, min, max, step } = useZoom();
  const activeAgent = useMemo(() => parseAgentRoute(pathname), [pathname]);
  const { sorted: sortedProjects, reorder: reorderProjects } = useSortOrder("eyrie-project-order", projects);
  const [dragOverId, setDragOverId] = useState<string | null>(null);
  const dragIdRef = useRef<string | null>(null);
  const transparentImg = useRef<HTMLImageElement | null>(null);
  const activeProject = useMemo(() => parseProjectRoute(pathname), [pathname]);

  // Installed frameworks from the registry — shows frameworks in the
  // sidebar even when no agent is running (discovery only returns running ones).
  const [installedFrameworks, setInstalledFrameworks] = useState<string[]>([]);
  useEffect(() => {
    let cancelled = false;
    const load = () => {
      fetchFrameworks()
        .then((fws) => {
          if (cancelled) return;
          const ids = fws.filter((fw) => fw.installed).map((fw) => fw.id);
          setInstalledFrameworks((prev) =>
            JSON.stringify(prev) === JSON.stringify(ids) ? prev : ids,
          );
        })
        .catch(() => {});
    };
    load();
    const id = setInterval(load, 30_000);
    return () => { cancelled = true; clearInterval(id); };
  }, []);

  // Create a 1x1 transparent image for drag operations to prevent
  // Chrome's split-view suggestion that appears when dragging <a> tags
  useEffect(() => {
    const img = new Image(1, 1);
    img.src = "data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7";
    transparentImg.current = img;
  }, []);

  const [missionControlExpanded, setMissionControlExpanded] = useState(true);
  const [agentsExpanded, setAgentsExpanded] = useState(true);
  const [projectsExpanded, setProjectsExpanded] = useState(true);
  const [frameworksExpanded, setFrameworksExpanded] = useState(true);
  const [windMode, setWindMode] = useState(false);
  const prevPendingRef = useRef(0);
  const windTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Bird flies away when an agent action starts
  useEffect(() => {
    const count = Object.keys(pendingActions).length;
    if (count > prevPendingRef.current && !windTimerRef.current) {
      setWindMode(true);
      windTimerRef.current = setTimeout(() => {
        setWindMode(false);
        windTimerRef.current = null;
      }, 2000);
    }
    prevPendingRef.current = count;
    return () => {
      if (windTimerRef.current) {
        clearTimeout(windTimerRef.current);
        windTimerRef.current = null;
      }
    };
  }, [pendingActions]);

  const activeFramework = pathname.startsWith("/frameworks/") ? pathname.split("/")[2] : null;

  useEffect(() => {
    if (activeFramework) setFrameworksExpanded(true);
  }, [activeFramework]);

  useEffect(() => {
    if (activeAgent) setAgentsExpanded(true);
  }, [activeAgent]);

  useEffect(() => {
    if (activeProject) setProjectsExpanded(true);
  }, [activeProject]);

  return (
    <aside className="flex h-screen w-56 shrink-0 flex-col bg-bg-sidebar border-r border-border">
      <div className="px-5 pt-7 pb-6">
        <Link to="/" className="flex items-center gap-2 hover:opacity-80 transition-opacity">
          <span className="relative h-5 w-5">
            <Bird className={`h-5 w-5 text-accent absolute inset-0 transition-all duration-500 ${windMode ? "opacity-0 translate-x-3 -translate-y-2 scale-75" : "opacity-100"}`} />
            <Wind className={`h-5 w-5 text-accent absolute inset-0 transition-all duration-500 ${windMode ? "opacity-100" : "opacity-0 -translate-x-2 scale-75"}`} />
          </span>
          <span className="text-base font-bold text-text">eyrie</span>
        </Link>
      </div>

      <nav className="flex-1 overflow-y-auto px-3 space-y-0.5">
        {/* ── Mission Control ── */}
        <div className={`flex items-center rounded text-xs transition-colors ${
            pathname.startsWith("/mission-control")
              ? "bg-surface-hover text-text"
              : "text-text-secondary hover:bg-surface-hover/50"
          }`}>
          <Link
            to="/mission-control"
            className="flex flex-1 items-center gap-2 px-3 py-1.5"
          >
            <LayoutDashboard className="h-3.5 w-3.5" />
            <span className="font-medium">mission control</span>
          </Link>
          <button
            onClick={() => setMissionControlExpanded((prev) => !prev)}
            aria-expanded={missionControlExpanded}
            aria-controls="mission-control-list"
            aria-label={missionControlExpanded ? "Collapse mission control" : "Expand mission control"}
            className="px-3 py-1.5 hover:text-text transition-colors"
          >
            {missionControlExpanded ? (
              <ChevronDown className="h-3 w-3 text-green" />
            ) : (
              <ChevronRight className="h-3 w-3 text-text-muted" />
            )}
          </button>
        </div>

        {missionControlExpanded && (
          <div id="mission-control-list" className="ml-4 border-l border-border pl-2 space-y-px">
            <Link
              to="/mission-control/agents"
              className={`flex items-center gap-2 rounded px-3 py-1.5 text-xs transition-colors ${
                pathname === "/mission-control/agents"
                  ? "bg-surface-hover text-accent font-medium"
                  : "text-text-secondary hover:text-text hover:bg-surface-hover/50"
              }`}
            >
              <Crown className="h-3 w-3" />
              <span>hierarchy</span>
            </Link>
            <Link
              to="/agents/compare"
              className={`flex items-center gap-2 rounded px-3 py-1.5 text-xs transition-colors ${
                pathname === "/agents/compare"
                  ? "bg-surface-hover text-accent font-medium"
                  : "text-text-secondary hover:text-text hover:bg-surface-hover/50"
              }`}
            >
              <BarChart3 className="h-3 w-3" />
              <span>compare agents</span>
            </Link>
            <Link
              to="/mission-control/mesh"
              className={`flex items-center gap-2 rounded px-3 py-1.5 text-xs transition-colors ${
                pathname === "/mission-control/mesh"
                  ? "bg-surface-hover text-accent font-medium"
                  : "text-text-secondary hover:text-text hover:bg-surface-hover/50"
              }`}
            >
              <Network className="h-3 w-3" />
              <span>mesh status</span>
            </Link>
          </div>
        )}

        {/* ── Frameworks ── */}
        {(() => {
          // Merge frameworks with running agents + installed frameworks from registry
          const fromAgents = agents.map((a) => a.framework);
          const frameworks = [...new Set([...installedFrameworks, ...fromAgents])];
          return (
            <>
              <div className={`flex items-center rounded text-xs transition-colors ${
                pathname.startsWith("/frameworks")
                  ? "bg-surface-hover text-text"
                  : "text-text-secondary hover:bg-surface-hover/50"
              }`}>
                <Link
                  to="/frameworks"
                  className="flex flex-1 items-center gap-2 px-3 py-1.5"
                >
                  <Layers className="h-3.5 w-3.5" />
                  <span className="font-medium">frameworks</span>
                </Link>
                <button
                  onClick={() => setFrameworksExpanded((prev) => !prev)}
                  aria-expanded={frameworksExpanded}
                  aria-controls="frameworks-list"
                  aria-label={frameworksExpanded ? "Collapse frameworks" : "Expand frameworks"}
                  className="px-3 py-1.5 hover:text-text transition-colors"
                >
                  {frameworksExpanded ? (
                    <ChevronDown className="h-3 w-3 text-green" />
                  ) : (
                    <ChevronRight className="h-3 w-3 text-text-muted" />
                  )}
                </button>
              </div>
              {frameworksExpanded && (
                <div id="frameworks-list" className="ml-4 border-l border-border pl-2 space-y-px">
                  {frameworks.map((fw) => {
                    const fwAgents = agents.filter((a) => a.framework === fw);
                    const aliveCount = fwAgents.filter((a) => a.alive).length;
                    const emoji = FRAMEWORK_EMOJI[fw] || "";
                    return (
                      <Link
                        key={fw}
                        to={`/frameworks/${fw}`}
                        className={`flex items-center gap-2 rounded px-3 py-1.5 text-xs transition-colors ${
                          pathname === `/frameworks/${fw}`
                            ? "bg-surface-hover text-accent font-medium"
                            : "text-text-secondary hover:text-text hover:bg-surface-hover/50"
                        }`}
                      >
                        <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${aliveCount > 0 ? "bg-green" : fwAgents.length > 0 ? "bg-red" : "bg-text-muted/30"}`} />
                        <span className="truncate">{fw} {emoji}</span>
                      </Link>
                    );
                  })}
                </div>
              )}
            </>
          );
        })()}

        {/* ── Agents ── */}
        <div className={`flex items-center rounded text-xs transition-colors ${
            pathname.startsWith("/agents/")
              ? "bg-surface-hover text-text"
              : "text-text-secondary hover:bg-surface-hover/50"
          }`}>
          <Link
            to="/agents/overview"
            className="flex flex-1 items-center gap-2 px-3 py-1.5"
          >
            <Bot className="h-3.5 w-3.5" />
            <span className="font-medium">agents</span>
          </Link>
          <button
            onClick={() => setAgentsExpanded((prev) => !prev)}
            aria-expanded={agentsExpanded}
            aria-controls="agents-list"
            aria-label={agentsExpanded ? "Collapse agents" : "Expand agents"}
            className="px-3 py-1.5 hover:text-text transition-colors"
          >
            {agentsExpanded ? (
              <ChevronDown className="h-3 w-3 text-green" />
            ) : (
              <ChevronRight className="h-3 w-3 text-text-muted" />
            )}
          </button>
        </div>

        {agentsExpanded && agents.length > 0 && (
          <div id="agents-list" className="ml-4 border-l border-border pl-2">
            {(() => {
              const nameCounts = new Map<string, number>();
              for (const a of agents) {
                const label = a.display_name || a.name;
                nameCounts.set(label, (nameCounts.get(label) || 0) + 1);
              }
              const roleMap = new Map<string, string>();
              for (const inst of instances) {
                if (inst.hierarchy_role) roleMap.set(inst.name, inst.hierarchy_role);
              }

              // Group agents by role: commander → captain → talon → standalone
              const roleOrder = ["commander", "captain", "talon", ""];
              const grouped = roleOrder.map((r) =>
                agents.filter((a) => (roleMap.get(a.name) || "") === r)
              ).filter((g) => g.length > 0);

              const renderAgent = (agent: typeof agents[0]) => {
                const isActive = activeAgent === agent.name;
                const label = agent.display_name || agent.name;
                const needsDisambig = (nameCounts.get(label) || 0) > 1;
                return (
                  <Link
                    key={agent.name}
                    to={`/agents/${agent.name}/chat`}
                    className={`flex items-center gap-2 rounded px-3 py-1.5 text-xs transition-colors ${
                      isActive
                        ? "bg-surface-hover text-accent font-medium"
                        : "text-text-secondary hover:text-text hover:bg-surface-hover/50"
                    }`}
                  >
                    <span
                      className={`h-1.5 w-1.5 shrink-0 rounded-full ${pendingActions[agent.name] ? "bg-yellow-400 animate-pulse" : !agent.alive ? "bg-red" : agent.status?.provider_status === "error" ? "bg-yellow" : "bg-green"}`}
                    />
                    <span className="flex-1 truncate">{needsDisambig ? `${label} (${agent.framework})` : label}</span>
                    <span className="shrink-0 text-[10px] leading-none">{FRAMEWORK_EMOJI[agent.framework] || ""}</span>
                  </Link>
                );
              };

              return grouped.map((group, gi) => (
                <div key={gi}>
                  {gi > 0 && <div className="my-1 mx-12 h-px bg-accent/30" />}
                  <div className="space-y-px">
                    {group.map(renderAgent)}
                  </div>
                </div>
              ));
            })()}
          </div>
        )}

        {/* ── Projects ── */}
        <div className={`flex items-center rounded text-xs transition-colors ${
            pathname.startsWith("/projects")
              ? "bg-surface-hover text-text"
              : "text-text-secondary hover:bg-surface-hover/50"
          }`}>
          <Link
            to="/projects"
            className="flex flex-1 items-center gap-2 px-3 py-1.5"
          >
            <Briefcase className="h-3.5 w-3.5" />
            <span className="font-medium">projects</span>
          </Link>
          <button
            onClick={() => setProjectsExpanded((prev) => !prev)}
            aria-expanded={projectsExpanded}
            aria-controls="projects-list"
            aria-label={projectsExpanded ? "Collapse projects" : "Expand projects"}
            className="px-3 py-1.5 hover:text-text transition-colors"
          >
            {projectsExpanded ? (
              <ChevronDown className="h-3 w-3 text-green" />
            ) : (
              <ChevronRight className="h-3 w-3 text-text-muted" />
            )}
          </button>
        </div>

        {projectsExpanded && sortedProjects.length > 0 && (
          <div id="projects-list" className="ml-4 border-l border-border pl-2 space-y-px">
            {sortedProjects.map((project) => {
              const isActive = activeProject === project.id;
              const isDragOver = dragOverId === project.id;
              return (
                <div
                  key={project.id}
                  role="button"
                  tabIndex={0}
                  onClick={() => navigate(`/projects/${project.id}`)}
                  onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); navigate(`/projects/${project.id}`); } }}
                  draggable
                  onDragStart={(e) => {
                    dragIdRef.current = project.id;
                    e.dataTransfer.effectAllowed = "move";
                    if (transparentImg.current) e.dataTransfer.setDragImage(transparentImg.current, 0, 0);
                  }}
                  onDragOver={(e) => {
                    e.preventDefault();
                    e.dataTransfer.dropEffect = "move";
                    setDragOverId(project.id);
                  }}
                  onDragLeave={() => setDragOverId(null)}
                  onDrop={(e) => {
                    e.preventDefault();
                    setDragOverId(null);
                    if (dragIdRef.current) reorderProjects(dragIdRef.current, project.id);
                    dragIdRef.current = null;
                  }}
                  onDragEnd={() => { setDragOverId(null); dragIdRef.current = null; }}
                  className={`group flex items-center gap-2 rounded px-3 py-1.5 text-xs cursor-pointer transition-colors ${
                    isDragOver
                      ? "border-t border-accent"
                      : isActive
                        ? "bg-surface-hover text-accent font-medium"
                        : "text-text-secondary hover:text-text hover:bg-surface-hover/50"
                  }`}
                >
                  <span
                    className={`h-1.5 w-1.5 shrink-0 rounded-full ${project.status === "active" ? "bg-green" : "bg-text-muted/30"}`}
                  />
                  <span className="truncate">{project.name}</span>
                </div>
              );
            })}
          </div>
        )}

        {/* ── Bottom items ── */}
        <div className="space-y-px">
          <Link
            to="/personas"
            className={`flex items-center gap-2 rounded px-3 py-2 text-xs transition-colors ${
              pathname === "/personas"
                ? "bg-surface-hover text-accent"
                : "text-text-secondary hover:text-text hover:bg-surface-hover/50"
            }`}
          >
            <Users className="h-3.5 w-3.5" />
            <span className="font-medium">personas</span>
          </Link>

          <Link
            to="/settings"
            className={`flex items-center gap-2 rounded px-3 py-2 text-xs transition-colors ${
              pathname === "/settings"
                ? "bg-surface-hover text-accent"
                : "text-text-secondary hover:text-text hover:bg-surface-hover/50"
            }`}
          >
            <Settings className="h-3.5 w-3.5" />
            <span className="font-medium">settings</span>
          </Link>
        </div>
      </nav>

      <ZoomSlider
        zoom={zoom}
        min={min}
        max={max}
        step={step}
        onChange={setZoom}
        onReset={resetZoom}
      />
    </aside>
  );
}
