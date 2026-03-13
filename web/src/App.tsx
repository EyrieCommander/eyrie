import { useEffect, useState, useCallback } from "react";
import type { AgentInfo } from "./lib/types";
import { fetchAgents } from "./lib/api";
import AgentCard from "./components/AgentCard";
import AgentDetail from "./components/AgentDetail";
import { Bird, RefreshCw } from "lucide-react";

export default function App() {
  const [agents, setAgents] = useState<AgentInfo[]>([]);
  const [selected, setSelected] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await fetchAgents();
      setAgents(data);
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

  const selectedAgent = agents.find((a) => a.name === selected);

  return (
    <div className="mx-auto min-h-screen max-w-5xl px-6 py-8">
      <header className="mb-8 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Bird className="h-8 w-8 text-accent" />
          <div>
            <h1 className="text-2xl font-bold tracking-tight">Eyrie</h1>
            <p className="text-sm text-text-muted">
              Claw Agent Management Dashboard
            </p>
          </div>
        </div>
        <button
          onClick={refresh}
          disabled={loading}
          className="flex items-center gap-2 rounded-md border border-border px-3 py-1.5 text-sm text-text-muted transition-colors hover:bg-surface-hover hover:text-text disabled:opacity-50"
        >
          <RefreshCw className={`h-4 w-4 ${loading ? "animate-spin" : ""}`} />
          Refresh
        </button>
      </header>

      {error && (
        <div className="mb-6 rounded-lg border border-red/30 bg-red/5 px-4 py-3 text-sm text-red">
          {error}
        </div>
      )}

      {selectedAgent ? (
        <AgentDetail
          agent={selectedAgent}
          onBack={() => setSelected(null)}
        />
      ) : (
        <div>
          <div className="mb-4 flex items-center justify-between">
            <h2 className="text-lg font-semibold">
              Agents
              {!loading && (
                <span className="ml-2 text-sm font-normal text-text-muted">
                  ({agents.length} discovered)
                </span>
              )}
            </h2>
          </div>

          {loading && agents.length === 0 ? (
            <div className="flex items-center justify-center py-20 text-text-muted">
              Discovering agents...
            </div>
          ) : agents.length === 0 ? (
            <div className="rounded-lg border border-border bg-surface p-8 text-center">
              <p className="text-text-muted">
                No agents discovered. Make sure ZeroClaw or OpenClaw is
                installed and configured.
              </p>
            </div>
          ) : (
            <div className="grid gap-4 sm:grid-cols-2">
              {agents.map((agent) => (
                <AgentCard
                  key={agent.name}
                  agent={agent}
                  onSelect={setSelected}
                />
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
