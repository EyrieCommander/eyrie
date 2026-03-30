import { useEffect, useState } from "react";
import { Download, CheckCircle, Loader2, Settings } from "lucide-react";
import { FRAMEWORK_EMOJI } from "../lib/types";
import type { Framework, InstallProgress } from "../lib/types";

interface FrameworkCardProps {
  framework: Framework;
  installProgress?: InstallProgress;
  onInstall: () => void;
  onManage?: () => void;
  onSetup?: () => void;
  disabled?: boolean;
}

export default function FrameworkCard({
  framework,
  installProgress,
  onInstall,
  onManage,
  onSetup,
  disabled,
}: FrameworkCardProps) {
  const isInstalling = installProgress?.status === "running";
  // A cached "success" status is stale if the binary no longer exists on disk.
  const isSuccess = installProgress?.status === "success" && framework.installed;
  const isAlreadyInstalled = framework.installed && !isInstalling;
  const isConfigured = framework.configured;
  const needsSetup = isAlreadyInstalled && !isConfigured;
  const isError = installProgress?.status === "error" && !isAlreadyInstalled;
  // Install claimed success but binary is missing — treat as failed
  const isStale = installProgress?.status === "success" && !framework.installed;

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
    <div className="flex flex-col gap-2 rounded border border-border bg-surface p-3 hover:border-accent/30 transition-colors">
      {/* Header */}
      <div className="flex items-center gap-2.5">
        <span className="text-xl leading-none">{emoji}</span>
        <div className="flex-1 min-w-0">
          <h3 className="text-sm font-semibold text-text">{framework.name}</h3>
          <p className="text-[10px] text-text-muted">{framework.id}</p>
        </div>
        {isStale ? (
          <span className="rounded bg-red/10 px-1.5 py-0.5 text-[10px] font-medium text-red">
            not found
          </span>
        ) : isAlreadyInstalled && needsSetup ? (
          <span className="rounded bg-yellow/10 px-1.5 py-0.5 text-[10px] font-medium text-yellow">
            needs setup
          </span>
        ) : isAlreadyInstalled ? (
          <span className="rounded bg-green/10 px-1.5 py-0.5 text-[10px] font-medium text-green">
            ready
          </span>
        ) : null}
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
        onClick={isStale ? onInstall : needsSetup && onSetup ? onSetup : (isAlreadyInstalled || isSuccess) && onManage ? onManage : onInstall}
        disabled={disabled}
        className={`flex w-full items-center justify-center gap-2 rounded px-3 py-2 text-xs font-medium transition-colors ${
          isStale
            ? "bg-red/10 text-red hover:bg-red/20"
            : needsSetup
            ? "bg-yellow/10 text-yellow hover:bg-yellow/20"
            : (isSuccess || isAlreadyInstalled)
            ? "bg-green/10 text-green hover:bg-green/20"
            : isError
              ? "bg-red/10 text-red hover:bg-red/20"
              : isInstalling
                ? "border border-yellow text-yellow hover:bg-yellow/10"
                : "border border-accent text-accent hover:bg-accent hover:text-white"
        } disabled:opacity-50 disabled:cursor-not-allowed`}
      >
        {isStale ? (
          <>
            <Download className="h-3.5 w-3.5" />
            reinstall
          </>
        ) : isAlreadyInstalled && needsSetup ? (
          <>
            <Settings className="h-3.5 w-3.5" />
            set up
          </>
        ) : isAlreadyInstalled ? (
          <>
            <Settings className="h-3.5 w-3.5" />
            manage
          </>
        ) : isInstalling ? (
          <>
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
            view progress
          </>
        ) : isSuccess ? (
          <>
            <CheckCircle className="h-3.5 w-3.5" />
            manage
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
