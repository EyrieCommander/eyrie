import { useNavigate } from "react-router-dom";
import { Download, CheckCircle, Settings } from "lucide-react";
import { FRAMEWORK_EMOJI } from "../lib/types";
import type { Framework } from "../lib/types";
import { getFrameworkStatus } from "../lib/frameworkStatus";

interface FrameworkCardProps {
  framework: Framework;
  /** When provided, clicking the card calls this instead of navigating to the detail page. */
  onSelect?: (id: string) => void;
}

export default function FrameworkCard({
  framework,
  onSelect,
}: FrameworkCardProps) {
  const navigate = useNavigate();
  const status = getFrameworkStatus(framework);
  const emoji = FRAMEWORK_EMOJI[framework.id] || "";
  const handleClick = onSelect
    ? () => onSelect(framework.id)
    : () => navigate(`/frameworks/${framework.id}`);

  const badgeColors: Record<string, string> = {
    green: "bg-green/10 text-green",
    yellow: "bg-yellow/10 text-yellow",
    red: "bg-red/10 text-red",
    blue: "bg-blue/10 text-blue",
  };

  return (
    <div
      onClick={handleClick}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          handleClick();
        }
      }}
      className="flex flex-1 flex-col gap-2 rounded border border-border bg-surface p-3 hover:border-accent/30 transition-colors cursor-pointer focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2"
    >
      {/* Header */}
      <div className="flex items-center gap-2.5">
        <span className="text-xl leading-none">{emoji}</span>
        <div className="flex-1 min-w-0">
          <h3 className="text-sm font-semibold text-text">{framework.name}</h3>
          <p className="text-[10px] text-text-muted">{framework.id}</p>
        </div>
        {status.badge && (
          <span className={`rounded px-1.5 py-0.5 text-[10px] font-medium ${badgeColors[status.badge.color ?? ""] || ""}`}>
            {status.badge.label}
          </span>
        )}
      </div>

      {/* Description */}
      <p className="text-xs text-text-secondary line-clamp-3">
        {framework.description}
      </p>

      {/* Badges */}
      <div className="flex flex-wrap gap-1.5">
        <span className="rounded border border-border bg-surface-hover px-1.5 py-0.5 text-[10px] text-text-secondary">
          {framework.language}
        </span>
        <span className="rounded border border-border bg-surface-hover px-1.5 py-0.5 text-[10px] text-text-secondary">
          {framework.adapter_type}
        </span>
        <span className="rounded border border-border bg-surface-hover px-1.5 py-0.5 text-[10px] text-text-secondary">
          {framework.install_method}
        </span>
      </div>

      {/* Requirements */}
      {framework.requirements.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {framework.requirements.map((req) => (
            <span
              key={req}
              className="rounded bg-surface-hover px-1.5 py-0.5 text-[10px] text-text-muted"
            >
              {req}
            </span>
          ))}
        </div>
      )}

      {/* Action button — pushed to bottom, navigates to detail page */}
      <div className="flex-1" />
      <div
        className={`flex w-full items-center justify-center gap-2 rounded px-3 py-2 text-xs font-medium transition-colors ${
          status.needsSetup || status.isBinaryMissing
            ? "bg-yellow/10 text-yellow hover:bg-yellow/20"
            : status.isReady
              ? "bg-green/10 text-green hover:bg-green/20"
              : "border border-accent text-accent hover:bg-accent hover:text-white"
        }`}
      >
        {status.isBinaryMissing ? (
          <>
            <Download className="h-3.5 w-3.5" />
            install
          </>
        ) : status.needsSetup ? (
          <>
            <Settings className="h-3.5 w-3.5" />
            set up
          </>
        ) : status.isReady ? (
          <>
            <CheckCircle className="h-3.5 w-3.5" />
            manage
          </>
        ) : (
          <>
            <Download className="h-3.5 w-3.5" />
            install
          </>
        )}
      </div>

      {/* Links */}
      <div className="flex items-center justify-center gap-3">
        <a
          href={framework.repository}
          target="_blank"
          rel="noopener noreferrer"
          onClick={(e) => e.stopPropagation()}
          className="text-[10px] text-text-muted hover:text-accent transition-colors"
        >
          repository
        </a>
        {framework.website && (
          <>
            <span className="text-[10px] text-text-muted">·</span>
            <a
              href={framework.website}
              target="_blank"
              rel="noopener noreferrer"
              onClick={(e) => e.stopPropagation()}
              className="text-[10px] text-text-muted hover:text-accent transition-colors"
            >
              website
            </a>
          </>
        )}
      </div>
    </div>
  );
}
