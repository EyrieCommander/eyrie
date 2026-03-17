import type {
  AgentInfo,
  LogEntry,
  ActivityEvent,
  SessionsResponse,
  ChatMessage,
  ChatEvent,
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

export function streamMessage(
  name: string,
  message: string,
  sessionKey: string | undefined,
  onEvent: (event: ChatEvent) => void,
): AbortController {
  const controller = new AbortController();
  (async () => {
    try {
      const res = await fetch(`${BASE}/api/agents/${name}/chat`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ message, session_key: sessionKey }),
        signal: controller.signal,
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({ error: res.statusText }));
        onEvent({
          type: "error",
          error: body.error || `Failed to send message: ${res.statusText}`,
        });
        return;
      }
      const reader = res.body!.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      for (;;) {
        const { done, value } = await reader.read();
        if (!done) {
          buffer += decoder.decode(value, { stream: true });
        } else {
          buffer += decoder.decode();
        }
        const lines = buffer.split("\n");
        buffer = lines.pop()!;
        for (const line of lines) {
          if (line.startsWith("data: ")) {
            try {
              onEvent(JSON.parse(line.slice(6)));
            } catch {
              // skip malformed SSE lines
            }
          }
        }
        if (done) {
          if (buffer.startsWith("data: ")) {
            try {
              onEvent(JSON.parse(buffer.slice(6)));
            } catch { /* skip */ }
          }
          break;
        }
      }
    } catch (e) {
      if ((e as Error).name !== "AbortError") {
        onEvent({
          type: "error",
          error: e instanceof Error ? e.message : "Stream failed",
        });
      }
    }
  })();
  return controller;
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

export async function resetSession(
  name: string,
  sessionKey: string,
): Promise<void> {
  const res = await fetch(
    `${BASE}/api/agents/${name}/sessions/${encodeURIComponent(sessionKey)}`,
    { method: "DELETE" },
  );
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to reset session: ${res.statusText}`);
  }
}

export async function purgeSession(
  name: string,
  sessionKey: string,
): Promise<void> {
  const res = await fetch(
    `${BASE}/api/agents/${name}/sessions/${encodeURIComponent(sessionKey)}/purge`,
    { method: "DELETE" },
  );
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to purge session: ${res.statusText}`);
  }
}

export async function hideSession(
  name: string,
  sessionKey: string,
): Promise<void> {
  const res = await fetch(
    `${BASE}/api/agents/${name}/sessions/${encodeURIComponent(sessionKey)}/hide`,
    { method: "POST" },
  );
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to hide session: ${res.statusText}`);
  }
}
