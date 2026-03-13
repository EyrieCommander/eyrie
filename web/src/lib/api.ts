import type { AgentInfo, LogEntry } from "./types";

const BASE = "";

export async function fetchAgents(): Promise<AgentInfo[]> {
  const res = await fetch(`${BASE}/api/agents`);
  if (!res.ok) throw new Error(`Failed to fetch agents: ${res.statusText}`);
  return res.json();
}

export async function fetchAgentConfig(name: string): Promise<string> {
  const res = await fetch(`${BASE}/api/agents/${name}/config`);
  if (!res.ok)
    throw new Error(`Failed to fetch config: ${res.statusText}`);
  const data = await res.json();
  return data.raw;
}

export async function agentAction(
  name: string,
  action: "start" | "stop" | "restart",
): Promise<void> {
  const res = await fetch(`${BASE}/api/agents/${name}/${action}`, {
    method: "POST",
  });
  if (!res.ok) throw new Error(`Failed to ${action} agent: ${res.statusText}`);
}

export function streamLogs(
  name: string,
  onEntry: (entry: LogEntry) => void,
): () => void {
  const es = new EventSource(`${BASE}/api/agents/${name}/logs`);
  es.onmessage = (event) => {
    try {
      const entry = JSON.parse(event.data);
      onEntry(entry);
    } catch {
      onEntry({
        timestamp: new Date().toISOString(),
        level: "info",
        message: event.data,
      });
    }
  };
  return () => es.close();
}
