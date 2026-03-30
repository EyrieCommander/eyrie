import { useState, useEffect } from "react";
import type { Persona } from "../lib/types";
import { fetchPersonas, createInstance } from "../lib/api";

export interface AddAgentDialogProps {
  projectId: string;
  onCreated: () => void;
  onClose: () => void;
}

export function AddAgentDialog({
  projectId,
  onCreated,
  onClose,
}: AddAgentDialogProps) {
  const [name, setName] = useState("");
  const [framework, setFramework] = useState("embedded");
  const [personaId, setPersonaId] = useState("");
  const [personas, setPersonas] = useState<Persona[]>([]);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (creating) return;
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [onClose, creating]);

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
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      onClick={() => { if (!creating) onClose(); }}
      role="dialog"
      aria-modal="true"
      aria-labelledby="add-agent-dialog-title"
    >
      <div className="w-full max-w-md rounded border border-border bg-bg p-6 space-y-4" onClick={(e) => e.stopPropagation()}>
        <h2 id="add-agent-dialog-title" className="text-sm font-bold text-text">add agent to project</h2>

        <div>
          <label htmlFor="agent-name" className="block text-xs font-medium text-text-secondary mb-1">name</label>
          <input
            id="agent-name"
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="w-full rounded border border-border bg-surface px-3 py-2 text-xs text-text focus:border-accent focus:outline-none"
            placeholder="researcher-riley"
            autoFocus
          />
        </div>

        <div>
          <label htmlFor="agent-framework" className="block text-xs font-medium text-text-secondary mb-1">framework</label>
          <select
            id="agent-framework"
            value={framework}
            onChange={(e) => setFramework(e.target.value)}
            className="w-full rounded border border-border bg-surface px-3 py-2 text-xs text-text focus:border-accent focus:outline-none"
          >
            <option value="embedded">Embedded (EyrieClaw)</option>
            <option value="zeroclaw">ZeroClaw</option>
            <option value="openclaw">OpenClaw</option>
            <option value="hermes">Hermes</option>
            <option value="picoclaw">PicoClaw</option>
          </select>
        </div>

        <div>
          <label htmlFor="agent-persona" className="block text-xs font-medium text-text-secondary mb-1">persona (optional)</label>
          <select
            id="agent-persona"
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
