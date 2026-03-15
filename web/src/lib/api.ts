import type {
  AgentInfo,
  LogEntry,
  ActivityEvent,
  SessionsResponse,
  ChatMessage,
} from "./types";

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
      onEntry(JSON.parse(event.data));
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

export function streamActivity(
  name: string,
  onEvent: (event: ActivityEvent) => void,
): () => void {
  const es = new EventSource(`${BASE}/api/agents/${name}/activity`);
  es.onmessage = (event) => {
    try {
      onEvent(JSON.parse(event.data));
    } catch {
      onEvent({
        timestamp: new Date().toISOString(),
        type: "log",
        summary: event.data,
      });
    }
  };
  return () => es.close();
}

export async function fetchSessions(
  name: string,
): Promise<SessionsResponse> {
  const res = await fetch(`${BASE}/api/agents/${name}/sessions`);
  if (!res.ok)
    throw new Error(`Failed to fetch sessions: ${res.statusText}`);
  return res.json();
}

export async function fetchChatMessages(
  name: string,
  sessionKey: string,
  limit = 50,
): Promise<ChatMessage[]> {
  const res = await fetch(
    `${BASE}/api/agents/${name}/sessions/${encodeURIComponent(sessionKey)}/messages?limit=${limit}`,
  );
  if (!res.ok)
    throw new Error(`Failed to fetch messages: ${res.statusText}`);
  return res.json();
}
