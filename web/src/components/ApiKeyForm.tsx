// Reusable single-provider API key form.
//
// Extracted from SettingsPage's ApiKeysSection so the onboarding flow
// (FrameworksPhase sub-step 4) can embed the same input/save UX for a
// specific provider detected from the framework's config.
//
// SettingsPage still has its own multi-provider management section — this
// component intentionally scopes to ONE provider (the caller knows which
// one) and handles save + optional validation only. It does not render
// "known providers" selection or list of existing keys.

import { useEffect, useState } from "react";
import { Check, Eye, EyeOff, Loader2 } from "lucide-react";
import { fetchKeys, setKey } from "../lib/api";
import type { KeyEntry } from "../lib/types";

interface Props {
  /** Provider name (e.g. "openrouter", "anthropic"). Becomes the KeyVault key. */
  provider: string;
  /** Fires after a successful save. Callers use this to refetch keys / advance step. */
  onSaved?: () => void;
  /** Placeholder shown in the password input. Defaults to a generic "sk-...". */
  placeholder?: string;
  /** Hide the "saved: sk-***" existing-key indicator (when the caller shows it elsewhere). */
  hideSavedStatus?: boolean;
}

export default function ApiKeyForm({ provider, onSaved, placeholder, hideSavedStatus }: Props) {
  const [existing, setExisting] = useState<KeyEntry | null>(null);
  const [value, setValue] = useState("");
  const [show, setShow] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  // Load the masked existing key (if any) so the user sees their current state.
  useEffect(() => {
    let cancelled = false;
    fetchKeys()
      .then((keys) => {
        if (cancelled) return;
        const match = keys.find((k) => k.provider === provider);
        setExisting(match ?? null);
      })
      .catch(() => {
        /* silent — not having keys is a valid state */
      });
    return () => {
      cancelled = true;
    };
  }, [provider]);

  // Clear the "saved" flash after 3s so repeated saves still light up.
  useEffect(() => {
    if (!saved) return;
    const t = setTimeout(() => setSaved(false), 3000);
    return () => clearTimeout(t);
  }, [saved]);

  const handleSave = async () => {
    if (!value || saving) return;
    try {
      setSaving(true);
      setError(null);
      await setKey(provider, value);
      setValue("");
      setShow(false);
      setSaved(true);
      onSaved?.();
      // Refresh masked display separately — failure here should not
      // override the save-success state.
      try {
        const keys = await fetchKeys();
        setExisting(keys.find((k) => k.provider === provider) ?? null);
      } catch {
        /* best effort — key is saved, masked display just stale */
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to save key");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-2">
      {existing?.has_key && !hideSavedStatus && (
        <div className="flex items-center gap-2 text-[10px] text-text-muted">
          <Check className="h-3 w-3 text-green" />
          <span>saved:</span>
          <span className="font-mono text-text-muted">{existing.masked_key}</span>
          <span className="text-text-muted">(replace below to update)</span>
        </div>
      )}

      <div className="flex items-center gap-2">
        <div className="relative flex-1">
          <input
            type={show ? "text" : "password"}
            value={value}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") handleSave();
            }}
            placeholder={placeholder ?? "sk-..."}
            aria-label={`${provider} API key`}
            autoComplete="one-time-code"
            data-1p-ignore
            data-lpignore="true"
            className="w-full rounded border border-border bg-bg px-2 py-1.5 pr-7 text-xs text-text font-mono focus:border-accent focus:outline-none"
          />
          <button
            type="button"
            onClick={() => setShow(!show)}
            className="absolute right-1.5 top-1/2 -translate-y-1/2 text-text-muted hover:text-text"
            aria-label={show ? "hide key" : "show key"}
          >
            {show ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
          </button>
        </div>
        <button
          onClick={handleSave}
          disabled={!value || saving}
          className="flex items-center gap-1 rounded border border-accent bg-accent/5 px-3 py-1.5 text-xs font-medium text-accent transition-colors hover:bg-accent/10 disabled:opacity-30"
        >
          {saving ? <Loader2 className="h-3 w-3 animate-spin" /> : null}
          {existing?.has_key ? "update" : "save"}
        </button>
      </div>

      {error && (
        <div className="rounded border border-red/20 bg-red/5 px-2 py-1 text-[10px] text-red">
          {error}
        </div>
      )}
      {saved && (
        <div className="flex items-center gap-1 text-[10px] text-accent">
          <Check className="h-2.5 w-2.5" /> saved
        </div>
      )}
    </div>
  );
}
