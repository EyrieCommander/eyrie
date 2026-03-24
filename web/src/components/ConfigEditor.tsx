import { useState } from "react";
import { AlertCircle } from "lucide-react";

interface ConfigEditorProps {
  value: string;
  format: string;
  onChange: (value: string) => void;
}

export default function ConfigEditor({
  value,
  format,
  onChange,
}: ConfigEditorProps) {
  const [parseError, setParseError] = useState<string | null>(null);

  const handleChange = (newValue: string) => {
    onChange(newValue);

    // Try to parse to validate syntax
    setParseError(null);
    if (!newValue.trim()) return;

    try {
      if (format === "json") {
        JSON.parse(newValue);
      } else if (format === "yaml" || format === "yml") {
        // Basic YAML validation - just check for common syntax errors
        if (newValue.includes("\t")) {
          setParseError("YAML does not allow tabs - use spaces for indentation");
        }
      }
      // TOML validation is complex, skip for now
    } catch (err) {
      setParseError(err instanceof Error ? err.message : "Parse error");
    }
  };

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <label className="text-sm font-medium text-fg">
          raw configuration ({format})
        </label>
        {parseError && (
          <div className="flex items-center gap-2 text-xs text-red">
            <AlertCircle className="w-4 h-4" />
            <span>{parseError}</span>
          </div>
        )}
      </div>

      <textarea
        value={value}
        onChange={(e) => handleChange(e.target.value)}
        className={`w-full h-[500px] px-4 py-3 bg-bg-subtle border rounded-lg
          font-mono text-sm text-fg resize-none focus:outline-none focus:ring-2
          ${parseError ? "border-red focus:ring-red/50" : "border-border focus:ring-accent/50"}`}
        spellCheck={false}
      />

      <p className="text-xs text-fg-muted">
        note: comments and formatting may be lost when saving
      </p>
    </div>
  );
}
