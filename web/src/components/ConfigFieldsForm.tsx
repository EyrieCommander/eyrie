// Inline config form rendered from a framework's config_schema.common_fields.
//
// Each field is rendered as the appropriate input type (text, number, select,
// checkbox, multiselect). On save, all field values are sent as dot-path keys
// to PUT /api/registry/frameworks/{id}/config, which patches the framework's
// config file without replacing the rest of the content.

import { useCallback, useEffect, useState } from "react";
import { Check, Loader2 } from "lucide-react";
import type { ConfigField, Framework } from "../lib/types";
import { fetchFrameworkConfig, patchFrameworkConfig } from "../lib/api";
import { shellQuote } from "../lib/shell";

interface Props {
  framework: Framework;
  onSaved?: () => void;
  /** Echo a message into the tmux terminal (for visual confirmation). */
  onEcho?: (msg: string) => void;
}

export default function ConfigFieldsForm({ framework, onSaved, onEcho }: Props) {
  const fields = framework.config_schema?.common_fields;
  const [values, setValues] = useState<Record<string, unknown>>({});
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  // Load current values from the config file (if it exists) so the form
  // shows what's already configured rather than just defaults.
  useEffect(() => {
    let cancelled = false;
    fetchFrameworkConfig(framework.id)
      .then((cfg) => {
        if (cancelled || !cfg.content) return;
        try {
          const parsed = typeof cfg.content === "string" ? JSON.parse(cfg.content) : cfg.content;
          if (!fields) return;
          const loaded: Record<string, unknown> = {};
          for (const field of fields) {
            const val = getNestedValue(parsed, field.key);
            if (val !== undefined) loaded[field.key] = val;
          }
          setValues((prev) => ({ ...loaded, ...prev }));
        } catch { /* config not parseable — use defaults */ }
      })
      .catch(() => { /* no config yet — use defaults */ });
    return () => { cancelled = true; };
  }, [framework.id, fields]);

  // Initialize defaults for fields that have no value yet
  useEffect(() => {
    if (!fields) return;
    setValues((prev) => {
      const next = { ...prev };
      let changed = false;
      for (const f of fields) {
        if (next[f.key] === undefined && f.default !== undefined) {
          next[f.key] = f.default;
          changed = true;
        }
      }
      return changed ? next : prev;
    });
  }, [fields]);

  // Clear saved flash
  useEffect(() => {
    if (!saved) return;
    const t = setTimeout(() => setSaved(false), 3000);
    return () => clearTimeout(t);
  }, [saved]);

  const setValue = useCallback((key: string, val: unknown) => {
    setValues((prev) => ({ ...prev, [key]: val }));
  }, []);

  const handleSave = async () => {
    if (saving) return;
    setSaving(true);
    setError(null);
    try {
      await patchFrameworkConfig(framework.id, values);
      setSaved(true);
      onEcho?.(`echo "✓ Config saved to " ${shellQuote(framework.config_path)}`);
      onSaved?.();
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to save");
    } finally {
      setSaving(false);
    }
  };

  const [showAdvanced, setShowAdvanced] = useState(false);

  if (!fields || fields.length === 0) {
    return (
      <p className="text-xs text-text-muted">
        No configurable fields defined for {framework.name}.
      </p>
    );
  }

  const essentialFields = fields.filter((f) => !f.advanced);
  const advancedFields = fields.filter((f) => f.advanced);

  return (
    <div className="space-y-3">
      {essentialFields.map((field) => (
        <FieldInput
          key={field.key}
          field={field}
          value={values[field.key]}
          onChange={(val) => setValue(field.key, val)}
        />
      ))}

      {advancedFields.length > 0 && (
        <>
          <button
            type="button"
            onClick={() => setShowAdvanced(!showAdvanced)}
            className="text-[10px] text-text-muted hover:text-text transition-colors"
          >
            {showAdvanced ? "hide" : "show"} advanced settings ({advancedFields.length})
          </button>
          {showAdvanced && advancedFields.map((field) => (
            <FieldInput
              key={field.key}
              field={field}
              value={values[field.key]}
              onChange={(val) => setValue(field.key, val)}
            />
          ))}
        </>
      )}

      <div className="flex items-center gap-2 pt-1">
        <button
          onClick={handleSave}
          disabled={saving}
          className="flex items-center gap-1.5 rounded bg-accent px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-accent-hover disabled:opacity-40"
        >
          {saving ? <Loader2 className="h-3 w-3 animate-spin" /> : null}
          save config
        </button>
        {saved && (
          <span className="flex items-center gap-1 text-[10px] text-green">
            <Check className="h-2.5 w-2.5" /> saved
          </span>
        )}
        {error && (
          <span className="text-[10px] text-red">{error}</span>
        )}
      </div>
    </div>
  );
}

