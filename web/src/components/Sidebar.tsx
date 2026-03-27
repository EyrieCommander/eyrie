import { useState, useEffect, useMemo } from "react";
import { Link, useLocation } from "react-router-dom";
import { Bird, Briefcase, Bot, ChevronDown, ChevronRight, Download, LayoutDashboard, Layers, Settings, Users } from "lucide-react";
import { useData } from "../lib/DataContext";
import { useZoom } from "../lib/useZoom";
import ZoomSlider from "./ZoomSlider";

const FRAMEWORK_EMOJI: Record<string, string> = {
  zeroclaw: "🌀",
  openclaw: "🦞",
  hermes: "🔱",
};

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
  const { agents, projects, pendingActions } = useData();
  const { pathname } = useLocation();
  const { zoom, setZoom, reset: resetZoom, min, max, step } = useZoom();
  const activeAgent = useMemo(() => parseAgentRoute(pathname), [pathname]);
  const activeProject = useMemo(() => parseProjectRoute(pathname), [pathname]);

  const [agentsExpanded, setAgentsExpanded] = useState(true);
  const [projectsExpanded, setProjectsExpanded] = useState(true);
  const [frameworksExpanded, setFrameworksExpanded] = useState(true);

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
        <Link to="/agents/overview" className="flex items-center gap-2 hover:opacity-80 transition-opacity">
          <Bird className="h-5 w-5 text-accent" />
          <span className="text-base font-bold text-text">eyrie</span>
        </Link>
      </div>

      <nav className="flex-1 overflow-y-auto px-3 space-y-0.5">
        <Link
          to="/hierarchy"
          className={`flex items-center gap-2 rounded px-3 py-2 text-xs transition-colors ${
            pathname === "/hierarchy"
              ? "bg-surface-hover text-accent"
              : "text-text-secondary hover:text-text hover:bg-surface-hover/50"
          }`}
        >
          <LayoutDashboard className="h-3.5 w-3.5" />
          <span className="font-medium">mission control</span>
        </Link>

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

        {projectsExpanded && projects.length > 0 && (
          <div id="projects-list" className="ml-4 border-l border-border pl-2 space-y-px">
            {projects.map((project) => {
              const isActive = activeProject === project.id;
              return (
                <Link
                  key={project.id}
                  to={`/projects/${project.id}`}
                  className={`flex items-center gap-2 rounded px-3 py-1.5 text-xs transition-colors ${
                    isActive
                      ? "bg-surface-hover text-accent font-medium"
                      : "text-text-secondary hover:text-text hover:bg-surface-hover/50"
                  }`}
                >
                  <span
                    className={`h-1.5 w-1.5 rounded-full ${project.status === "active" ? "bg-green" : "bg-text-muted/30"}`}
                  />
                  {project.name}
                </Link>
              );
            })}
          </div>
        )}

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
          <div id="agents-list" className="ml-4 border-l border-border pl-2 space-y-px">
            {(() => {
              // Detect display name collisions to disambiguate with framework
              const nameCounts = new Map<string, number>();
              for (const a of agents) {
                const label = a.display_name || a.name;
                nameCounts.set(label, (nameCounts.get(label) || 0) + 1);
              }
              return agents.map((agent) => {
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
              });
            })()}
          </div>
        )}

        <div className="space-y-px">
          {/* Frameworks */}
          {(() => {
            const frameworks = [...new Set(agents.map((a) => a.framework))];
            return (
              <>
                <div className={`flex items-center rounded text-xs transition-colors ${
                  pathname.startsWith("/frameworks") || pathname === "/install"
                    ? "bg-surface-hover text-text"
                    : "text-text-secondary hover:bg-surface-hover/50"
                }`}>
                  <span className="flex flex-1 items-center gap-2 px-3 py-1.5">
                    <Layers className="h-3.5 w-3.5" />
                    <span className="font-medium">frameworks</span>
                  </span>
                  <button
                    onClick={() => setFrameworksExpanded((prev) => !prev)}
                    aria-expanded={frameworksExpanded}
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
                  <div className="ml-4 border-l border-border pl-2 space-y-px">
                    {frameworks.map((fw) => {
                      const fwAgents = agents.filter((a) => a.framework === fw);
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
                          <span className="truncate">{fw} ({fwAgents.length} {FRAMEWORK_EMOJI[fw] || ""})</span>
                        </Link>
                      );
                    })}
                    <Link
                      to="/install"
                      className={`flex items-center gap-2 rounded px-3 py-1.5 text-xs transition-colors ${
                        pathname === "/install"
                          ? "bg-surface-hover text-accent font-medium"
                          : "text-text-muted hover:text-text hover:bg-surface-hover/50"
                      }`}
                    >
                      <span>install</span>
                      <Download className="h-3 w-3" />
                    </Link>
                  </div>
                )}
              </>
            );
          })()}

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
