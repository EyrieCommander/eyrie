import { createContext, useContext, useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import type { AgentInfo, Project, AgentInstance, CommanderInfo } from "./types";
import { fetchAgents, fetchProjects, fetchInstances, fetchCommander } from "./api";
import { cleanDisplayName } from "./format";

interface DataContextValue {
  agents: AgentInfo[];
  projects: Project[];
  instances: AgentInstance[];
  commander: CommanderInfo | null;
  loading: boolean;
  error: string | null;
  /** True when all API fetches failed — backend is likely down or restarting. */
  backendDown: boolean;
  refresh: (isUserInitiated?: boolean) => Promise<void>;
  pendingActions: Record<string, string>;
  setPendingAction: (agentName: string, action: string | null) => void;
}

const DataContext = createContext<DataContextValue | null>(null);

export function DataProvider({ children }: { children: ReactNode }) {
  const [agents, setAgents] = useState<AgentInfo[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [instances, setInstances] = useState<AgentInstance[]>([]);
  const [commander, setCommander] = useState<CommanderInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [backendDown, setBackendDown] = useState(false);
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

      const [agentResult, projectResult, instanceResult, commanderResult] = await Promise.allSettled([
        fetchAgents(),
        fetchProjects(),
        fetchInstances(),
        fetchCommander(),
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

      if (commanderResult.status === "fulfilled") {
        setCommander(commanderResult.value ?? null);
      }
      // Commander fetch failure is not counted as an error — it's optional
      // (no commander set up yet is a valid state)

      // All three core fetches failed → backend is down. Fewer than three → at least
      // partially reachable, so clear the down flag.
      setBackendDown(errors.length === 3);
      if (errors.length > 0) {
        setError(errors.join("; "));
      }
    } finally {
      setLoading(false);
    }
  }, []);

  // WHY refs instead of state in deps: Including `error`/`backendDown` in the
  // dependency array causes the entire polling loop to tear down and restart
  // on every state transition (null→"error"→null), triggering an extra fetch
  // each time. Refs let the backoff logic read current state without
  // restarting the loop.
  const errorRef = useRef(error);
  useEffect(() => { errorRef.current = error; }, [error]);
  const backendDownRef = useRef(backendDown);
  useEffect(() => { backendDownRef.current = backendDown; }, [backendDown]);

  // WHY consecutive failure count: A single failure could be a transient
  // hiccup (backend recompiling). We only ramp up backoff after sustained
  // failures, keeping reconnection snappy for brief restarts while reducing
  // console noise during extended downtime.
  const failCountRef = useRef(0);

  useEffect(() => {
    let timeoutId: ReturnType<typeof setTimeout>;
    let cancelled = false;

    const scheduleRefresh = () => {
      let delay: number;
      if (backendDownRef.current) {
        // Exponential backoff: 5s → 10s → 15s, capped at 15s.
        // Keeps console quiet during extended downtime while still
        // reconnecting within a reasonable window.
        delay = Math.min(5000 + failCountRef.current * 5000, 15000);
      } else {
        delay = 30000;
      }
      timeoutId = setTimeout(async () => {
        if (cancelled) return;
        await refresh(false);
        if (backendDownRef.current) {
          failCountRef.current++;
        } else {
          failCountRef.current = 0;
        }
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
    <DataContext.Provider value={{ agents, projects, instances, commander, loading, error, backendDown, refresh, pendingActions, setPendingAction }}>
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
