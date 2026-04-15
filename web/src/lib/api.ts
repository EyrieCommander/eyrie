import { readSSEStream } from "./sse";
import type {
  AgentInfo,
  AgentInstance,
  CreateInstanceRequest,
  LogEntry,
  ActivityEvent,
  SessionsResponse,
  ChatMessage,
  ChatEvent,
  Framework,
  Persona,
  PersonaCategory,
  Project,
  CreateProjectRequest,
  HierarchyTree,
  ProjectChatMessage,
  KeyEntry,
  SetKeyResponse,
  ValidateKeyResponse,
} from "./types";

const BASE = "";

// Default timeout for API requests (10 seconds). Prevents fetch calls from
// hanging indefinitely when the backend is down or unresponsive.
const API_TIMEOUT = 10_000;

/** Fetch with a default timeout. Throws on timeout instead of hanging. */
async function fetchWithTimeout(input: string | URL | Request, init?: RequestInit): Promise<Response> {
  const signal = init?.signal;
  // If the caller already provided a signal, don't override it
  if (signal) return fetch(input, init);
  try {
    return await fetch(input, { ...init, signal: AbortSignal.timeout(API_TIMEOUT) });
  } catch (err) {
    if (err instanceof DOMException && err.name === "TimeoutError") {
      throw new Error("request timed out — backend may be down");
    }
    throw err;
  }
}

// ApiError carries structured error info from the backend, including a
// machine-readable `code` (e.g. "auth_expired", "agent_unreachable") and
// the HTTP status. Components can match on these to show targeted UI
// instead of generic "something went wrong" messages.
export class ApiError extends Error {
  code: string;
  status: number;
  constructor(message: string, status: number, code: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

// throwIfNotOk parses a structured error response and throws an ApiError.
// The backend returns { "error": "...", "code": "..." } for adapter errors.
async function throwIfNotOk(res: Response, fallbackMsg: string): Promise<void> {
  if (res.ok) return;
  const body = await res.json().catch(() => ({ error: res.statusText }));
  throw new ApiError(
    body.error || `${fallbackMsg}: ${res.statusText}`,
    res.status,
    body.code || "unknown",
  );
}

export async function fetchAgents(): Promise<AgentInfo[]> {
  const res = await fetchWithTimeout(`${BASE}/api/agents`);
  if (!res.ok) throw new Error(`Failed to fetch agents: ${res.statusText}`);
  return res.json();
}

export interface AgentConfig {
  content: string;
  format: string;
}

export async function fetchAgentConfig(name: string): Promise<AgentConfig> {
  const res = await fetchWithTimeout(`${BASE}/api/agents/${name}/config`);
  await throwIfNotOk(res, "Failed to fetch config");
  const data = await res.json();
  const format = data.format || "text";
  try {
    const parsed = JSON.parse(data.raw);
    return { content: parsed.content ?? data.raw, format };
  } catch {
    return { content: data.raw, format };
  }
}

export async function updateDisplayName(agentName: string, displayName: string): Promise<string> {
  const res = await fetchWithTimeout(`${BASE}/api/agents/${agentName}/display-name`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ display_name: displayName }),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(err.error || res.statusText);
  }
  const data = await res.json();
  return data.display_name;
}

export async function agentAction(
  name: string,
  action: "start" | "stop" | "restart",
): Promise<void> {
  const res = await fetchWithTimeout(`${BASE}/api/agents/${name}/${action}`, {
    method: "POST",
  });
  if (!res.ok) throw new Error(`Failed to ${action} agent: ${res.statusText}`);
}

export async function fetchAgentModels(name: string): Promise<string[]> {
  const res = await fetchWithTimeout(`${BASE}/api/agents/${name}/models`);
  if (!res.ok) {
    console.warn(`fetchAgentModels failed for ${name}: ${res.status} ${res.statusText}`);
    throw new Error(`Failed to fetch models: ${res.statusText}`);
  }
  return res.json();
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
  // When the server closes the stream (e.g. after sending historical logs
  // for an offline agent), stop reconnecting.
  es.onerror = () => {
    if (es.readyState === EventSource.CLOSED) return;
    es.close();
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
  // Stop reconnecting when the stream closes or the backend goes down.
  // Without this, EventSource auto-reconnects every ~3s, flooding the
  // console with ERR_CONNECTION_REFUSED.
  es.onerror = () => {
    if (es.readyState === EventSource.CLOSED) return;
    es.close();
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
      const res = await fetchWithTimeout(`${BASE}/api/agents/${name}/chat`, {
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
          code: body.code,
        });
        return;
      }
      await readSSEStream(res.body!, (data) => onEvent(data));
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
  const res = await fetchWithTimeout(`${BASE}/api/agents/${name}/sessions`);
  await throwIfNotOk(res, "Failed to fetch sessions");
  return res.json();
}

export async function fetchChatMessages(
  name: string,
  sessionKey: string,
  limit = 50,
): Promise<ChatMessage[]> {
  const res = await fetchWithTimeout(
    `${BASE}/api/agents/${name}/sessions/${encodeURIComponent(sessionKey)}/messages?limit=${limit}`,
  );
  await throwIfNotOk(res, "Failed to fetch messages");
  return res.json();
}

export async function createSession(
  agentName: string,
  sessionName: string,
): Promise<{ key: string; title: string }> {
  const res = await fetchWithTimeout(`${BASE}/api/agents/${agentName}/sessions`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name: sessionName }),
  });
  await throwIfNotOk(res, "Failed to create session");
  return res.json();
}