function FieldInput({
  field,
  value,
  onChange,
}: {
  field: ConfigField;
  value: unknown;
  onChange: (val: unknown) => void;
}) {
  const id = `cfg-${field.key}`;

  return (
    <div className="space-y-1">
      {field.type === "checkbox" ? (
        <div className="block text-[10px] font-medium text-text-muted uppercase tracking-wider">
          {field.label}
          {field.required && <span className="text-red ml-0.5">*</span>}
        </div>
      ) : (
        <label htmlFor={id} className="block text-[10px] font-medium text-text-muted uppercase tracking-wider">
          {field.label}
          {field.required && <span className="text-red ml-0.5">*</span>}
        </label>
      )}

      {field.type === "select" && field.options ? (
        <select
          id={id}
          value={String(value ?? field.default ?? "")}
          onChange={(e) => onChange(e.target.value)}
          className="w-full rounded border border-border bg-bg px-2 py-1.5 text-xs text-text focus:border-accent focus:outline-none"
        >
          {field.options.map((opt) => (
            <option key={opt} value={opt}>{opt}</option>
          ))}
        </select>
      ) : field.type === "checkbox" ? (
        <div className="flex items-center gap-2 text-xs text-text">
          <input
            id={id}
            type="checkbox"
            checked={!!value}
            onChange={(e) => onChange(e.target.checked)}
            className="rounded border-border"
          />
          <label htmlFor={id}>{field.description}</label>
        </div>
      ) : field.type === "number" ? (
        <input
          id={id}
          type="number"
          value={value !== undefined ? Number(value) : (field.default as number) ?? ""}
          min={field.min}
          max={field.max}
          onChange={(e) => onChange(e.target.value ? Number(e.target.value) : undefined)}
          className="w-full rounded border border-border bg-bg px-2 py-1.5 text-xs text-text font-mono focus:border-accent focus:outline-none"
        />
      ) : field.type === "multiselect" && field.options ? (
        <div className="flex flex-wrap gap-1.5">
          {field.options.map((opt) => {
            const selected = Array.isArray(value) && value.includes(opt);
            return (
              <button
                key={opt}
                type="button"
                onClick={() => {
                  const arr = Array.isArray(value) ? [...value] : [];
                  if (selected) onChange(arr.filter((v) => v !== opt));
                  else onChange([...arr, opt]);
                }}
                className={`rounded border px-2 py-0.5 text-[10px] transition-colors ${
                  selected
                    ? "border-accent bg-accent/10 text-accent"
                    : "border-border text-text-muted hover:text-text"
                }`}
              >
                {opt}
              </button>
            );
          })}
        </div>
      ) : (
        <input
          id={id}
          type="text"
          value={String(value ?? field.default ?? "")}
          onChange={(e) => onChange(e.target.value)}
          placeholder={field.description}
          className="w-full rounded border border-border bg-bg px-2 py-1.5 text-xs text-text font-mono focus:border-accent focus:outline-none"
        />
      )}

      {field.type !== "checkbox" && (
        <p className="text-[10px] text-text-muted">{field.description}</p>
      )}
    </div>
  );
}

/** Read a dot-separated path from a nested object. */
function getNestedValue(obj: Record<string, unknown>, path: string): unknown {
  const parts = path.split(".");
  let current: unknown = obj;
  for (const part of parts) {
    if (current == null || typeof current !== "object") return undefined;
    current = (current as Record<string, unknown>)[part];
  }
  return current;
}
