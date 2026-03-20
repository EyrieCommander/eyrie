import type { ConfigField } from "../lib/types";

interface ConfigFormProps {
  fields: ConfigField[];
  data: Record<string, unknown>;
  onChange: (key: string, value: unknown) => void;
  errors: Record<string, string>;
}

export default function ConfigForm({
  fields,
  data,
  onChange,
  errors,
}: ConfigFormProps) {
  // Get value from nested path (e.g., "gateway.port" -> data.gateway.port)
  const getValue = (key: string): unknown => {
    const parts = key.split(".");
    let value: any = data;
    for (const part of parts) {
      if (value && typeof value === "object") {
        value = value[part];
      } else {
        return undefined;
      }
    }
    return value;
  };

  const renderField = (field: ConfigField) => {
    const value = getValue(field.key) ?? field.default;
    const error = errors[field.key];

    const commonClasses = `w-full px-3 py-2 bg-bg-subtle border rounded-lg text-sm text-fg
      focus:outline-none focus:ring-2 ${error ? "border-red focus:ring-red/50" : "border-border focus:ring-accent/50"}`;

    switch (field.type) {
      case "text":
        return (
          <input
            type="text"
            value={(value as string) ?? ""}
            onChange={(e) => onChange(field.key, e.target.value)}
            className={commonClasses}
            required={field.required}
          />
        );

      case "number":
        return (
          <input
            type="number"
            value={(value as number) ?? ""}
            onChange={(e) => onChange(field.key, parseInt(e.target.value, 10))}
            min={field.min}
            max={field.max}
            className={commonClasses}
            required={field.required}
          />
        );

      case "select":
        return (
          <select
            value={(value as string) ?? ""}
            onChange={(e) => onChange(field.key, e.target.value)}
            className={commonClasses}
            required={field.required}
          >
            {!value && !field.required && (
              <option value="">select an option...</option>
            )}
            {field.options?.map((opt) => (
              <option key={opt} value={opt}>
                {opt}
              </option>
            ))}
          </select>
        );

      case "checkbox":
        return (
          <label className="flex items-center gap-2 cursor-pointer">
            <input
              type="checkbox"
              checked={!!value}
              onChange={(e) => onChange(field.key, e.target.checked)}
              className="w-4 h-4 rounded border-border bg-bg-subtle text-accent
                focus:ring-2 focus:ring-accent/50"
            />
            <span className="text-sm text-fg">{field.description}</span>
          </label>
        );

      case "multiselect":
        return (
          <div className="space-y-2">
            {field.options?.map((opt) => {
              const selected = Array.isArray(value) && value.includes(opt);
              return (
                <label
                  key={opt}
                  className="flex items-center gap-2 cursor-pointer"
                >
                  <input
                    type="checkbox"
                    checked={selected}
                    onChange={(e) => {
                      const current = (value as string[]) || [];
                      const updated = e.target.checked
                        ? [...current, opt]
                        : current.filter((v) => v !== opt);
                      onChange(field.key, updated);
                    }}
                    className="w-4 h-4 rounded border-border bg-bg-subtle text-accent
                      focus:ring-2 focus:ring-accent/50"
                  />
                  <span className="text-sm text-fg">{opt}</span>
                </label>
              );
            })}
          </div>
        );

      default:
        return (
          <div className="text-sm text-fg-muted">
            unsupported field type: {field.type}
          </div>
        );
    }
  };

  return (
    <div className="space-y-6">
      {fields.map((field) => (
        <div key={field.key} className="space-y-2">
          <label className="flex items-center gap-2 text-sm font-medium text-fg">
            {field.label}
            {field.required && <span className="text-red">*</span>}
          </label>

          {field.description && field.type !== "checkbox" && (
            <p className="text-xs text-fg-muted">{field.description}</p>
          )}

          {renderField(field)}

          {errors[field.key] && (
            <p className="text-xs text-red">{errors[field.key]}</p>
          )}
        </div>
      ))}
    </div>
  );
}