export async function resetSession(
  name: string,
  sessionKey: string,
): Promise<void> {
  const res = await fetchWithTimeout(
    `${BASE}/api/agents/${name}/sessions/${encodeURIComponent(sessionKey)}`,
    { method: "DELETE" },
  );
  await throwIfNotOk(res, "Failed to reset session");
}

export async function deleteSession(
  name: string,
  sessionKey: string,
): Promise<void> {
  const res = await fetchWithTimeout(
    `${BASE}/api/agents/${name}/sessions/${encodeURIComponent(sessionKey)}/purge`,
    { method: "DELETE" },
  );
  await throwIfNotOk(res, "Failed to delete session");
}

export async function destroySession(
  name: string,
  sessionKey: string,
): Promise<void> {
  const res = await fetchWithTimeout(
    `${BASE}/api/agents/${name}/sessions/${encodeURIComponent(sessionKey)}/destroy`,
    { method: "DELETE" },
  );
  await throwIfNotOk(res, "Failed to destroy session");
}

export async function hideSession(
  name: string,
  sessionKey: string,
): Promise<void> {
  const res = await fetchWithTimeout(
    `${BASE}/api/agents/${name}/sessions/${encodeURIComponent(sessionKey)}/hide`,
    { method: "POST" },
  );
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to hide session: ${res.statusText}`);
  }
}

// Config API

export async function updateAgentConfig(
  name: string,
  config: unknown,
): Promise<void> {
  const res = await fetchWithTimeout(`${BASE}/api/agents/${name}/config`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ config }),
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to update config: ${res.statusText}`);
  }
}

export interface ConfigValidationResult {
  valid: boolean;
  error?: string;
  message?: string;
}

export async function validateAgentConfig(
  name: string,
  config: unknown,
): Promise<ConfigValidationResult> {
  const res = await fetchWithTimeout(`${BASE}/api/agents/${name}/config/validate`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ config }),
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(
      body.error || `Failed to validate config: ${res.statusText}`,
    );
  }
  return res.json();
}

// Registry and install API

export async function getFrameworkDetail(id: string): Promise<Framework> {
  const res = await fetchWithTimeout(`${BASE}/api/registry/frameworks/${id}`);
  if (!res.ok)
    throw new Error(`Failed to fetch framework detail: ${res.statusText}`);
  return res.json();
}

export async function fetchFrameworks(refresh = false): Promise<Framework[]> {
  const qs = refresh ? "?refresh=true" : "";
  const res = await fetchWithTimeout(`${BASE}/api/registry/frameworks${qs}`);
  if (!res.ok)
    throw new Error(`Failed to fetch frameworks: ${res.statusText}`);
  return res.json();
}

// Persona API

export async function fetchPersonas(): Promise<Persona[]> {
  const res = await fetchWithTimeout(`${BASE}/api/personas`);
  if (!res.ok) throw new Error(`Failed to fetch personas: ${res.statusText}`);
  return res.json();
}

export async function fetchPersonaCategories(): Promise<PersonaCategory[]> {
  const res = await fetchWithTimeout(`${BASE}/api/personas/categories`);
  if (!res.ok)
    throw new Error(`Failed to fetch categories: ${res.statusText}`);
  return res.json();
}

export async function fetchPersona(id: string): Promise<Persona> {
  const res = await fetchWithTimeout(`${BASE}/api/personas/${id}`);
  if (!res.ok) throw new Error(`Failed to fetch persona: ${res.statusText}`);
  return res.json();
}

export async function installPersona(personaId: string): Promise<Persona> {
  const res = await fetchWithTimeout(`${BASE}/api/personas/install`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ persona_id: personaId }),
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to install persona: ${res.statusText}`);
  }
  return res.json();
}

export async function updatePersona(
  id: string,
  persona: Persona,
): Promise<Persona> {
  const res = await fetchWithTimeout(`${BASE}/api/personas/${id}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(persona),
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to update persona: ${res.statusText}`);
  }
  return res.json();
}

export async function deletePersona(id: string): Promise<void> {
  const res = await fetchWithTimeout(`${BASE}/api/personas/${id}`, { method: "DELETE" });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to delete persona: ${res.statusText}`);
  }
}

