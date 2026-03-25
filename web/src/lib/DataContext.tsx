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
      const [agentData, projectData, instanceData] = await Promise.all([
        fetchAgents(),
        fetchProjects().catch((e) => {
          console.error("Failed to fetch projects:", e);
          return [] as Project[];
        }),
        fetchInstances().catch((e) => {
          console.error("Failed to fetch instances:", e);
          return [] as AgentInstance[];
        }),
      ]);
      setAgents(agentData);
      setProjects(projectData);
      setInstances(instanceData);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to fetch agents");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
    const interval = setInterval(refresh, 30000);
    return () => clearInterval(interval);
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
