import { useZoom } from "../lib/useZoom";
import { useFont, FONT_OPTIONS, type FontId } from "../lib/useFont";
import { useTheme, type Theme } from "../lib/useTheme";
import { useLatencyThresholds } from "../lib/useLatencyThresholds";
import { useState, useEffect, useRef } from "react";
import { Minus, Plus, RotateCcw, Moon, Sun, Check, Key, Trash2, Loader2, Eye, EyeOff, ShieldCheck, ShieldAlert } from "lucide-react";
import { fetchKeys, setKey, deleteKey } from "../lib/api";
import type { KeyEntry } from "../lib/types";

export default function SettingsPage() {
  const { zoom, setZoom, reset: resetZoom, min, max, step } = useZoom();
  const { font, setFont, reset: resetFont } = useFont();
  const { theme, setTheme } = useTheme();
  const { thresholds, setThresholds, reset: resetThresholds, defaults } = useLatencyThresholds();
  const [thresholdSaved, setThresholdSaved] = useState(false);
  const savedTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const [warnInput, setWarnInput] = useState(String(Math.round(thresholds.warn / 1000)));
  const [errorInput, setErrorInput] = useState(String(Math.round(thresholds.error / 1000)));

  // Clean up pending timer on unmount
  useEffect(() => {
    return () => { if (savedTimerRef.current) clearTimeout(savedTimerRef.current); };
  }, []);

  // Sync inputs when thresholds change externally (e.g., reset)
  useEffect(() => {
    setWarnInput(String(Math.round(thresholds.warn / 1000)));
    setErrorInput(String(Math.round(thresholds.error / 1000)));
  }, [thresholds.warn, thresholds.error]);

  const commitThresholds = (warn: string, error: string) => {
    const w = Math.max(1, Number(warn) || 1);
    const e = Math.max(w + 1, Number(error) || w + 1);
    setThresholds({ warn: w * 1000, error: e * 1000 });
    setThresholdSaved(true);
    if (savedTimerRef.current) clearTimeout(savedTimerRef.current);
    savedTimerRef.current = setTimeout(() => setThresholdSaved(false), 1000);
  };

  return (
    <div className="space-y-6">
      <div className="text-xs text-text-muted">~/settings</div>

      <div>
        <h1 className="text-xl font-bold">
          <span className="text-accent">&gt;</span> settings
        </h1>
        <p className="mt-1 text-xs text-text-muted">
          // dashboard appearance
        </p>
      </div>

      {/* API Keys */}
      <ApiKeysSection />

      {/* Theme + Zoom + Latency */}
      <div className="grid gap-4 md:grid-cols-3">
        {/* Theme */}
        <div className="rounded border border-border bg-surface p-4 space-y-3">
          <div>
            <h3 className="text-xs font-medium text-text">theme</h3>
            <p className="text-[10px] text-text-muted mt-0.5">
              dark or light mode
            </p>
          </div>
          <div className="flex gap-2">
            {([
              { id: "dark" as Theme, label: "dark", Icon: Moon },
              { id: "light" as Theme, label: "light", Icon: Sun },
            ]).map(({ id, label, Icon }) => (
              <button
                key={id}
                onClick={() => setTheme(id)}
                className={`flex items-center gap-2 rounded border px-4 py-2.5 text-xs font-medium transition-colors ${
                  theme === id
                    ? "border-accent bg-accent/5 text-accent"
                    : "border-border hover:border-text-muted/50 text-text-secondary hover:text-text"
                }`}
              >
                <Icon className="h-3.5 w-3.5" />
                {label}
              </button>
            ))}
          </div>
        </div>

        {/* Zoom */}
        <div className="rounded border border-border bg-surface p-4 space-y-3 overflow-hidden">
          <div className="flex items-center justify-between">
            <div>
              <h3 className="text-xs font-medium text-text">zoom level</h3>
              <p className="text-[10px] text-text-muted mt-0.5">
                scales the entire UI
              </p>
            </div>
            {zoom !== 100 && (
              <button
                onClick={resetZoom}
                className="flex items-center gap-1 text-[10px] text-text-muted hover:text-accent transition-colors"
              >
                <RotateCcw className="h-2.5 w-2.5" /> reset
              </button>
            )}
          </div>
          <div className="flex items-center gap-3">
            <button
              onClick={() => setZoom(zoom - step)}
              disabled={zoom <= min}
              className="p-1 rounded border border-border text-text-muted hover:text-text hover:border-text-muted/50 disabled:opacity-30 transition-colors"
            >
              <Minus className="h-3 w-3" />
            </button>
            <input
              type="range"
              min={min}
              max={max}
              step={step}
              value={zoom}
              onChange={(e) => setZoom(Number(e.target.value))}
              className="zoom-slider flex-1 h-1 appearance-none bg-border rounded-full cursor-pointer"
            />
            <button
              onClick={() => setZoom(zoom + step)}
              disabled={zoom >= max}
              className="p-1 rounded border border-border text-text-muted hover:text-text hover:border-text-muted/50 disabled:opacity-30 transition-colors"
            >
              <Plus className="h-3 w-3" />
            </button>
            <span className="text-xs tabular-nums text-text-secondary w-10 text-right">{zoom}%</span>
          </div>
        </div>
        {/* Latency thresholds */}
        <div className="rounded border border-border bg-surface p-4 space-y-3">
          <div className="flex items-center justify-between">
            <div>
              <h3 className="text-xs font-medium text-text">latency thresholds</h3>
              <p className="text-[10px] text-text-muted mt-0.5">
                compare page colors (seconds)
              </p>
            </div>
            {(thresholds.warn !== defaults.warn || thresholds.error !== defaults.error) && (
              <button
                onClick={resetThresholds}
                className="flex items-center gap-1 text-[10px] text-text-muted hover:text-accent transition-colors"
              >
                <RotateCcw className="h-2.5 w-2.5" /> reset
              </button>
            )}
          </div>
          <div className="flex items-center gap-1.5 text-xs flex-wrap">
            <span className="text-accent font-medium">green</span>
            <span className="text-text-muted">&lt;</span>
            <input
              type="text"
              inputMode="numeric"
              value={warnInput}
              onChange={(e) => setWarnInput(e.target.value)}
              onBlur={() => commitThresholds(warnInput, errorInput)}
              onKeyDown={(e) => { if (e.key === "Enter") (e.target as HTMLInputElement).blur(); }}
              className="w-8 rounded border border-border bg-bg px-1 py-0.5 text-center text-xs text-text tabular-nums focus:border-accent focus:outline-none"
            />
            <span className="text-text-muted">&lt;</span>
            <span className="text-yellow font-medium">yellow</span>
            <span className="text-text-muted">&lt;</span>
            <input
              type="text"
              inputMode="numeric"
              value={errorInput}
              onChange={(e) => setErrorInput(e.target.value)}
              onBlur={() => commitThresholds(warnInput, errorInput)}
              onKeyDown={(e) => { if (e.key === "Enter") (e.target as HTMLInputElement).blur(); }}
              className="w-8 rounded border border-border bg-bg px-1 py-0.5 text-center text-xs text-text tabular-nums focus:border-accent focus:outline-none"
            />
            <span className="text-text-muted">&lt;</span>
            <span className="text-red font-medium">red</span>
          </div>
          <div className={`flex items-center gap-1 text-[10px] text-accent transition-opacity ${thresholdSaved ? "opacity-100" : "opacity-0"}`}>
            <Check className="h-2.5 w-2.5" /> saved
          </div>
        </div>
      </div>

      {/* Font */}
      <div className="rounded border border-border bg-surface p-4 space-y-3">
        <div className="flex items-center justify-between">
          <div>
            <h3 className="text-xs font-medium text-text">font family</h3>
            <p className="text-[10px] text-text-muted mt-0.5">
              applied across the entire dashboard
            </p>
          </div>
          {font !== "jetbrains-mono" && (
            <button
              onClick={resetFont}
              className="flex items-center gap-1 text-[10px] text-text-muted hover:text-accent transition-colors"
            >
              <RotateCcw className="h-2.5 w-2.5" /> reset
            </button>
          )}
        </div>

        <div className="grid grid-cols-2 gap-2">
          {FONT_OPTIONS.map((option) => (
            <button
              key={option.id}
              onClick={() => setFont(option.id as FontId)}
              className={`rounded border px-3 py-2.5 text-left transition-colors ${
                font === option.id
                  ? "border-accent bg-accent/5 text-accent"
                  : "border-border hover:border-text-muted/50 text-text-secondary hover:text-text"
              }`}
            >
              <div className="text-xs font-medium" style={{ fontFamily: option.family }}>
                {option.label}
              </div>
              <div className="text-[10px] mt-1 text-text-muted" style={{ fontFamily: option.family }}>
                The quick brown fox jumps over the lazy dog
              </div>
            </button>
          ))}
        </div>
      </div>

    </div>
  );
}