// Instance API

export async function fetchInstances(): Promise<AgentInstance[]> {
  const res = await fetchWithTimeout(`${BASE}/api/instances`);
  if (!res.ok) throw new Error(`Failed to fetch instances: ${res.statusText}`);
  return res.json();
}

export async function fetchInstance(id: string): Promise<AgentInstance> {
  const res = await fetchWithTimeout(`${BASE}/api/instances/${id}`);
  if (!res.ok) throw new Error(`Failed to fetch instance: ${res.statusText}`);
  return res.json();
}

export async function createInstance(req: CreateInstanceRequest): Promise<AgentInstance> {
  const res = await fetchWithTimeout(`${BASE}/api/instances`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to create instance: ${res.statusText}`);
  }
  return res.json();
}

export async function deleteInstance(id: string): Promise<void> {
  const res = await fetchWithTimeout(`${BASE}/api/instances/${id}`, { method: "DELETE" });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to delete instance: ${res.statusText}`);
  }
}

export async function instanceAction(id: string, action: "start" | "stop" | "restart"): Promise<void> {
  const res = await fetchWithTimeout(`${BASE}/api/instances/${id}/${action}`, { method: "POST" });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to ${action} instance: ${res.statusText}`);
  }
}

// Project API

export async function fetchProjects(): Promise<Project[]> {
  const res = await fetchWithTimeout(`${BASE}/api/projects`);
  if (!res.ok) throw new Error(`Failed to fetch projects: ${res.statusText}`);
  return res.json();
}

export async function createProject(req: CreateProjectRequest): Promise<Project> {
  const res = await fetchWithTimeout(`${BASE}/api/projects`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to create project: ${res.statusText}`);
  }
  return res.json();
}

export async function updateProject(id: string, updates: Partial<Pick<Project, "name" | "description" | "goal" | "status" | "orchestrator_id">>): Promise<Project> {
  const res = await fetchWithTimeout(`${BASE}/api/projects/${id}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(updates),
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to update project: ${res.statusText}`);
  }
  return res.json();
}

export async function deleteProject(id: string): Promise<void> {
  const res = await fetchWithTimeout(`${BASE}/api/projects/${id}`, { method: "DELETE" });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to delete project: ${res.statusText}`);
  }
}

export async function resetProject(id: string): Promise<void> {
  const res = await fetchWithTimeout(`${BASE}/api/projects/${id}/reset`, { method: "POST" });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to reset project: ${res.statusText}`);
  }
}

// Hierarchy API

export async function fetchHierarchy(): Promise<HierarchyTree> {
  const res = await fetchWithTimeout(`${BASE}/api/hierarchy`);
  if (!res.ok) throw new Error(`Failed to fetch hierarchy: ${res.statusText}`);
  return res.json();
}

// Lightweight commander lookup — reads a JSON file instead of running full
// discovery. Use this when you only need the commander's name/status.
export async function fetchCommander(): Promise<HierarchyTree["commander"]> {
  const res = await fetchWithTimeout(`${BASE}/api/hierarchy/commander`);
  if (!res.ok) throw new Error(`Failed to fetch commander: ${res.statusText}`);
  const data = await res.json();
  return data.commander ?? null;
}

export function streamCommanderBriefing(
  onEvent: (event: ChatEvent & { session_key?: string }) => void,
): { controller: AbortController; sessionReady: Promise<string> } {
  const controller = new AbortController();
  let resolveSession: (key: string) => void;
  let sessionResolved = false;
  const sessionReady = new Promise<string>((resolve) => { resolveSession = resolve; });
  (async () => {
    try {
      const res = await fetchWithTimeout(`${BASE}/api/hierarchy/commander/brief`, {
        method: "POST",
        signal: controller.signal,
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({ error: res.statusText }));
        onEvent({ type: "error", error: body.error || res.statusText, code: body.code });
        resolveSession!("");
        return;
      }
      await readSSEStream(res.body!, (ev) => {
        if (ev.type === "session" && ev.session_key) {
          resolveSession!(ev.session_key); sessionResolved = true;
        }
        onEvent(ev);
      });
      // Ensure promise settles even if no session event was received
      if (!sessionResolved) { resolveSession!(""); sessionResolved = true; }
    } catch (e) {
      if ((e as Error).name !== "AbortError") {
        onEvent({ type: "error", error: e instanceof Error ? e.message : "Briefing failed" });
      }
      if (!sessionResolved) { resolveSession!(""); sessionResolved = true; }
    }
  })();
  return { controller, sessionReady };
}

