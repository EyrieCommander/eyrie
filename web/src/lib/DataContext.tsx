import { createContext, useContext, useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import type { AgentInfo, Project, AgentInstance } from "./types";
import { fetchAgents, fetchProjects, fetchInstances } from "./api";
import { cleanDisplayName } from "./format";

interface DataContextValue {
  agents: AgentInfo[];
  projects: Project[];
  instances: AgentInstance[];
  loading: boolean;
  error: string | null;
  refresh: (isUserInitiated?: boolean) => Promise<void>;
  pendingActions: Record<string, string>;
  setPendingAction: (agentName: string, action: string | null) => void;
}

const DataContext = createContext<DataContextValue | null>(null);

export function DataProvider({ children }: { children: ReactNode }) {
  const [agents, setAgents] = useState<AgentInfo[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [instances, setInstances] = useState<AgentInstance[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [pendingActions, setPendingActions] = useState<Record<string, string>>({});

  const setPendingAction = useCallback((agentName: string, action: string | null) => {
    setPendingActions((prev) => {
      if (action === null) {
        const { [agentName]: _, ...rest } = prev;
        return rest;
      }
      return { ...prev, [agentName]: action };
    });
  }, []);

  const refresh = useCallback(async (isUserInitiated = true) => {
    try {
      if (isUserInitiated) setLoading(true);
      setError(null);
      const errors: string[] = [];

      const [agentResult, projectResult, instanceResult] = await Promise.allSettled([
        fetchAgents(),
        fetchProjects(),
        fetchInstances(),
      ]);

      if (agentResult.status === "fulfilled") {
        setAgents(agentResult.value.map((a) => ({ ...a, display_name: cleanDisplayName(a.display_name) || a.display_name })));
      } else {
        errors.push(`agents: ${agentResult.reason?.message || "fetch failed"}`);
      }

      if (projectResult.status === "fulfilled") {
        setProjects(projectResult.value);
      } else {
        errors.push(`projects: ${projectResult.reason?.message || "fetch failed"}`);
      }

      if (instanceResult.status === "fulfilled") {
        setInstances(instanceResult.value.map((i) => ({ ...i, display_name: cleanDisplayName(i.display_name) || i.display_name })));
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

  // WHY errorRef instead of error in deps: Including `error` in the
  // dependency array causes the entire polling loop to tear down and restart
  // on every error state transition (null→"error"→null), triggering an extra
  // fetch each time. The ref lets the backoff logic read the current error
  // state without restarting the loop.
  const errorRef = useRef(error);
  useEffect(() => { errorRef.current = error; }, [error]);

  useEffect(() => {
    let timeoutId: ReturnType<typeof setTimeout>;
    let cancelled = false;

    const scheduleRefresh = () => {
      // Back off to 60s when all fetches failed (backend likely down),
      // reset to 30s on any success.
      const delay = errorRef.current ? 60000 : 30000;
      timeoutId = setTimeout(async () => {
        if (cancelled) return;
        await refresh(false);
        if (!cancelled) scheduleRefresh();
      }, delay);
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
    <DataContext.Provider value={{ agents, projects, instances, loading, error, refresh, pendingActions, setPendingAction }}>
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
