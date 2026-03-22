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
  InstallProgress,
  InstallLogEvent,
  Persona,
  PersonaCategory,
  Project,
  CreateProjectRequest,
  HierarchyTree,
  ProjectChatMessage,
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

export async function createSession(
  agentName: string,
  sessionName: string,
): Promise<{ key: string; title: string }> {
  const res = await fetch(`${BASE}/api/agents/${agentName}/sessions`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name: sessionName }),
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to create session: ${res.statusText}`);
  }
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

export async function deleteSession(
  name: string,
  sessionKey: string,
): Promise<void> {
  const res = await fetch(
    `${BASE}/api/agents/${name}/sessions/${encodeURIComponent(sessionKey)}/purge`,
    { method: "DELETE" },
  );
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to delete session: ${res.statusText}`);
  }
}

export async function destroySession(
  name: string,
  sessionKey: string,
): Promise<void> {
  const res = await fetch(
    `${BASE}/api/agents/${name}/sessions/${encodeURIComponent(sessionKey)}/destroy`,
    { method: "DELETE" },
  );
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to destroy session: ${res.statusText}`);
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

// Config API

export async function updateAgentConfig(
  name: string,
  config: unknown,
): Promise<void> {
  const res = await fetch(`${BASE}/api/agents/${name}/config`, {
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
  const res = await fetch(`${BASE}/api/agents/${name}/config/validate`, {
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
  const res = await fetch(`${BASE}/api/registry/frameworks/${id}`);
  if (!res.ok)
    throw new Error(`Failed to fetch framework detail: ${res.statusText}`);
  return res.json();
}

export async function fetchFrameworks(): Promise<Framework[]> {
  const res = await fetch(`${BASE}/api/registry/frameworks`);
  if (!res.ok)
    throw new Error(`Failed to fetch frameworks: ${res.statusText}`);
  return res.json();
}

export async function fetchInstallStatus(): Promise<
  Record<string, InstallProgress>
> {
  const res = await fetch(`${BASE}/api/registry/install/status`);
  if (!res.ok)
    throw new Error(`Failed to fetch install status: ${res.statusText}`);
  return res.json();
}

export function streamInstall(
  frameworkId: string,
  copyFrom: string | undefined,
  onProgress: (progress: InstallProgress) => void,
  onLog: (log: string) => void,
  force?: boolean,
): AbortController {
  const controller = new AbortController();

  (async () => {
    try {
      const res = await fetch(`${BASE}/api/registry/install`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          framework_id: frameworkId,
          copy_from: copyFrom,
          skip_confirm: true,
          force: force || false,
        }),
        signal: controller.signal,
      });

      if (!res.ok) {
        const body = await res.json().catch(() => ({ error: res.statusText }));
        onProgress({
          framework_id: frameworkId,
          phase: "error",
          status: "error",
          progress: 0,
          message: body.error || `Installation failed: ${res.statusText}`,
          error: body.error || res.statusText,
          started_at: new Date().toISOString(),
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
              const data = JSON.parse(line.slice(6));
              if (data.type === "log") {
                onLog((data as InstallLogEvent).message);
              } else {
                onProgress(data as InstallProgress);
              }
            } catch {
              // skip malformed SSE lines
            }
          }
        }

        if (done) {
          if (buffer.startsWith("data: ")) {
            try {
              const data = JSON.parse(buffer.slice(6));
              if (data.type === "log") {
                onLog((data as InstallLogEvent).message);
              } else {
                onProgress(data as InstallProgress);
              }
            } catch {
              /* skip */
            }
          }
          break;
        }
      }
    } catch (e) {
      if ((e as Error).name !== "AbortError") {
        onProgress({
          framework_id: frameworkId,
          phase: "error",
          status: "error",
          progress: 0,
          message: e instanceof Error ? e.message : "Installation failed",
          error: e instanceof Error ? e.message : "Unknown error",
          started_at: new Date().toISOString(),
        });
      }
    }
  })();

  return controller;
}

// Persona API

export async function fetchPersonas(): Promise<Persona[]> {
  const res = await fetch(`${BASE}/api/personas`);
  if (!res.ok) throw new Error(`Failed to fetch personas: ${res.statusText}`);
  return res.json();
}

export async function fetchPersonaCategories(): Promise<PersonaCategory[]> {
  const res = await fetch(`${BASE}/api/personas/categories`);
  if (!res.ok)
    throw new Error(`Failed to fetch categories: ${res.statusText}`);
  return res.json();
}

export async function fetchPersona(id: string): Promise<Persona> {
  const res = await fetch(`${BASE}/api/personas/${id}`);
  if (!res.ok) throw new Error(`Failed to fetch persona: ${res.statusText}`);
  return res.json();
}

export async function installPersona(personaId: string): Promise<Persona> {
  const res = await fetch(`${BASE}/api/personas/install`, {
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
  const res = await fetch(`${BASE}/api/personas/${id}`, {
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
  const res = await fetch(`${BASE}/api/personas/${id}`, { method: "DELETE" });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to delete persona: ${res.statusText}`);
  }
}

// Instance API