// --- API Keys Section ---

const KNOWN_PROVIDERS = ["anthropic", "openrouter", "openai", "deepseek"];

function ApiKeysSection() {
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
      // Silently handle — keys may not exist yet
      setKeys([]);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { loadKeys(); }, []);

  // Clear success message after 3 seconds
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
      setSuccessMsg(`${newProvider} key saved`);
      await loadKeys();
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to save key");
    } finally {
      setSaving(false);
    }
  };

  const [deletingProvider, setDeletingProvider] = useState<string | null>(null);
  const handleDelete = async (provider: string) => {
    if (deletingProvider) return;
    try {
      setDeletingProvider(provider);
      setError(null);
      await deleteKey(provider);
      setSuccessMsg(`${provider} key removed`);
      await loadKeys();
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to delete key");
    } finally {
      setDeletingProvider(null);
    }
  };

  // Providers that don't already have a stored key
  const availableProviders = KNOWN_PROVIDERS.filter(
    (p) => !keys.some((k) => k.provider === p),
  );

  return (
    <div className="rounded border border-border bg-surface p-4 space-y-3">
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
          {/* Existing keys */}
          {keys.length > 0 && (
            <div className="space-y-1.5">
              {keys.map((entry) => (
                <div
                  key={entry.provider}
                  className="flex items-center justify-between rounded border border-border px-3 py-2 text-xs"
                >
                  <div className="flex items-center gap-2">
                    <ShieldCheck className="h-3.5 w-3.5 text-accent" />
                    <span className="font-medium text-text">{entry.provider}</span>
                    <span className="text-text-muted font-mono text-[10px]">{entry.masked_key}</span>
                  </div>
                  <button
                    onClick={() => handleDelete(entry.provider)}
                    disabled={deletingProvider === entry.provider}
                    className="p-1 rounded text-text-muted hover:text-red hover:bg-red/5 transition-colors disabled:opacity-30"
                    title="remove key"
                  >
                    <Trash2 className="h-3 w-3" />
                  </button>
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

          {/* Add new key */}
          {availableProviders.length > 0 && (
            <div className="flex items-center gap-2 pt-1">
              <select
                value={newProvider}
                onChange={(e) => setNewProvider(e.target.value)}
                aria-label="API provider"
                className="rounded border border-border bg-bg px-2 py-1.5 text-xs text-text focus:border-accent focus:outline-none"
              >
                <option value="">provider...</option>
                {availableProviders.map((p) => (
                  <option key={p} value={p}>{p}</option>
                ))}
              </select>
              <div className="relative flex-1">
                <input
                  type={showNewKey ? "text" : "password"}
                  value={newKey}
                  onChange={(e) => setNewKey(e.target.value)}
                  onKeyDown={(e) => { if (e.key === "Enter") handleAdd(); }}
                  placeholder="sk-..."
                  aria-label={`API key${newProvider ? ` for ${newProvider}` : ""}`}
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
          )}
        </>
      )}
    </div>
  );
}