export function streamCaptainBriefing(
  projectId: string,
  onEvent: (event: ChatEvent & { session_key?: string }) => void,
): { controller: AbortController; sessionReady: Promise<string> } {
  const controller = new AbortController();
  let resolveSession: (key: string) => void;
  let sessionResolved = false;
  const sessionReady = new Promise<string>((resolve) => { resolveSession = resolve; });
  (async () => {
    try {
      const res = await fetchWithTimeout(`${BASE}/api/projects/${projectId}/captain/brief`, {
        method: "POST",
        signal: controller.signal,
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({ error: res.statusText }));
        onEvent({ type: "error", error: body.error || res.statusText, code: body.code });
        resolveSession!("");
        return;
      }
      await readSSEStream(res.body!, (ev) => {
        if (ev.type === "session" && ev.session_key) {
          resolveSession!(ev.session_key); sessionResolved = true;
        }
        onEvent(ev);
      });
      if (!sessionResolved) { resolveSession!(""); sessionResolved = true; }
    } catch (e) {
      if ((e as Error).name !== "AbortError") {
        onEvent({ type: "error", error: e instanceof Error ? e.message : "Briefing failed" });
      }
      if (!sessionResolved) { resolveSession!(""); sessionResolved = true; }
    }
  })();
  return { controller, sessionReady };
}

// --- Project Chat ---

export async function fetchProjectChat(projectId: string): Promise<ProjectChatMessage[]> {
  const res = await fetchWithTimeout(`${BASE}/api/projects/${projectId}/chat`);
  if (!res.ok) {
    throw new Error(`Failed to fetch project chat: ${res.status} ${res.statusText}`);
  }
  return res.json();
}

export interface ProjectChatEvent {
  type: "message" | "agent_event" | "done" | "error" | "debug";
  message?: ProjectChatMessage;
  sender?: string;
  role?: string;
  event?: ChatEvent;
  error?: string;
  code?: string; // Machine-readable error code from backend
  msg?: string;
  detail?: Record<string, any>;
}

export function streamProjectChat(
  projectId: string,
  message: string,
  onEvent: (event: ProjectChatEvent) => void,
): AbortController {
  const controller = new AbortController();
  (async () => {
    try {
      const res = await fetchWithTimeout(`${BASE}/api/projects/${projectId}/chat`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ message }),
        signal: controller.signal,
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({ error: res.statusText }));
        onEvent({ type: "error", error: body.error || res.statusText, code: body.code });
        return;
      }
      await readSSEStream(res.body!, onEvent);
    } catch (e) {
      if ((e as Error).name !== "AbortError") {
        onEvent({ type: "error", error: e instanceof Error ? e.message : "Chat failed" });
      }
    }
  })();
  return controller;
}

/** Check if a project chat response is currently being streamed. */
export async function projectChatStatus(projectId: string): Promise<{ streaming: boolean }> {
  const res = await fetchWithTimeout(`${BASE}/api/projects/${projectId}/chat/status`);
  if (!res.ok) return { streaming: false };
  return res.json();
}

/** Tell the backend to cancel an in-flight project chat orchestration. */
export async function stopProjectChat(projectId: string): Promise<void> {
  const res = await fetchWithTimeout(`${BASE}/api/projects/${projectId}/chat/stop`, { method: "POST" });
  if (!res.ok) {
    const body = await res.text().catch(() => res.statusText);
    throw new Error(`Failed to stop chat: ${res.status} ${body}`);
  }
}

export async function setCommander(opts: { instanceId?: string; agentName?: string }): Promise<void> {
  const res = await fetchWithTimeout(`${BASE}/api/hierarchy/commander`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ instance_id: opts.instanceId, agent_name: opts.agentName }),
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to set commander: ${res.statusText}`);
  }
}

// --- Key Vault API ---

export async function fetchKeys(): Promise<KeyEntry[]> {
  const res = await fetchWithTimeout(`${BASE}/api/keys`);
  if (!res.ok) throw new Error(`Failed to fetch keys: ${res.statusText}`);
  return res.json();
}

export async function setKey(
  provider: string,
  key: string,
  skipValidation = false,
): Promise<SetKeyResponse> {
  const res = await fetchWithTimeout(`${BASE}/api/keys/${encodeURIComponent(provider)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ key, skip_validation: skipValidation }),
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to set key: ${res.statusText}`);
  }
  return res.json();
}

export async function deleteKey(provider: string): Promise<void> {
  const res = await fetchWithTimeout(`${BASE}/api/keys/${encodeURIComponent(provider)}`, {
    method: "DELETE",
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to delete key: ${res.statusText}`);
  }
}

export async function validateKey(
  provider: string,
  key: string,
): Promise<ValidateKeyResponse> {
  const res = await fetchWithTimeout(`${BASE}/api/keys/${encodeURIComponent(provider)}/validate`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ key }),
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to validate key: ${res.statusText}`);
  }
  return res.json();
}