export async function fetchInstances(): Promise<AgentInstance[]> {
  const res = await fetch(`${BASE}/api/instances`);
  if (!res.ok) throw new Error(`Failed to fetch instances: ${res.statusText}`);
  return res.json();
}

export async function fetchInstance(id: string): Promise<AgentInstance> {
  const res = await fetch(`${BASE}/api/instances/${id}`);
  if (!res.ok) throw new Error(`Failed to fetch instance: ${res.statusText}`);
  return res.json();
}

export async function createInstance(req: CreateInstanceRequest): Promise<AgentInstance> {
  const res = await fetch(`${BASE}/api/instances`, {
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
  const res = await fetch(`${BASE}/api/instances/${id}`, { method: "DELETE" });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to delete instance: ${res.statusText}`);
  }
}

export async function instanceAction(id: string, action: "start" | "stop" | "restart"): Promise<void> {
  const res = await fetch(`${BASE}/api/instances/${id}/${action}`, { method: "POST" });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to ${action} instance: ${res.statusText}`);
  }
}

// Project API

export async function fetchProjects(): Promise<Project[]> {
  const res = await fetch(`${BASE}/api/projects`);
  if (!res.ok) throw new Error(`Failed to fetch projects: ${res.statusText}`);
  return res.json();
}

export async function createProject(req: CreateProjectRequest): Promise<Project> {
  const res = await fetch(`${BASE}/api/projects`, {
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
  const res = await fetch(`${BASE}/api/projects/${id}`, {
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
  const res = await fetch(`${BASE}/api/projects/${id}`, { method: "DELETE" });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to delete project: ${res.statusText}`);
  }
}

// Hierarchy API

export async function fetchHierarchy(): Promise<HierarchyTree> {
  const res = await fetch(`${BASE}/api/hierarchy`);
  if (!res.ok) throw new Error(`Failed to fetch hierarchy: ${res.statusText}`);
  return res.json();
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
      const res = await fetch(`${BASE}/api/hierarchy/commander/brief`, {
        method: "POST",
        signal: controller.signal,
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({ error: res.statusText }));
        onEvent({ type: "error", error: body.error || res.statusText });
        resolveSession!("");
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
              const ev = JSON.parse(line.slice(6));
              if (ev.type === "session" && ev.session_key) {
                resolveSession!(ev.session_key); sessionResolved = true;
              }
              onEvent(ev);
            } catch { /* skip */ }
          }
        }
        if (done) {
          // Process any trailing data left in buffer
          if (buffer.startsWith("data: ")) {
            try {
              const ev = JSON.parse(buffer.slice(6));
              if (ev.type === "session" && ev.session_key) {
                resolveSession!(ev.session_key); sessionResolved = true;
              }
              onEvent(ev);
            } catch { /* skip */ }
          }
          break;
        }
      }
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
      const res = await fetch(`${BASE}/api/projects/${projectId}/captain/brief`, {
        method: "POST",
        signal: controller.signal,
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({ error: res.statusText }));
        onEvent({ type: "error", error: body.error || res.statusText });
        resolveSession!("");
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
              const ev = JSON.parse(line.slice(6));
              if (ev.type === "session" && ev.session_key) {
                resolveSession!(ev.session_key); sessionResolved = true;
              }
              onEvent(ev);
            } catch { /* skip */ }
          }
        }
        if (done) {
          if (buffer.startsWith("data: ")) {
            try {
              const ev = JSON.parse(buffer.slice(6));
              if (ev.type === "session" && ev.session_key) {
                resolveSession!(ev.session_key); sessionResolved = true;
              }
              onEvent(ev);
            } catch { /* skip */ }
          }
          break;
        }
      }
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
  const res = await fetch(`${BASE}/api/projects/${projectId}/chat`);
  if (!res.ok) return [];
  return res.json();
}

export interface ProjectChatEvent {
  type: "message" | "agent_event" | "done" | "error";
  message?: ProjectChatMessage;
  sender?: string;
  role?: string;
  event?: ChatEvent;
  error?: string;
}

export function streamProjectChat(
  projectId: string,
  message: string,
  onEvent: (event: ProjectChatEvent) => void,
): AbortController {
  const controller = new AbortController();
  (async () => {
    try {
      const res = await fetch(`${BASE}/api/projects/${projectId}/chat`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ message }),
        signal: controller.signal,
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({ error: res.statusText }));
        onEvent({ type: "error", error: body.error || res.statusText });
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
            try { onEvent(JSON.parse(line.slice(6))); } catch { /* skip */ }
          }
        }
        if (done) break;
      }
    } catch (e) {
      if ((e as Error).name !== "AbortError") {
        onEvent({ type: "error", error: e instanceof Error ? e.message : "Chat failed" });
      }
    }
  })();
  return controller;
}

export async function setCommander(opts: { instanceId?: string; agentName?: string }): Promise<void> {
  const res = await fetch(`${BASE}/api/hierarchy/commander`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ instance_id: opts.instanceId, agent_name: opts.agentName }),
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Failed to set commander: ${res.statusText}`);
  }
}
