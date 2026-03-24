import { useState, useEffect, useMemo } from "react";
import { Link, useLocation } from "react-router-dom";
import { Bird, Briefcase, Bot, ChevronDown, ChevronRight, Download, GitBranch, Settings, Users } from "lucide-react";
import type { AgentInfo, Project } from "../lib/types";

function parseAgentRoute(pathname: string) {
  const match = pathname.match(/^\/agents\/([^/]+?)(?:\/(status|chat|logs|config))?$/);
  if (!match || match[1] === "overview") return null;
  return match[1];
}

function parseProjectRoute(pathname: string) {
  const match = pathname.match(/^\/projects\/([^/]+)/);
  return match ? match[1] : null;
}

export default function Sidebar({ agents, projects }: { agents: AgentInfo[]; projects: Project[] }) {
  const { pathname } = useLocation();
  const activeAgent = useMemo(() => parseAgentRoute(pathname), [pathname]);
  const activeProject = useMemo(() => parseProjectRoute(pathname), [pathname]);

  const [agentsExpanded, setAgentsExpanded] = useState(true);
  const [projectsExpanded, setProjectsExpanded] = useState(true);

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
          <GitBranch className="h-3.5 w-3.5" />
          <span className="font-medium">hierarchy</span>
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
          <div className="ml-4 border-l border-border pl-2 space-y-px">
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
            className="px-3 py-1.5 hover:text-text transition-colors"
          >
            {agentsExpanded ? (
              <ChevronDown className="h-3 w-3 text-green" />
            ) : (
              <ChevronRight className="h-3 w-3 text-text-muted" />
            )}
          </button>
        </div>

        {agentsExpanded && (
          <div className="ml-4 border-l border-border pl-2 space-y-px">
            {agents.map((agent) => {
              const isActive = activeAgent === agent.name;
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
                    className={`h-1.5 w-1.5 rounded-full ${!agent.alive ? "bg-red" : agent.status?.provider_status === "error" ? "bg-yellow" : "bg-green"}`}
                  />
                  {agent.name}
                </Link>
              );
            })}
          </div>
        )}

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
            to="/install"
            className={`flex items-center gap-2 rounded px-3 py-2 text-xs transition-colors ${
              pathname === "/install"
                ? "bg-surface-hover text-accent"
                : "text-text-secondary hover:text-text hover:bg-surface-hover/50"
            }`}
          >
            <Download className="h-3.5 w-3.5" />
            <span className="font-medium">install</span>
          </Link>

          <div
            className="flex items-center gap-2 rounded px-3 py-2 text-xs text-text-muted cursor-not-allowed opacity-50"
            title="coming soon"
          >
            <Settings className="h-3.5 w-3.5" />
            <span className="font-medium">settings</span>
          </div>
        </div>
      </nav>
    </aside>
  );
}
