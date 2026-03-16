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

export interface AgentConfig {
  content: string;
  format: string;
}

export async function fetchAgentConfig(name: string): Promise<AgentConfig> {
  const res = await fetch(`${BASE}/api/agents/${name}/config`);
  if (!res.ok)
    throw new Error(`Failed to fetch config: ${res.statusText}`);
  const data = await res.json();
  const format = data.format || "text";
  try {
    const parsed = JSON.parse(data.raw);
    return { content: parsed.content ?? data.raw, format };
  } catch {
    return { content: data.raw, format };
  }
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

export async function sendMessage(
  name: string,
  message: string,
  sessionKey?: string,
): Promise<ChatMessage> {
  const res = await fetch(`${BASE}/api/agents/${name}/chat`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ message, session_key: sessionKey }),
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to send message: ${res.statusText}`);
  }
  return res.json();
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

export async function deleteSession(
  name: string,
  sessionKey: string,
): Promise<void> {
  const res = await fetch(
    `${BASE}/api/agents/${name}/sessions/${encodeURIComponent(sessionKey)}`,
    { method: "DELETE" },
  );
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to delete session: ${res.statusText}`);
  }
}
