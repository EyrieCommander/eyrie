import { createContext, useContext, useCallback, useEffect, useState, type ReactNode } from "react";
import type { AgentInfo, Project, AgentInstance } from "./types";
import { fetchAgents, fetchProjects, fetchInstances } from "./api";

interface DataContextValue {
  agents: AgentInfo[];
  projects: Project[];
  instances: AgentInstance[];
  loading: boolean;
  error: string | null;
  refresh: () => Promise<void>;
}

const DataContext = createContext<DataContextValue | null>(null);

export function DataProvider({ children }: { children: ReactNode }) {
  const [agents, setAgents] = useState<AgentInfo[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [instances, setInstances] = useState<AgentInstance[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const errors: string[] = [];

      const [agentResult, projectResult, instanceResult] = await Promise.allSettled([
        fetchAgents(),
        fetchProjects(),
        fetchInstances(),
      ]);

      if (agentResult.status === "fulfilled") {
        setAgents(agentResult.value);
      } else {
        errors.push(`agents: ${agentResult.reason?.message || "fetch failed"}`);
      }

      if (projectResult.status === "fulfilled") {
        setProjects(projectResult.value);
      } else {
        errors.push(`projects: ${projectResult.reason?.message || "fetch failed"}`);
      }

      if (instanceResult.status === "fulfilled") {
        setInstances(instanceResult.value);
      } else {
        errors.push(`instances: ${instanceResult.reason?.message || "fetch failed"}`);
      }

      if (errors.length > 0) {
        setError(errors.join("; "));
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    let timeoutId: ReturnType<typeof setTimeout>;
    let cancelled = false;

    const scheduleRefresh = () => {
      timeoutId = setTimeout(async () => {
        if (cancelled) return;
        await refresh();
        if (!cancelled) scheduleRefresh();
      }, 30000);
    };

    refresh().then(() => {
      if (!cancelled) scheduleRefresh();
    });

    return () => {
      cancelled = true;
      clearTimeout(timeoutId);
    };
  }, [refresh]);

  return (
    <DataContext.Provider value={{ agents, projects, instances, loading, error, refresh }}>
      {children}
    </DataContext.Provider>
  );
}

export function useData(): DataContextValue {
  const ctx = useContext(DataContext);
  if (!ctx) {
    throw new Error("useData must be used within a DataProvider");
  }
  return ctx;
}
