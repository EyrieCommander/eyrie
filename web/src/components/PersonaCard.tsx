import {
  Compass,
  Briefcase,
  Megaphone,
  Inbox,
  Microscope,
  PenTool,
  Cpu,
  BarChart3,
  User,
  CheckCircle,
  Plus,
  type LucideIcon,
} from "lucide-react";
import type { Persona } from "../lib/types";

const iconMap: Record<string, LucideIcon> = {
  compass: Compass,
  briefcase: Briefcase,
  megaphone: Megaphone,
  inbox: Inbox,
  microscope: Microscope,
  "pen-tool": PenTool,
  cpu: Cpu,
  "bar-chart": BarChart3,
};

const categoryColors: Record<string, string> = {
  personal: "bg-green/10 text-green border-green/20",
  business: "bg-purple/10 text-purple border-purple/20",
  creative: "bg-pink-500/10 text-pink-500 border-pink-500/20",
  technical: "bg-cyan-500/10 text-cyan-500 border-cyan-500/20",
};

const modelLabels: Record<string, string> = {
  "claude-opus-4-6": "opus 4.6",
  "claude-sonnet-4-6": "sonnet 4.6",
  "claude-haiku-4-5": "haiku 4.5",
};

interface PersonaCardProps {
  persona: Persona;
  onInstall: (id: string) => void;
  onSelect?: (id: string) => void;
  installingId?: string;
}

export default function PersonaCard({
  persona,
  onInstall,
  onSelect,
  installingId,
}: PersonaCardProps) {
  const isInstalling = persona.id === installingId;
  const Icon = iconMap[persona.icon] || User;

  return (
    <div
      className={`border rounded-lg p-5 transition-all duration-200 ${
        persona.installed
          ? "border-green/30 bg-green/[0.02]"
          : "border-border hover:border-accent hover:shadow-md hover:-translate-y-0.5"
      }`}
    >
      {/* Header */}
      <div className="flex items-start justify-between mb-3">
        <div className="flex items-center gap-3">
          <div
            className={`w-10 h-10 rounded-lg flex items-center justify-center ${
              persona.installed ? "bg-green/10" : "bg-accent/10"
            }`}
          >
            <Icon
              className={`w-5 h-5 ${persona.installed ? "text-green" : "text-accent"}`}
            />
          </div>
          <div>
            <h3 className="text-sm font-semibold text-text">{persona.name}</h3>
            <span
              className={`text-[10px] px-1.5 py-0.5 rounded border ${categoryColors[persona.category] || "bg-gray-500/10 text-gray-500 border-gray-500/20"}`}
            >
              {persona.category}
            </span>
          </div>
        </div>
      </div>

      {/* Description */}
      <p className="text-xs text-text-secondary mb-3 leading-relaxed line-clamp-2">
        {persona.description}
      </p>

      {/* Traits */}
      <div className="flex flex-wrap gap-1.5 mb-3">
        {persona.traits.slice(0, 3).map((trait, idx) => (
          <span
            key={`${trait}-${idx}`}
            className="text-[10px] px-2 py-0.5 rounded-full bg-surface text-text-muted"
          >
            {trait}
          </span>
        ))}
      </div>

      {/* Model badge */}
      <div className="flex items-center gap-2 mb-4">
        <span className="text-[10px] px-2 py-0.5 rounded bg-accent/10 text-accent">
          {modelLabels[persona.preferred_model] || persona.preferred_model}
        </span>
        {persona.reasoning_level && (
          <span className="text-[10px] text-text-muted">
            {persona.reasoning_level} reasoning
          </span>
        )}
      </div>

      {/* Action */}
      {persona.installed ? (
        onSelect ? (
          <button
            onClick={() => onSelect(persona.id)}
            className="w-full px-3 py-2 rounded text-xs font-medium bg-green/10 text-green flex items-center justify-center gap-2 hover:bg-green/20 transition-colors"
          >
            <CheckCircle className="w-3.5 h-3.5" />
            installed
          </button>
        ) : (
          <div
            aria-disabled="true"
            className="w-full px-3 py-2 rounded text-xs font-medium bg-green/10 text-green flex items-center justify-center gap-2 cursor-default opacity-75"
          >
            <CheckCircle className="w-3.5 h-3.5" />
            installed
          </div>
        )
      ) : (
        <button
          onClick={() => onInstall(persona.id)}
          disabled={isInstalling}
          className="w-full px-3 py-2 rounded text-xs font-medium border border-accent text-accent hover:bg-accent hover:text-white transition-all flex items-center justify-center gap-2 disabled:opacity-50"
        >
          <Plus className="w-3.5 h-3.5" />
          {isInstalling ? "adding..." : "add persona"}
        </button>
      )}
    </div>
  );
}
