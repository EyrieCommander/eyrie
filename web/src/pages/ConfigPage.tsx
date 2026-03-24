import { useState, useEffect } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { ArrowLeft, Save, FileText, Code, Loader2, CheckCircle } from "lucide-react";
import { fetchAgents } from "../lib/api";
import {
  fetchAgentConfig,
  getFrameworkDetail,
  updateAgentConfig,
  validateAgentConfig,
} from "../lib/api";
import type { Framework, AgentInfo } from "../lib/types";
import APIKeyBanner from "../components/APIKeyBanner";
import ConfigForm from "../components/ConfigForm";
import ConfigEditor from "../components/ConfigEditor";

export default function ConfigPage() {
  const { agentName } = useParams<{ agentName: string }>();
  const navigate = useNavigate();

  const [, setAgent] = useState<AgentInfo | null>(null);
  const [framework, setFramework] = useState<Framework | null>(null);
  const [rawConfig, setRawConfig] = useState("");
  const [formData, setFormData] = useState<Record<string, unknown>>({});
  const [mode, setMode] = useState<"form" | "raw">("form");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [saveSuccess, setSaveSuccess] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Load agent and config on mount
  useEffect(() => {
    if (!agentName) return;

    const loadConfig = async () => {
      try {
        setLoading(true);
        setError(null);

        // Fetch agent info to get framework
        const agents = await fetchAgents();
        const agentInfo = agents.find((a) => a.name === agentName);
        if (!agentInfo) {
          setError(`Agent "${agentName}" not found`);
          return;
        }
        setAgent(agentInfo);

        // Fetch framework detail with schema
        const fw = await getFrameworkDetail(agentInfo.framework);
        setFramework(fw);

        // Fetch current config
        const cfg = await fetchAgentConfig(agentName);
        setRawConfig(cfg.content);

        // Parse config into form data
        try {
          const parsed = parseConfig(cfg.content, fw.config_format);
          setFormData(parsed);
        } catch (err) {
          console.error("Failed to parse config:", err);
          // Fall back to raw mode if parsing fails
          setMode("raw");
        }
      } catch (err) {
        setError(err instanceof Error ? err.message : "Failed to load configuration");
      } finally {
        setLoading(false);
      }
    };

    loadConfig();
  }, [agentName]);

  // Parse config string to object
  const parseConfig = (content: string, format: string): Record<string, unknown> => {
    if (format === "json") {
      return JSON.parse(content);
    }
    // For TOML and YAML, we'd need proper parsers
    // For now, just return empty object and stay in raw mode
    return {};
  };

  // Handle field change in form mode
  const handleFieldChange = (key: string, value: unknown) => {
    // Update nested fields (e.g., "gateway.port")
    const parts = key.split(".");
    const updated = { ...formData };
    let current: any = updated;

    for (let i = 0; i < parts.length - 1; i++) {
      const part = parts[i];
      if (!current[part] || typeof current[part] !== "object") {
        current[part] = {};
      }
      current = current[part];
    }

    current[parts[parts.length - 1]] = value;
    setFormData(updated);

    // Clear error for this field
    setErrors((prev) => {
      const next = { ...prev };
      delete next[key];
      return next;
    });

    // Mark as unsaved
    setSaveSuccess(false);
  };

  // Validate form data
  const validateForm = (): boolean => {
    if (!framework?.config_schema) return true;

    const newErrors: Record<string, string> = {};

    for (const field of framework.config_schema.common_fields) {
      if (!field.required) continue;

      const parts = field.key.split(".");
      let value: any = formData;
      for (const part of parts) {
        value = value?.[part];
      }

      if (value === undefined || value === null || value === "") {
        newErrors[field.key] = `${field.label} is required`;
      }

      // Type validation
      if (field.type === "number" && typeof value === "number") {
        if (field.min !== undefined && value < field.min) {
          newErrors[field.key] = `${field.label} must be at least ${field.min}`;
        }
        if (field.max !== undefined && value > field.max) {
          newErrors[field.key] = `${field.label} must be at most ${field.max}`;
        }
      }
    }

    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  };

  // Save configuration
  const handleSave = async () => {
    // Validate first
    if (mode === "form" && !validateForm()) {
      return;
    }

    try {
      setSaving(true);
      setError(null);

      const configToSave = mode === "form" ? formData : rawConfig;

      // Validate with backend
      const validation = await validateAgentConfig(agentName!, configToSave);
      if (!validation.valid) {
        setError(validation.error || "Configuration is invalid");
        return;
      }

      // Save config
      await updateAgentConfig(agentName!, configToSave);

      setSaveSuccess(true);
      setTimeout(() => setSaveSuccess(false), 3000);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to save configuration");
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader2 className="w-8 h-8 animate-spin text-accent" />
      </div>
    );
  }

  if (error && !framework) {
    return (
      <div className="p-8">
        <div className="p-4 bg-red/10 border border-red/20 rounded-lg text-red">
          {error}
        </div>
      </div>
    );
  }

  return (
    <div className="p-8 max-w-4xl mx-auto">
      {/* Header */}
      <div className="mb-8">
        <button
          onClick={() => navigate(-1)}
          className="flex items-center gap-2 text-sm text-fg-muted hover:text-fg mb-4"
        >
          <ArrowLeft className="w-4 h-4" />
          back
        </button>

        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-bold text-fg">
              configure {framework?.name || agentName}
            </h1>
            <p className="text-sm text-fg-muted mt-1">
              {framework?.description}
            </p>
          </div>

          {/* Mode Toggle */}
          <div className="flex items-center gap-2 bg-bg-subtle rounded-lg p-1">
            <button
              onClick={() => setMode("form")}
              className={`flex items-center gap-2 px-3 py-1.5 rounded text-sm transition-colors ${
                mode === "form"
                  ? "bg-bg text-fg"
                  : "text-fg-muted hover:text-fg"
              }`}
            >
              <FileText className="w-4 h-4" />
              form
            </button>
            <button
              onClick={() => setMode("raw")}
              className={`flex items-center gap-2 px-3 py-1.5 rounded text-sm transition-colors ${
                mode === "raw"
                  ? "bg-bg text-fg"
                  : "text-fg-muted hover:text-fg"
              }`}
            >
              <Code className="w-4 h-4" />
              raw editor
            </button>
          </div>
        </div>
      </div>

      {/* API Key Banner */}
      {framework?.config_schema && (
        <APIKeyBanner hint={framework.config_schema.api_key_hint} />
      )}

      {/* Error Banner */}
      {error && (
        <div className="mb-6 p-4 bg-red/10 border border-red/20 rounded-lg text-red text-sm">
          {error}
        </div>
      )}

      {/* Success Banner */}
      {saveSuccess && (
        <div className="mb-6 p-4 bg-green/10 border border-green/20 rounded-lg text-green text-sm flex items-center gap-2">
          <CheckCircle className="w-4 h-4" />
          configuration saved successfully
        </div>
      )}

      {/* Form or Editor */}
      {mode === "form" && framework?.config_schema ? (
        <ConfigForm
          fields={framework.config_schema.common_fields}
          data={formData}
          onChange={handleFieldChange}
          errors={errors}
        />
      ) : (
        <ConfigEditor
          value={rawConfig}
          format={framework?.config_format || "text"}
          onChange={setRawConfig}
        />
      )}

      {/* Save Button */}
      <div className="mt-8 flex items-center gap-4">
        <button
          onClick={handleSave}
          disabled={saving}
          className="flex items-center gap-2 px-6 py-2.5 bg-accent hover:bg-accent-hover
            text-white rounded-lg font-medium transition-colors disabled:opacity-50
            disabled:cursor-not-allowed"
        >
          {saving ? (
            <>
              <Loader2 className="w-4 h-4 animate-spin" />
              saving...
            </>
          ) : (
            <>
              <Save className="w-4 h-4" />
              save configuration
            </>
          )}
        </button>

        <button
          onClick={() => navigate(`/agents/${agentName}`)}
          className="px-6 py-2.5 bg-bg-subtle hover:bg-bg-muted border border-border
            text-fg rounded-lg font-medium transition-colors"
        >
          cancel
        </button>
      </div>
    </div>
  );
}
