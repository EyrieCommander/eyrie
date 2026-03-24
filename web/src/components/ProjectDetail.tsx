import { useState, useEffect, useCallback, useRef } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { ArrowLeft, Plus, Trash2, Briefcase, ChevronRight, Crown } from "lucide-react";
import { MessageSquare, Send } from "lucide-react";
import type { Project, AgentInstance, AgentInfo, Persona, ProjectChatMessage, ChatPart } from "../lib/types";
import { PartToolCallCard, StreamingCursor } from "./ChatPanel";
import { fetchProjects, fetchInstances, fetchAgents, fetchPersonas, fetchHierarchy, createInstance, deleteProject, updateProject, streamCaptainBriefing, fetchProjectChat, streamProjectChat, agentAction, instanceAction } from "../lib/api";

function InstanceRow({ instance, onClick }: { instance: AgentInstance; onClick: () => void }) {
  const isProvisioning = instance.status === "created" || instance.status === "provisioning" || instance.status === "starting";
  const statusColor = isProvisioning
    ? "bg-yellow-400"
    : instance.status === "running"
      ? "bg-green"
      : instance.status === "error"
        ? "bg-red"
        : "bg-text-muted";
  return (
    <button
      onClick={onClick}
      className={`flex w-full items-center gap-3 rounded border px-4 py-3 text-left text-xs transition-all ${
        isProvisioning
          ? "border-yellow-400/30 bg-yellow-400/5 hover:border-yellow-400/50 hover:bg-yellow-400/10"
          : "border-border bg-surface hover:border-accent/50 hover:bg-surface-hover/50"
      }`}
    >
      <span className={`h-1.5 w-1.5 rounded-full ${statusColor} ${isProvisioning ? "animate-pulse" : ""}`} />
      <div className="flex-1 min-w-0">
        <span className="font-medium text-text">{instance.display_name}</span>
        <span className="ml-2 text-text-muted">{instance.framework} · :{instance.port}</span>
        {isProvisioning && (
          <span className="ml-2 rounded bg-yellow-400/10 px-1.5 py-0.5 text-[10px] font-medium text-yellow-400">
            provisioning...
          </span>
        )}
        {!isProvisioning && instance.hierarchy_role && (
          <span className="ml-2 rounded bg-accent/10 px-1.5 py-0.5 text-[10px] font-medium text-accent">
            {instance.hierarchy_role}
          </span>
        )}
      </div>
      <ChevronRight className="h-3.5 w-3.5 text-text-muted" />
    </button>
  );
}

