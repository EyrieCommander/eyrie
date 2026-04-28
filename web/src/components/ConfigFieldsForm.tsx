// Inline config form rendered from a framework's config_schema.common_fields.
//
// Each field is rendered as the appropriate input type (text, number, select,
// checkbox, multiselect). On save, all field values are sent as dot-path keys
// to PUT /api/registry/frameworks/{id}/config, which patches the framework's
// config file without replacing the rest of the content.

import { useCallback, useEffect, useRef, useState } from "react";
import { Check, Loader2 } from "lucide-react";
import type { ConfigField, Framework } from "../lib/types";
import { fetchFrameworkConfig, patchFrameworkConfig } from "../lib/api";

interface Props {
  framework: Framework;
  onSaved?: () => void;
}

export default function ConfigFieldsForm({ framework, onSaved }: Props) {
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
        if (cancelled || !cfg.parsed) return;
        if (!fields) return;
        const loaded: Record<string, unknown> = {};
        for (const field of fields) {
          const val = getNestedValue(cfg.parsed, field.key);
          if (val !== undefined) loaded[field.key] = val;
        }
        setValues((prev) => ({ ...prev, ...loaded }));
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
    setValues((prev) => {
      const next = { ...prev, [key]: val };
      // When a field that other fields depend on via suggestions_key changes,
      // reset dependents to the first suggestion for the new value.
      if (fields) {
        for (const f of fields) {
          if (f.suggestions_key === key && f.suggestions && !Array.isArray(f.suggestions)) {
            const newList = (f.suggestions as Record<string, string[]>)[String(val)];
            if (newList?.[0]) next[f.key] = newList[0];
          }
        }
      }
      return next;
    });
  }, [fields]);

  const handleSave = async () => {
    if (saving) return;
    setSaving(true);
    setError(null);
    try {
      await patchFrameworkConfig(framework.id, values);
      setSaved(true);
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
      <FieldList fields={essentialFields} values={values} setValue={setValue} />

      {advancedFields.length > 0 && (
        <>
          <button
            type="button"
            onClick={() => setShowAdvanced(!showAdvanced)}
            className="text-[10px] text-text-muted hover:text-text transition-colors"
          >
            {showAdvanced ? "hide" : "show"} advanced settings ({advancedFields.length})
          </button>
          {showAdvanced && <FieldList fields={advancedFields} values={values} setValue={setValue} />}
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

/** Renders a list of fields, grouping same-group fields side-by-side. */
function FieldList({
  fields,
  values,
  setValue,
}: {
  fields: ConfigField[];
  values: Record<string, unknown>;
  setValue: (key: string, val: unknown) => void;
}) {
  const rows: ConfigField[][] = [];
  let i = 0;
  while (i < fields.length) {
    const f = fields[i];
    // Collect consecutive fields with the same group
    if (f.group) {
      const group: ConfigField[] = [f];
      while (i + 1 < fields.length && fields[i + 1].group === f.group) {
        group.push(fields[++i]);
      }
      rows.push(group);
    } else {
      rows.push([f]);
    }
    i++;
  }

  return (
    <>
      {rows.map((row) =>
        row.length > 1 ? (
          <div key={row[0].key} className="grid gap-3" style={{ gridTemplateColumns: `repeat(${row.length}, 1fr)` }}>
            {row.map((field) => (
              <FieldInput
                key={field.key}
                field={field}
                value={values[field.key]}
                allValues={values}
                onChange={(val) => setValue(field.key, val)}
              />
            ))}
          </div>
        ) : (
          <FieldInput
            key={row[0].key}
            field={row[0]}
            value={values[row[0].key]}
            allValues={values}
            onChange={(val) => setValue(row[0].key, val)}
          />
        ),
      )}
    </>
  );
}

function FieldInput({
  field,
  value,
  allValues,
  onChange,
}: {
  field: ConfigField;
  value: unknown;
  allValues: Record<string, unknown>;
  onChange: (val: unknown) => void;
}) {
  const id = `cfg-${field.key}`;
  const strVal = String(value ?? field.default ?? "");

  const resolvedSuggestions = resolveSuggestions(field, allValues);

  const hasSuggestions = field.type === "text" && resolvedSuggestions && resolvedSuggestions.length > 0;
  const isCustom = hasSuggestions && !resolvedSuggestions!.includes(strVal) && strVal !== "";
  const [showCustom, setShowCustom] = useState(isCustom);

  // Reset to dropdown when the suggestions list changes (e.g., provider switched)
  const prevSuggestionsRef = useRef(resolvedSuggestions);
  useEffect(() => {
    if (prevSuggestionsRef.current !== resolvedSuggestions) {
      prevSuggestionsRef.current = resolvedSuggestions;
      setShowCustom(false);
    }
  }, [resolvedSuggestions]);

  return (
    <div className="space-y-1 min-w-0">
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
          value={strVal}
          onChange={(e) => onChange(e.target.value)}
          className="w-full rounded border border-border bg-bg px-2 py-1.5 text-xs text-text focus:border-accent focus:outline-none"
        >
          {field.options.map((opt) => (
            <option key={opt} value={opt}>{opt}</option>
          ))}
        </select>
      ) : hasSuggestions && !showCustom ? (
        <select
          id={id}
          value={resolvedSuggestions!.includes(strVal) ? strVal : "__custom__"}
          onChange={(e) => {
            if (e.target.value === "__custom__") {
              setShowCustom(true);
              onChange("");
            } else {
              onChange(e.target.value);
            }
          }}
          className="w-full rounded border border-border bg-bg px-2 py-1.5 text-xs text-text focus:border-accent focus:outline-none"
        >
          {resolvedSuggestions!.map((opt) => (
            <option key={opt} value={opt}>{opt}</option>
          ))}
          <option value="__custom__">custom...</option>
        </select>
      ) : hasSuggestions && showCustom ? (
        <div className="flex gap-1">
          <input
            id={id}
            type="text"
            value={strVal}
            onChange={(e) => onChange(e.target.value)}
            placeholder={field.description}
            className="flex-1 min-w-0 rounded border border-border bg-bg px-2 py-1.5 text-xs text-text font-mono focus:border-accent focus:outline-none"
          />
          <button
            type="button"
            onClick={() => { setShowCustom(false); onChange(resolvedSuggestions![0]); }}
            className="shrink-0 rounded border border-border px-2 py-1.5 text-[10px] text-text-muted hover:text-text transition-colors"
          >
            list
          </button>
        </div>
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
          value={strVal}
          onChange={(e) => onChange(e.target.value)}
          placeholder={field.description}
          className="w-full rounded border border-border bg-bg px-2 py-1.5 text-xs text-text font-mono focus:border-accent focus:outline-none"
        />
      )}

      {field.type !== "checkbox" && !field.group && (
        <p className="text-[10px] text-text-muted">{field.description}</p>
      )}
    </div>
  );
}

/** Resolve suggestions for a field: flat array or map keyed by another field's value. */
function resolveSuggestions(
  field: ConfigField,
  allValues: Record<string, unknown>,
): string[] | undefined {
  if (!field.suggestions) return undefined;
  if (Array.isArray(field.suggestions)) return field.suggestions;
  if (field.suggestions_key) {
    const keyVal = String(allValues[field.suggestions_key] ?? "");
    return (field.suggestions as Record<string, string[]>)[keyVal];
  }
  return undefined;
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
