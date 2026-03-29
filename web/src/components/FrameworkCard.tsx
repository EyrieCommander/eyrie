import { useEffect, useState } from "react";
import { Download, CheckCircle, Loader2 } from "lucide-react";
import type { Framework, InstallProgress } from "../lib/types";

const FRAMEWORK_EMOJI: Record<string, string> = {
  zeroclaw: "🌀",
  openclaw: "🦞",
  hermes: "🔱",
};

interface FrameworkCardProps {
  framework: Framework;
  installProgress?: InstallProgress;
  onInstall: () => void;
  disabled?: boolean;
}

export default function FrameworkCard({
  framework,
  installProgress,
  onInstall,
  disabled,
}: FrameworkCardProps) {
  const isInstalling = installProgress?.status === "running";
  const isSuccess = installProgress?.status === "success";
  const isError = installProgress?.status === "error";
  const isAlreadyInstalled = framework.installed && !installProgress;

  const [, setTick] = useState(0);

  useEffect(() => {
    if (!isInstalling) return;
    const interval = setInterval(() => setTick((t) => t + 1), 1000);
    return () => clearInterval(interval);
  }, [isInstalling]);

  const getElapsedTime = () => {
    if (!isInstalling || !installProgress?.started_at) return "";
    const elapsed = Date.now() - new Date(installProgress.started_at).getTime();
    const minutes = Math.floor(elapsed / 60000);
    const seconds = Math.floor((elapsed % 60000) / 1000);
    if (minutes < 1) return `${seconds}s`;
    return `${minutes}m ${seconds}s`;
  };

  const emoji = FRAMEWORK_EMOJI[framework.id] || "";

  return (
    <div className="rounded border border-border bg-surface p-4 hover:border-accent/30 transition-colors space-y-3">
      {/* Header */}
      <div className="flex items-center gap-2.5">
        <span className="text-xl leading-none">{emoji}</span>
        <div className="flex-1 min-w-0">
          <h3 className="text-sm font-semibold text-text">{framework.name}</h3>
          <p className="text-[10px] text-text-muted">{framework.id}</p>
        </div>
        {isAlreadyInstalled && (
          <span className="rounded bg-green/10 px-1.5 py-0.5 text-[10px] font-medium text-green">
            installed
          </span>
        )}
      </div>

      {/* Description */}
      <p className="text-xs text-text-secondary line-clamp-2">
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

      {/* Install button */}
      <button
        onClick={onInstall}
        disabled={disabled || isSuccess || isAlreadyInstalled}
        className={`flex w-full items-center justify-center gap-2 rounded px-3 py-2 text-xs font-medium transition-colors ${
          isAlreadyInstalled || isSuccess
            ? "bg-green/10 text-green cursor-default"
            : isError
              ? "bg-red/10 text-red hover:bg-red/20"
              : isInstalling
                ? "border border-yellow text-yellow hover:bg-yellow/10"
                : "border border-accent text-accent hover:bg-accent hover:text-white"
        } disabled:opacity-50 disabled:cursor-not-allowed`}
      >
        {isAlreadyInstalled ? (
          <>
            <CheckCircle className="h-3.5 w-3.5" />
            installed
          </>
        ) : isInstalling ? (
          <>
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
            view progress
          </>
        ) : isSuccess ? (
          <>
            <CheckCircle className="h-3.5 w-3.5" />
            installed
          </>
        ) : isError ? (
          <>
            <Download className="h-3.5 w-3.5" />
            retry install
          </>
        ) : (
          <>
            <Download className="h-3.5 w-3.5" />
            install
          </>
        )}
      </button>

      {isInstalling && (
        <p className="text-center text-[10px] text-text-muted">
          running for {getElapsedTime()}
        </p>
      )}

      {/* Links */}
      <div className="flex items-center justify-center gap-3">
        <a
          href={framework.repository}
          target="_blank"
          rel="noopener noreferrer"
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