function SetCaptainDialog({
  projectId,
  projectName,
  onDone,
  onClose,
}: {
  projectId: string;
  projectName: string;
  onDone: () => void;
  onClose: () => void;
}) {
  const [mode, setMode] = useState<"create" | "existing">("create");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [startingCaptain, setStartingCaptain] = useState("");

  // Create new form — default name derived from project
  const defaultName = `captain-${projectName.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "")}`;
  const [name, setName] = useState("");
  const [framework, setFramework] = useState("openclaw");
  const [captainInstances, setCaptainInstances] = useState<AgentInstance[]>([]);

  const refreshInstances = useCallback(() => {
    fetchInstances().then((all) => {
      setCaptainInstances(all.filter((i) => i.hierarchy_role === "captain"));
    }).catch((err) => { console.error("Failed to fetch instances:", err); });
  }, []);

  useEffect(() => {
    refreshInstances();
  }, [refreshInstances]);

  const handleCreate = async () => {
    const effectiveName = name.trim() || defaultName;
    setSaving(true);
    setError("");
    try {
      const inst = await createInstance({
        name: effectiveName,
        framework,
        hierarchy_role: "captain",
        project_id: projectId,
        auto_start: true,
      });
      await updateProject(projectId, { orchestrator_id: inst.id });
      // Brief the captain on the project (fire and forget — it runs in background)
      streamCaptainBriefing(projectId, (ev) => {
        if (ev.type === "error") {
          console.error("Captain briefing failed:", ev.error);
        }
      });
      onDone();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to create captain");
    } finally {
      setSaving(false);
    }
  };

  const handleSelectExisting = async (agentName: string) => {
    setSaving(true);
    setError("");
    try {
      await updateProject(projectId, { orchestrator_id: agentName });
      onDone();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to set captain");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={onClose}>
      <div className="w-full max-w-md rounded border border-border bg-bg p-6 space-y-4" onClick={(e) => e.stopPropagation()}>
        <h2 className="text-sm font-bold text-text">assign captain</h2>

        {error && (
          <div className="rounded border border-red/30 bg-red/5 px-3 py-2 text-xs text-red">{error}</div>
        )}

        {mode === "create" ? (
          <div className="space-y-3">
            <p className="text-xs text-text-muted">create a dedicated captain agent for this project</p>
            <div>
              <label className="block text-xs font-medium text-text-secondary mb-1">name</label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="w-full rounded border border-border bg-surface px-3 py-2 text-xs text-text focus:border-accent focus:outline-none"
                placeholder={defaultName}
                autoFocus
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-text-secondary mb-1">framework</label>
              <select
                value={framework}
                onChange={(e) => setFramework(e.target.value)}
                className="w-full rounded border border-border bg-surface px-3 py-2 text-xs text-text focus:border-accent focus:outline-none"
              >
                <option value="openclaw">OpenClaw</option>
                <option value="zeroclaw">ZeroClaw</option>
                <option value="hermes">Hermes</option>
              </select>
            </div>
            <p className="text-[10px] text-text-muted">
              the captain will use the built-in project manager identity
            </p>
            <div className="flex items-center justify-between">
              <button
                onClick={() => setMode("existing")}
                className="text-xs text-green hover:text-green/80 transition-colors"
              >
                use existing captain
              </button>
              <div className="flex gap-2">
                <button onClick={onClose} className="rounded border border-border px-3 py-1.5 text-xs text-text-secondary hover:bg-surface-hover">
                  cancel
                </button>
                <button
                  onClick={handleCreate}
                  disabled={saving}
                  className="rounded bg-accent px-3 py-1.5 text-xs font-medium text-white hover:bg-accent/80 disabled:opacity-50"
                >
                  {saving ? "creating..." : "create captain"}
                </button>
              </div>
            </div>
          </div>
        ) : (
          <div className="space-y-3">
            <p className="text-xs text-text-muted">select an existing captain instance</p>
            <div className="space-y-1.5 max-h-64 overflow-y-auto">
              {captainInstances.length === 0 ? (
                <div className="rounded border border-border bg-surface p-4 text-center text-xs text-text-muted">
                  no captain instances available
                </div>
              ) : (
                captainInstances.map((inst) => {
                  const isStopped = inst.status !== "running";
                  return (
                    <div
                      key={inst.id}
                      className="flex items-center gap-3 rounded border border-border bg-surface px-4 py-3 text-xs transition-all hover:border-green/50 hover:bg-surface-hover/50"
                    >
                      <button
                        onClick={() => handleSelectExisting(inst.id)}
                        disabled={saving}
                        className="flex flex-1 items-center gap-3 text-left disabled:opacity-50"
                      >
                        <span className={`h-1.5 w-1.5 rounded-full ${isStopped ? "bg-text-muted" : "bg-green"}`} />
                        <div className="flex-1">
                          <span className="font-medium text-text">{inst.display_name}</span>
                          <span className="ml-2 text-text-muted">{inst.framework} · :{inst.port}</span>
                          {inst.project_id && (
                            <span className="ml-2 text-[10px] text-text-muted">(assigned)</span>
                          )}
                        </div>
                      </button>
                      {isStopped && (
                        <button
                          disabled={startingCaptain === inst.id}
                          onClick={async () => {
                            setStartingCaptain(inst.id);
                            try {
                              await instanceAction(inst.id, "start");
                              setTimeout(refreshInstances, 2000);
                            } catch (e) {
                              setError(e instanceof Error ? e.message : "failed to start captain");
                            } finally {
                              setStartingCaptain("");
                            }
                          }}
                          className="shrink-0 rounded bg-green px-2 py-0.5 text-[10px] font-medium text-white hover:bg-green/80 disabled:opacity-50"
                        >
                          {startingCaptain === inst.id ? "starting..." : "start"}
                        </button>
                      )}
                    </div>
                  );
                })
              )}
            </div>
            <div className="flex items-center justify-between">
              <button
                onClick={() => setMode("create")}
                className="text-xs text-green hover:text-green/80 transition-colors"
              >
                &larr; create new instead
              </button>
              <button onClick={onClose} className="rounded border border-border px-3 py-1.5 text-xs text-text-secondary hover:bg-surface-hover">
                cancel
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function AddAgentDialog({
  projectId,
  onCreated,
  onClose,
}: {
  projectId: string;
  onCreated: () => void;
  onClose: () => void;
}) {
  const [name, setName] = useState("");
  const [framework, setFramework] = useState("openclaw");
  const [personaId, setPersonaId] = useState("");
  const [personas, setPersonas] = useState<Persona[]>([]);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    fetchPersonas().then(setPersonas).catch((err) => {
      console.error("Failed to load personas:", err);
      setPersonas([]);
    });
  }, []);

  const handleCreate = async () => {
    const trimmedName = name.trim();
    if (!trimmedName) {
      setError("Name cannot be blank");
      return;
    }
    setCreating(true);
    setError("");
    try {
      await createInstance({
        name: trimmedName,
        framework,
        persona_id: personaId || undefined,
        hierarchy_role: "talon",
        project_id: projectId,
        auto_start: true,
      });
      onCreated();
      onClose();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to create agent");
    } finally {
      setCreating(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={onClose}>
      <div className="w-full max-w-md rounded border border-border bg-bg p-6 space-y-4" onClick={(e) => e.stopPropagation()}>
        <h2 className="text-sm font-bold text-text">add agent to project</h2>

        <div>
          <label className="block text-xs font-medium text-text-secondary mb-1">name</label>
          <input
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="w-full rounded border border-border bg-surface px-3 py-2 text-xs text-text focus:border-accent focus:outline-none"
            placeholder="researcher-riley"
            autoFocus
          />
        </div>

        <div>
          <label className="block text-xs font-medium text-text-secondary mb-1">framework</label>
          <select
            value={framework}
            onChange={(e) => setFramework(e.target.value)}
            className="w-full rounded border border-border bg-surface px-3 py-2 text-xs text-text focus:border-accent focus:outline-none"
          >
            <option value="zeroclaw">ZeroClaw</option>
            <option value="openclaw">OpenClaw</option>
            <option value="hermes">Hermes</option>
          </select>
        </div>

        <div>
          <label className="block text-xs font-medium text-text-secondary mb-1">persona (optional)</label>
          <select
            value={personaId}
            onChange={(e) => setPersonaId(e.target.value)}
            className="w-full rounded border border-border bg-surface px-3 py-2 text-xs text-text focus:border-accent focus:outline-none"
          >
            <option value="">none</option>
            {personas.map((p) => (
              <option key={p.id} value={p.id}>{p.icon} {p.name} — {p.role}</option>
            ))}
          </select>
        </div>

        {error && (
          <div className="rounded border border-red/30 bg-red/5 px-3 py-2 text-xs text-red">{error}</div>
        )}

        <div className="flex justify-end gap-2">
          <button onClick={onClose} className="rounded border border-border px-3 py-1.5 text-xs text-text-secondary hover:bg-surface-hover">
            cancel
          </button>
          <button
            onClick={handleCreate}
            disabled={creating || !name.trim()}
            className="rounded bg-accent px-3 py-1.5 text-xs font-medium text-white hover:bg-accent/80 disabled:opacity-50"
          >
            {creating ? "creating..." : "create agent"}
          </button>
        </div>
      </div>
    </div>
  );
}

const ROLE_COLORS: Record<string, string> = {
  user: "text-green",
  commander: "text-purple",
  captain: "text-yellow-400",
  talon: "text-blue-400",
  system: "text-text-muted",
};

function ProjectChat({ projectId, participants }: { projectId: string; participants: { name: string; role: string }[] }) {
  const [messages, setMessages] = useState<ProjectChatMessage[]>([]);
  const [input, setInput] = useState("");
  const [sending, setSending] = useState(false);
  const [chatError, setChatError] = useState("");
  const [streamingSender, setStreamingSender] = useState<string | null>(null);
  const [streamingRole, setStreamingRole] = useState<string | null>(null);
  const [streamingContent, setStreamingContent] = useState("");
  const [streamingToolCalls, setStreamingToolCalls] = useState<ChatPart[]>([]);
  const [showMentions, setShowMentions] = useState(false);
  const [mentionFilter, setMentionFilter] = useState("");
  const [mentionIdx, setMentionIdx] = useState(0);
  const scrollRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const streamControllerRef = useRef<AbortController | null>(null);
  const mountedRef = useRef(true);

  // Load chat history and poll for new messages
  useEffect(() => {
    fetchProjectChat(projectId).then(setMessages).catch(() => {});
    const interval = setInterval(() => {
      if (sending) return;
      fetchProjectChat(projectId).then((msgs) => {
        setMessages((prev) => {
          if (msgs.length !== prev.length) return msgs;
          return prev;
        });
      }).catch(() => {});
    }, 4000);
    return () => clearInterval(interval);
  }, [projectId, sending]);

  // Auto-scroll
  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [messages, streamingContent]);

  useEffect(() => {
    return () => {
      mountedRef.current = false;
      streamControllerRef.current?.abort();
    };
  }, []);

  const handleSend = useCallback(() => {
    const msg = input.trim();
    if (!msg || sending) return;
    setInput("");
    setSending(true);
    setChatError("");
    setStreamingContent("");
    setStreamingSender(null);
    setStreamingToolCalls([]);

    const controller = streamProjectChat(projectId, msg, (event) => {
      if (event.type === "message" && event.message) {
        setMessages((prev) => [...prev, event.message!]);
        setStreamingContent("");
        setStreamingSender(null);
        setStreamingRole(null);
        setStreamingToolCalls([]);
      } else if (event.type === "agent_event" && event.event) {
        if (event.event.type === "delta") {
          setStreamingSender(event.sender || null);
          setStreamingRole(event.role || null);
          setStreamingContent((prev) => prev + (event.event!.content || ""));
        } else if (event.event.type === "tool_start") {
          setStreamingSender(event.sender || null);
          setStreamingRole(event.role || null);
          setStreamingToolCalls((prev) => [...prev, {
            type: "tool_call",
            id: event.event!.tool_id,
            name: event.event!.tool,
          }]);
        } else if (event.event.type === "tool_result") {
          setStreamingToolCalls((prev) => {
            const updated = [...prev];
            for (let i = updated.length - 1; i >= 0; i--) {
              if ((event.event!.tool_id && updated[i].id === event.event!.tool_id) ||
                  (!event.event!.tool_id && updated[i].name === event.event!.tool && !updated[i].output)) {
                updated[i] = { ...updated[i], output: event.event!.output };
                break;
              }
            }
            return updated;
          });
        } else if (event.event.type === "error") {
          const agentName = event.sender || "agent";
          const errContent = event.event.content || "unknown error";
          setMessages((prev) => [...prev, {
            id: `err-${Date.now()}`,
            sender: agentName,
            role: event.role || "system",
            content: `error: ${errContent}`,
            timestamp: new Date().toISOString(),
          }]);
          setStreamingContent("");
          setStreamingSender(null);
          setStreamingRole(null);
        }
      } else if (event.type === "done") {
        setSending(false);
        setStreamingContent("");
        setStreamingSender(null);
        setStreamingRole(null);
        setStreamingToolCalls([]);
      } else if (event.type === "error") {
        setSending(false);
        setChatError(event.error || "failed to send message");
      }
    });
    streamControllerRef.current = controller;
  }, [input, sending, projectId]);

  const startProjectChat = useCallback(() => {
    setSending(true);
    setChatError("");
    // Brief the captain first (fetches API ref, saves TOOLS.md), then start the chat
    const { sessionReady } = streamCaptainBriefing(projectId, () => {});
    sessionReady.then(() => {
      if (!mountedRef.current) return;
      const controller = streamProjectChat(projectId, "Let's get started on this project.", (event) => {
        if (!mountedRef.current) return;
        if (event.type === "message" && event.message) {
          setMessages((prev) => [...prev, event.message!]);
        } else if (event.type === "done") {
          setSending(false);
        } else if (event.type === "error") {
          setSending(false);
          setChatError(event.error || "failed to start chat");
        }
      });
      streamControllerRef.current = controller;
    }).catch((err) => {
      console.error("Captain briefing failed:", err);
      if (!mountedRef.current) return;
      setSending(false);
      setChatError("failed to brief captain");
    });
  }, [projectId]);

  return (
    <div className="flex flex-col resize-y overflow-hidden" style={{ height: "calc(100vh - 300px)", minHeight: "300px", maxHeight: "calc(100vh - 120px)" }}>
      {/* Messages */}
      <div ref={scrollRef} className="flex-1 overflow-y-auto space-y-3 p-4">
        {messages.length === 0 && !sending && (
          <div className="text-center py-10 space-y-4">
            <p className="text-xs text-text-muted">
              start the project chat to bring the team together
            </p>
            {chatError && (
              <div className="rounded border border-red/30 bg-red/5 px-4 py-2 text-xs text-red max-w-sm mx-auto">
                {chatError}
              </div>
            )}
            <button
              onClick={startProjectChat}
              disabled={sending}
              className="rounded bg-accent px-4 py-2 text-xs font-medium text-white hover:bg-accent/80 disabled:opacity-50"
            >
              start project chat
            </button>
          </div>
        )}
        {messages.map((msg) => {
          const parts = msg.parts ?? [];
          const toolParts = parts.filter((p) => p.type === "tool_call");
          const hasParts = toolParts.length > 0;
          return (
            <div key={msg.id} className="text-xs">
              <div className="flex items-baseline gap-2">
                <span className={`font-bold ${ROLE_COLORS[msg.role] || "text-text"}`}>
                  {msg.role === "user" ? "you" : msg.role}
                </span>
                {msg.role !== "user" && msg.sender !== msg.role && (
                  <span className="text-[10px] text-text-muted">({msg.sender})</span>
                )}
                <span className="text-[10px] text-text-muted">
                  {new Date(msg.timestamp).toLocaleTimeString()}
                </span>
                {hasParts && (
                  <span className="text-[10px] text-accent/60">
                    [{toolParts.length} tool{toolParts.length > 1 ? "s" : ""}]
                  </span>
                )}
              </div>
              {hasParts && (
                <div className="mt-1 space-y-1">
                  {toolParts.map((tc, i) => (
                    <PartToolCallCard key={`${msg.id}-tc-${i}`} part={tc} />
                  ))}
                </div>
              )}
              <div className="mt-0.5 text-text whitespace-pre-wrap">{msg.content}</div>
            </div>
          );
        })}
        {sending && !streamingContent && !streamingSender && streamingToolCalls.length === 0 && (
          <div className="text-xs py-1 text-text-muted animate-pulse">
            <span className={`font-bold ${ROLE_COLORS["commander"]}`}>agent</span>{" "}
            thinking...
          </div>
        )}
        {streamingSender && (
          <div className="text-xs">
            <div className="flex items-baseline gap-2">
              <span className={`font-bold ${ROLE_COLORS[streamingRole || "captain"] || "text-text"}`}>
                {streamingRole || "agent"}
              </span>
              {streamingSender !== streamingRole && (
                <span className="text-[10px] text-text-muted">({streamingSender})</span>
              )}
            </div>
            {streamingToolCalls.length > 0 && (
              <div className="mt-1 space-y-1">
                {streamingToolCalls.map((tc, i) => (
                  <div key={`stc-${i}`} className="rounded border border-border bg-surface px-2 py-1">
                    <div className="flex items-center gap-2">
                      <span className="text-accent text-[10px] font-mono">{tc.name}</span>
                      {!tc.output && <span className="text-[10px] text-text-muted animate-pulse">running...</span>}
                    </div>
                  </div>
                ))}
              </div>
            )}
            {streamingContent && (
              <div className="mt-0.5 text-text whitespace-pre-wrap">
                {streamingContent}
                <StreamingCursor />
              </div>
            )}
          </div>
        )}
      </div>

      {/* Input */}
      <div className="relative border-t border-border p-3 flex gap-2">
        {showMentions && (() => {
          const filtered = participants.filter((p) => !mentionFilter || p.role.includes(mentionFilter) || p.name.includes(mentionFilter));
          if (filtered.length === 0) return null;
          return (
            <div className="absolute bottom-full left-3 mb-1 rounded border border-border bg-bg shadow-lg py-1 min-w-[160px]">
              {filtered.map((p, i) => (
                <button
                  key={p.name}
                  onClick={() => {
                    const atIdx = input.lastIndexOf("@");
                    const before = atIdx >= 0 ? input.slice(0, atIdx) : input;
                    setInput(before + "@" + p.role + " ");
                    setShowMentions(false);
                    inputRef.current?.focus();
                  }}
                  className={`flex w-full items-center gap-2 px-3 py-1.5 text-xs text-left ${i === mentionIdx ? "bg-surface-hover" : "hover:bg-surface-hover"}`}
                >
                  <span className={`font-bold ${ROLE_COLORS[p.role] || "text-text"}`}>{p.role}</span>
                  <span className="text-text-muted">{p.name}</span>
                </button>
              ))}
            </div>
          );
        })()}
        <input
          ref={inputRef}
          type="text"
          value={input}
          onChange={(e) => {
            const val = e.target.value;
            setInput(val);
            const atIdx = val.lastIndexOf("@");
            if (atIdx >= 0 && (atIdx === 0 || val[atIdx - 1] === " ")) {
              const partial = val.slice(atIdx + 1).toLowerCase();
              setMentionFilter(partial);
              setShowMentions(true);
              setMentionIdx(0);
            } else {
              setShowMentions(false);
            }
          }}
          onKeyDown={(e) => {
            if (e.key === "Escape") { setShowMentions(false); return; }
            if (showMentions) {
              const filtered = participants.filter((p) => !mentionFilter || p.role.includes(mentionFilter) || p.name.includes(mentionFilter));
              if (e.key === "ArrowDown") { e.preventDefault(); setMentionIdx((prev) => Math.min(prev + 1, filtered.length - 1)); return; }
              if (e.key === "ArrowUp") { e.preventDefault(); setMentionIdx((prev) => Math.max(prev - 1, 0)); return; }
              if ((e.key === "Enter" || e.key === "Tab") && filtered.length > 0) {
                e.preventDefault();
                const p = filtered[mentionIdx];
                const atIdx = input.lastIndexOf("@");
                const before = atIdx >= 0 ? input.slice(0, atIdx) : input;
                setInput(before + "@" + p.role + " ");
                setShowMentions(false);
                return;
              }
            }
            if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); handleSend(); }
          }}
          className="flex-1 rounded border border-border bg-surface px-3 py-2 text-xs text-text focus:border-accent focus:outline-none"
          placeholder="type a message... (@ to mention)"
          disabled={sending}
        />
        <button
          onClick={handleSend}
          disabled={sending || !input.trim()}
          className="rounded bg-accent px-3 py-2 text-xs font-medium text-white hover:bg-accent/80 disabled:opacity-50"
        >
          <Send className="h-3.5 w-3.5" />
        </button>
      </div>
    </div>
  );
}

export default function ProjectDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [project, setProject] = useState<Project | null>(null);
  const [instances, setInstances] = useState<AgentInstance[]>([]);
  const [agents, setAgents] = useState<AgentInfo[]>([]);
  const [showAddAgent, setShowAddAgent] = useState(false);
  const [showSetOrchestrator, setShowSetOrchestrator] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [tab, setTab] = useState<"team" | "chat">("chat");
  const [commanderName, setCommanderName] = useState("");
  const [commanderStatus, setCommanderStatus] = useState("");
  const [startingAgent, setStartingAgent] = useState("");
  const hasLoadedRef = useRef(false);
  const pollRef = useRef<{ interval: ReturnType<typeof setInterval> | null; timeout: ReturnType<typeof setTimeout> | null }>({ interval: null, timeout: null });

  const refresh = useCallback(async () => {
    if (!id) return;
    try {
      if (!hasLoadedRef.current) setLoading(true);
      setLoadError("");
      const [projects, allInstances, allAgents, hierarchy] = await Promise.all([
        fetchProjects(),
        fetchInstances(),
        fetchAgents(),
        fetchHierarchy().catch(() => null),
      ]);
      if (hierarchy?.commander) {
        setCommanderName(hierarchy.commander.name);
        setCommanderStatus(hierarchy.commander.status);
      }
      const p = projects.find((p) => p.id === id);
      setProject(p ?? null);
      setAgents(allAgents);
      if (p) {
        const projectInstances = allInstances.filter(
          (inst) =>
            inst.project_id === id ||
            p.orchestrator_id === inst.id ||
            p.role_agent_ids?.includes(inst.id),
        );
        setInstances(projectInstances);
      }
      hasLoadedRef.current = true;
    } catch (err) {
      console.error("Failed to load project data:", err);
      setLoadError(err instanceof Error ? err.message : "Failed to load project");
    } finally {
      setLoading(false);
    }
  }, [id]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  // Cleanup pollRef on unmount
  useEffect(() => {
    return () => {
      if (pollRef.current.interval) clearInterval(pollRef.current.interval);
      if (pollRef.current.timeout) clearTimeout(pollRef.current.timeout);
    };
  }, []);

  // Poll while any instance is provisioning
  useEffect(() => {
    const hasProvisioning = instances.some((i) => i.status === "created" || i.status === "provisioning" || i.status === "starting");
    if (!hasProvisioning) return;
    const interval = setInterval(refresh, 3000);
    return () => clearInterval(interval);
  }, [instances, refresh]);

  if (loading && !project) {
    return <div className="py-20 text-center text-xs text-text-muted">loading project...</div>;
  }

  if (!project) {
    return (
      <div className="py-20 text-center text-xs text-text-muted">
        project not found
      </div>
    );
  }

  // Orchestrator can be a provisioned instance or a legacy agent name
  const captainInstance = instances.find((i) => i.id === project.orchestrator_id);
  const captainAgent = !captainInstance
    ? agents.find((a) => a.name === project.orchestrator_id)
    : null;
  const hasCaptain = captainInstance || captainAgent;
  const roleAgents = instances.filter((i) => i.hierarchy_role === "talon");

  return (
    <div className="space-y-6">
      <div className="text-xs text-text-muted">~/projects/{project.name}</div>

      {loadError && (
        <div className="rounded border border-red/30 bg-red/5 px-3 py-2 text-xs text-red">{loadError}</div>
      )}

      <div className="flex items-center gap-3">
        <button
          onClick={() => navigate("/projects")}
          className="rounded p-1 text-text-muted transition-colors hover:bg-surface-hover hover:text-text"
        >
          <ArrowLeft className="h-4 w-4" />
        </button>
        <div className="flex-1">
          <div className="flex items-center gap-2">
            <Briefcase className="h-4 w-4 text-green" />
            <h1 className="text-xl font-bold text-text">{project.name}</h1>
            <span className="rounded bg-green/10 px-1.5 py-0.5 text-[10px] font-medium text-green">
              {project.status}
            </span>
          </div>
          {project.description && (
            <p className="mt-1 text-xs text-text-muted">{project.description}</p>
          )}
          {project.goal && (
            <p className="mt-0.5 text-xs text-text-secondary">goal: {project.goal}</p>
          )}
        </div>
        <button
          onClick={async () => {
            if (confirm("delete this project?")) {
              try {
                await deleteProject(project.id);
                navigate("/projects");
              } catch (e) {
                setLoadError(e instanceof Error ? e.message : "Failed to delete project");
              }
            }
          }}
          className="rounded p-2 text-text-muted transition-colors hover:bg-red/10 hover:text-red"
          title="delete project"
        >
          <Trash2 className="h-3.5 w-3.5" />
        </button>
      </div>

      {/* Tabs */}
      <div className="flex gap-4 border-b border-border">
        <button
          onClick={() => setTab("chat")}
          className={`flex items-center gap-1.5 pb-2 text-xs font-medium transition-colors ${
            tab === "chat" ? "border-b-2 border-accent text-accent" : "text-text-muted hover:text-text"
          }`}
        >
          <MessageSquare className="h-3.5 w-3.5" /> chat
        </button>
        <button
          onClick={() => setTab("team")}
          className={`flex items-center gap-1.5 pb-2 text-xs font-medium transition-colors ${
            tab === "team" ? "border-b-2 border-accent text-accent" : "text-text-muted hover:text-text"
          }`}
        >
          <Crown className="h-3.5 w-3.5" /> team
        </button>
      </div>

      {/* Chat tab */}
      {tab === "chat" && (() => {
        if (!commanderName) {
          return (
            <div className="py-10 text-center space-y-3">
              <p className="text-xs text-text-muted">no commander set up yet</p>
              <button
                onClick={() => navigate("/hierarchy")}
                className="rounded bg-accent px-4 py-2 text-xs font-medium text-white hover:bg-accent/80"
              >
                set up commander
              </button>
            </div>
          );
        }

        if (!hasCaptain) {
          return (
            <div className="py-10 text-center space-y-3">
              <p className="text-xs text-text-muted">assign a captain to start the project chat</p>
              <button
                onClick={() => setTab("team")}
                className="rounded bg-accent px-4 py-2 text-xs font-medium text-white hover:bg-accent/80"
              >
                assign captain
              </button>
            </div>
          );
        }

        // Check which required agents are stopped
        const stoppedAgents: { name: string; role: string; isInstance: boolean; id: string }[] = [];
        if (commanderStatus !== "running") {
          const cmdInst = instances.find((i) => i.name === commanderName);
          stoppedAgents.push({ name: commanderName, role: "commander", isInstance: !!cmdInst, id: cmdInst?.id || commanderName });
        }
        if (captainInstance && captainInstance.status !== "running") {
          stoppedAgents.push({ name: captainInstance.display_name || captainInstance.name, role: "captain", isInstance: true, id: captainInstance.id });
        }
        if (captainAgent && !captainAgent.alive) {
          stoppedAgents.push({ name: captainAgent.name, role: "captain", isInstance: false, id: captainAgent.name });
        }

        if (stoppedAgents.length > 0) {
          const pollUntilRunning = () => {
            // Clear any existing poll to prevent overlapping intervals
            if (pollRef.current.interval) clearInterval(pollRef.current.interval);
            if (pollRef.current.timeout) clearTimeout(pollRef.current.timeout);

            const poll = setInterval(async () => {
              await refresh();
            }, 2000);
            pollRef.current.interval = poll;
            // Stop polling after 30s as a safety net
            const timeout = setTimeout(() => {
              clearInterval(poll);
              pollRef.current.interval = null;
              pollRef.current.timeout = null;
              setStartingAgent("");
            }, 30000);
            pollRef.current.timeout = timeout;
          };

          return (
            <div className="py-10 text-center space-y-4">
              <p className="text-xs text-text-muted">
                these agents need to be running before starting the chat
              </p>
              <div className="flex flex-col items-center gap-2">
                {stoppedAgents.map((a) => (
                  <div key={a.id} className="flex items-center gap-3 rounded border border-border bg-surface px-4 py-2 text-xs">
                    <span className={`h-1.5 w-1.5 rounded-full ${startingAgent === a.id || startingAgent === "all" ? "bg-yellow-400 animate-pulse" : "bg-text-muted"}`} />
                    <span className="font-medium text-text">{a.name}</span>
                    <span className="text-text-muted">{a.role}</span>
                    <button
                      disabled={!!startingAgent}
                      onClick={async () => {
                        setStartingAgent(a.id);
                        try {
                          if (a.isInstance) await instanceAction(a.id, "start");
                          else await agentAction(a.id, "start");
                          pollUntilRunning();
                        } catch (e) {
                          setLoadError(e instanceof Error ? e.message : "failed to start agent");
                          setStartingAgent("");
                        }
                      }}
                      className="rounded bg-green px-2 py-0.5 text-[10px] font-medium text-white hover:bg-green/80 disabled:opacity-50"
                    >
                      {startingAgent === a.id || startingAgent === "all" ? "starting..." : "start"}
                    </button>
                  </div>
                ))}
              </div>
              {stoppedAgents.length > 1 && (
                <button
                  disabled={!!startingAgent}
                  onClick={async () => {
                    setStartingAgent("all");
                    try {
                      for (const a of stoppedAgents) {
                        if (a.isInstance) await instanceAction(a.id, "start");
                        else await agentAction(a.id, "start");
                      }
                      pollUntilRunning();
                    } catch (e) {
                      setLoadError(e instanceof Error ? e.message : "failed to start agents");
                      setStartingAgent("");
                    }
                  }}
                  className="rounded bg-accent px-4 py-2 text-xs font-medium text-white hover:bg-accent/80 disabled:opacity-50"
                >
                  {startingAgent === "all" ? "starting..." : "start all"}
                </button>
              )}
            </div>
          );
        }

        return (
          <ProjectChat
            projectId={project.id}
            participants={[
              { name: commanderName, role: "commander" },
              ...(captainInstance ? [{ name: captainInstance.name, role: "captain" }] : []),
              ...(captainAgent ? [{ name: captainAgent.name, role: "captain" }] : []),
              ...roleAgents.map((a) => ({ name: a.name, role: "talon" })),
            ]}
          />
        );
      })()}

      {/* Team tab */}
      {tab !== "chat" && <>

      {/* Orchestrator */}
      <div>
        <div className="mb-2 flex items-center justify-between">
          <span className="text-[10px] font-medium uppercase tracking-wider text-text-muted">
            captain
          </span>
          {!hasCaptain && (
            <button
              onClick={() => setShowSetOrchestrator(true)}
              className="flex items-center gap-1 text-xs text-accent transition-colors hover:text-accent/80"
            >
              <Crown className="h-3 w-3" /> assign captain
            </button>
          )}
        </div>
        {captainInstance ? (
          <div className="space-y-1.5">
            <InstanceRow
              instance={captainInstance}
              onClick={() => navigate(`/agents/${captainInstance.name}/chat`)}
            />
            <button
              onClick={() => {
                const { sessionReady } = streamCaptainBriefing(project.id, (ev) => {
                  if (ev.type === "error") console.error("Captain briefing error:", ev.error);
                });
                sessionReady
                  .then(() => navigate(`/agents/${captainInstance.name}/chat?brief=captain`))
                  .catch((e) => console.error("Captain briefing session failed:", e));
              }}
              className="ml-4 text-xs text-green hover:text-green/80 transition-colors"
            >
              brief captain on project
            </button>
          </div>
        ) : captainAgent ? (
          <button
            onClick={() => navigate(`/agents/${captainAgent.name}`)}
            className="flex w-full items-center gap-3 rounded border border-border bg-surface px-4 py-3 text-left text-xs transition-all hover:border-accent/50 hover:bg-surface-hover/50"
          >
            <span className={`h-1.5 w-1.5 rounded-full ${captainAgent.alive ? "bg-green" : "bg-red"}`} />
            <div className="flex-1">
              <span className="font-medium text-text">{captainAgent.name}</span>
              <span className="ml-2 text-text-muted">{captainAgent.framework} · :{captainAgent.port}</span>
              <span className="ml-2 rounded bg-text-muted/10 px-1.5 py-0.5 text-[10px] text-text-muted">existing agent</span>
            </div>
            <ChevronRight className="h-3.5 w-3.5 text-text-muted" />
          </button>
        ) : (
          <div className="rounded border border-border bg-surface p-4 text-center text-xs text-text-muted">
            no captain assigned
          </div>
        )}
      </div>

      {showSetOrchestrator && (
        <SetCaptainDialog
          projectId={project.id}
          projectName={project.name}
          onDone={() => { setShowSetOrchestrator(false); refresh(); }}
          onClose={() => setShowSetOrchestrator(false)}
        />
      )}

      {/* Role Agents */}
      <div>
        <div className="mb-2 flex items-center justify-between">
          <span className="text-[10px] font-medium uppercase tracking-wider text-text-muted">
            role agents ({roleAgents.length})
          </span>
          <button
            onClick={() => setShowAddAgent(true)}
            className="flex items-center gap-1 text-xs text-accent transition-colors hover:text-accent/80"
          >
            <Plus className="h-3 w-3" /> add agent
          </button>
        </div>
        {roleAgents.length === 0 ? (
          <div className="rounded border border-border bg-surface p-6 text-center text-xs text-text-muted">
            no role agents yet — add one to start building your team
          </div>
        ) : (
          <div className="space-y-1.5">
            {roleAgents.map((agent) => (
              <InstanceRow
                key={agent.id}
                instance={agent}
                onClick={() => navigate(`/agents/${agent.name}`)}
              />
            ))}
          </div>
        )}
      </div>

      {showAddAgent && (
        <AddAgentDialog
          projectId={project.id}
          onCreated={refresh}
          onClose={() => setShowAddAgent(false)}
        />
      )}

      </>}
    </div>
  );
}
