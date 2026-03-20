import { useState, useEffect, useMemo } from "react";
import { Link, useLocation } from "react-router-dom";
import { Bird, ChevronDown, ChevronRight, Download, Settings } from "lucide-react";
import type { AgentInfo } from "../lib/types";

const subPages = ["overview", "chat", "logs", "config"] as const;

function parseAgentRoute(pathname: string) {
  const match = pathname.match(/^\/agents\/([^/]+?)(?:\/(overview|chat|logs|config))?$/);
  if (!match || match[1] === "overview") return { name: null, tab: null };
  return { name: match[1], tab: match[2] ?? "overview" };
}

export default function Sidebar({ agents }: { agents: AgentInfo[] }) {
  const { pathname } = useLocation();
  const { name: activeName, tab: activeTab } = useMemo(
    () => parseAgentRoute(pathname),
    [pathname],
  );

  const [expanded, setExpanded] = useState<Record<string, boolean>>(() => {
    const init: Record<string, boolean> = {};
    if (activeName) init[activeName] = true;
    return init;
  });

  useEffect(() => {
    if (activeName) {
      setExpanded((prev) => (prev[activeName] ? prev : { ...prev, [activeName]: true }));
    }
  }, [activeName]);

  const toggle = (name: string) =>
    setExpanded((prev) => ({ ...prev, [name]: !prev[name] }));

  return (
    <aside className="flex h-screen w-56 shrink-0 flex-col bg-bg-sidebar border-r border-border">
      <div className="px-5 pt-7 pb-6">
        <Link to="/agents/overview" className="flex items-center gap-2 hover:opacity-80 transition-opacity">
          <Bird className="h-5 w-5 text-accent" />
          <span className="text-base font-bold text-text">eyrie</span>
        </Link>
      </div>

      <nav className="flex-1 overflow-y-auto px-3 space-y-0.5">
        {agents.map((agent) => {
          const isExpanded = expanded[agent.name] ?? false;
          const isActive = activeName === agent.name;

          return (
            <div key={agent.name}>
              <button
                onClick={() => toggle(agent.name)}
                className={`flex w-full items-center gap-2 rounded px-3 py-2 text-xs transition-colors ${
                  isActive
                    ? "bg-surface-hover text-text"
                    : "text-text-secondary hover:bg-surface-hover/50"
                }`}
              >
                {isExpanded ? (
                  <ChevronDown className="h-3 w-3 text-text-muted" />
                ) : (
                  <ChevronRight className="h-3 w-3 text-text-muted" />
                )}
                <span
                  className={`h-1.5 w-1.5 rounded-full ${agent.alive ? "bg-green" : "bg-red"}`}
                />
                <span className={`font-medium ${isActive ? "text-text" : ""}`}>
                  {agent.name}
                </span>
              </button>

              {isExpanded && (
                <div className="ml-4 border-l border-border pl-2 space-y-px">
                  {subPages.map((page) => {
                    const isCurrent = isActive && activeTab === page;
                    return (
                      <Link
                        key={page}
                        to={`/agents/${agent.name}/${page}`}
                        className={`block rounded px-3 py-1.5 text-xs transition-colors ${
                          isCurrent
                            ? "bg-surface-hover text-accent font-medium"
                            : "text-text-secondary hover:text-text hover:bg-surface-hover/50"
                        }`}
                      >
                        {page}
                      </Link>
                    );
                  })}
                </div>
              )}
            </div>
          );
        })}

        <div className="pt-4 border-t border-border mt-4 space-y-px">
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
