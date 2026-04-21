// Reusable API keys management section — lists stored keys with delete,
// and a provider dropdown + input to add new ones. Used by both
// SettingsPage and CommanderPhase.

import { useEffect, useMemo, useState } from "react";
import { Key, Trash2, Loader2, Eye, EyeOff, Check, ShieldCheck, ShieldAlert, Pencil } from "lucide-react";
import { fetchKeys, setKey, deleteKey } from "../lib/api";
import type { KeyEntry } from "../lib/types";
import { KEYS_CHANGED_EVENT } from "../lib/events";

const KNOWN_PROVIDERS = ["openrouter", "anthropic", "openai", "deepseek"];

interface Props {
  /** Called after any key is added or deleted (parent can re-check health etc). */
  onChanged?: () => void;
  /** Hide the section header. Useful when embedded inside another card. */
  compact?: boolean;
}

export default function ApiKeysSection({ onChanged, compact }: Props) {
  const [keys, setKeys] = useState<KeyEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [newProvider, setNewProvider] = useState("");
  const [newKey, setNewKey] = useState("");
  const [showNewKey, setShowNewKey] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [successMsg, setSuccessMsg] = useState<string | null>(null);

  const loadKeys = async () => {
    try {
      setLoading(true);
      const data = await fetchKeys();
      setKeys(data);
    } catch {
      setKeys([]);
    } finally {
      setLoading(false);
    }
  };

  /** Shared epilogue after any successful add/update/delete. */
  const afterMutation = async (msg: string) => {
    setSuccessMsg(msg);
    await loadKeys();
    window.dispatchEvent(new Event(KEYS_CHANGED_EVENT));
    onChanged?.();
  };

  useEffect(() => { loadKeys(); }, []);

  useEffect(() => {
    if (!successMsg) return;
    const t = setTimeout(() => setSuccessMsg(null), 3000);
    return () => clearTimeout(t);
  }, [successMsg]);

  const handleAdd = async () => {
    if (!newProvider || !newKey) return;
    try {
      setSaving(true);
      setError(null);
      await setKey(newProvider, newKey);
      setNewProvider("");
      setNewKey("");
      setShowNewKey(false);
      await afterMutation(`${newProvider} key saved`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to save key");
    } finally {
      setSaving(false);
    }
  };

  const [deletingProvider, setDeletingProvider] = useState<string | null>(null);
  const handleDelete = async (provider: string) => {
    if (deletingProvider) return;
    if (!window.confirm(`Remove the ${provider} API key? This may disable the commander or framework features that depend on it.`)) return;
    try {
      setDeletingProvider(provider);
      setError(null);
      await deleteKey(provider);
      await afterMutation(`${provider} key removed`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to delete key");
    } finally {
      setDeletingProvider(null);
    }
  };

  const [editingProvider, setEditingProvider] = useState<string | null>(null);
  const [editKey, setEditKey] = useState("");
  const [showEditKey, setShowEditKey] = useState(false);
  const [editSaving, setEditSaving] = useState(false);

  const handleUpdate = async (provider: string) => {
    if (!editKey || editSaving) return;
    try {
      setEditSaving(true);
      setError(null);
      await setKey(provider, editKey);
      setEditingProvider(null);
      setEditKey("");
      setShowEditKey(false);
      await afterMutation(`${provider} key updated`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to update key");
    } finally {
      setEditSaving(false);
    }
  };

  const availableProviders = useMemo(
    () => KNOWN_PROVIDERS.filter((p) => !keys.some((k) => k.provider === p)),
    [keys],
  );

  // Default to the first available provider so the save button works immediately
  useEffect(() => {
    if (!newProvider && availableProviders.length > 0) {
      setNewProvider(availableProviders[0]);
    }
  }, [availableProviders, newProvider]);

  return (
    <div className={compact ? "space-y-3" : "rounded border border-border bg-surface p-4 space-y-3"}>
      {!compact && (
        <div className="flex items-center justify-between">
          <div>
            <h3 className="text-xs font-medium text-text flex items-center gap-1.5">
              <Key className="h-3.5 w-3.5" />
              api keys
            </h3>
            <p className="text-[10px] text-text-muted mt-0.5">
              centralized key vault — injected as env vars on agent start. changes require restart.
            </p>
          </div>
        </div>
      )}

      {error && (
        <div className="text-[10px] text-red bg-red/5 border border-red/20 rounded px-2 py-1">
          {error}
        </div>
      )}
      {successMsg && (
        <div className="flex items-center gap-1 text-[10px] text-accent">
          <Check className="h-2.5 w-2.5" /> {successMsg}
        </div>
      )}

      {loading ? (
        <div className="flex items-center gap-2 text-[10px] text-text-muted py-2">
          <Loader2 className="h-3 w-3 animate-spin" /> loading keys...
        </div>
      ) : (
        <>
          {keys.length > 0 && (
            <div className="space-y-1.5">
              {keys.map((entry) => (
                <div key={entry.provider} className="rounded border border-border px-3 py-2 text-xs space-y-2">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <ShieldCheck className="h-3.5 w-3.5 text-accent" />
                      <span className="font-medium text-text">{entry.provider}</span>
                      <span className="text-text-muted font-mono text-[10px]">{entry.masked_key}</span>
                    </div>
                    <div className="flex items-center gap-1">
                      <button
                        onClick={() => {
                          setEditingProvider(editingProvider === entry.provider ? null : entry.provider);
                          setEditKey("");
                          setShowEditKey(false);
                        }}
                        className="p-1 rounded text-text-muted hover:text-accent hover:bg-accent/5 transition-colors"
                        title="update key"
                        aria-label={`Update ${entry.provider} key`}
                      >
                        <Pencil className="h-3 w-3" />
                      </button>
                      <button
                        onClick={() => handleDelete(entry.provider)}
                        disabled={deletingProvider === entry.provider}
                        className="p-1 rounded text-text-muted hover:text-red hover:bg-red/5 transition-colors disabled:opacity-30"
                        title="remove key"
                        aria-label={`Remove ${entry.provider} key`}
                      >
                        <Trash2 className="h-3 w-3" />
                      </button>
                    </div>
                  </div>
                  {editingProvider === entry.provider && (
                    <div className="flex items-center gap-2">
                      <div className="relative flex-1">
                        <input
                          type="text"
                          value={editKey}
                          onChange={(e) => setEditKey(e.target.value)}
                          onKeyDown={(e) => { if (e.key === "Enter") handleUpdate(entry.provider); }}
                          placeholder="new key..."
                          aria-label={`New key for ${entry.provider}`}
                          autoComplete="off"
                          data-1p-ignore
                          data-lpignore="true"
                          style={showEditKey ? undefined : { WebkitTextSecurity: "disc" } as React.CSSProperties}
                          className="w-full rounded border border-border bg-bg px-2 py-1.5 pr-7 text-xs text-text font-mono focus:border-accent focus:outline-none"
                        />
                        <button
                          type="button"
                          onClick={() => setShowEditKey(!showEditKey)}
                          aria-label={showEditKey ? "hide key" : "show key"}
                          className="absolute right-1.5 top-1/2 -translate-y-1/2 text-text-muted hover:text-text"
                        >
                          {showEditKey ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
                        </button>
                      </div>
                      <button
                        onClick={() => handleUpdate(entry.provider)}
                        disabled={!editKey || editSaving}
                        className="rounded border border-accent bg-accent/5 px-3 py-1.5 text-xs font-medium text-accent hover:bg-accent/10 disabled:opacity-30 transition-colors flex items-center gap-1"
                      >
                        {editSaving ? <Loader2 className="h-3 w-3 animate-spin" /> : null}
                        update
                      </button>
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}

          {keys.length === 0 && (
            <div className="text-[10px] text-text-muted py-1 flex items-center gap-1.5">
              <ShieldAlert className="h-3 w-3" />
              no api keys configured — agents will rely on environment variables
            </div>
          )}

          {availableProviders.length > 0 && (
            <div className="space-y-1.5 pt-1">
              <div className="text-[10px] text-text-muted">
                {keys.length > 0 ? "add another provider" : "add a provider"}
              </div>
            <div className="flex items-center gap-2">
              <select
                value={newProvider}
                onChange={(e) => setNewProvider(e.target.value)}
                aria-label="API provider"
                className="rounded border border-border bg-bg px-2 py-1.5 text-xs text-text focus:border-accent focus:outline-none"
              >
                <option value="" disabled>provider...</option>
                {availableProviders.map((p) => (
                  <option key={p} value={p}>{p}</option>
                ))}
              </select>
              <div className="relative flex-1">
                <input
                  type="text"
                  value={newKey}
                  onChange={(e) => setNewKey(e.target.value)}
                  onKeyDown={(e) => { if (e.key === "Enter") handleAdd(); }}
                  placeholder="sk-..."
                  aria-label={`API key${newProvider ? ` for ${newProvider}` : ""}`}
                  autoComplete="off"
                  data-1p-ignore
                  data-lpignore="true"
                  style={showNewKey ? undefined : { WebkitTextSecurity: "disc" } as React.CSSProperties}
                  className="w-full rounded border border-border bg-bg px-2 py-1.5 pr-7 text-xs text-text font-mono focus:border-accent focus:outline-none"
                />
                <button
                  type="button"
                  onClick={() => setShowNewKey(!showNewKey)}
                  aria-label={showNewKey ? "hide API key" : "show API key"}
                  className="absolute right-1.5 top-1/2 -translate-y-1/2 text-text-muted hover:text-text"
                >
                  {showNewKey ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
                </button>
              </div>
              <button
                onClick={handleAdd}
                disabled={!newProvider || !newKey || saving}
                className="rounded border border-accent bg-accent/5 px-3 py-1.5 text-xs font-medium text-accent hover:bg-accent/10 disabled:opacity-30 transition-colors flex items-center gap-1"
              >
                {saving ? <Loader2 className="h-3 w-3 animate-spin" /> : null}
                save
              </button>
            </div>
            </div>
          )}
        </>
      )}
    </div>
  );
}
