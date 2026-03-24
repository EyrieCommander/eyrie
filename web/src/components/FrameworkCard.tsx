import { useEffect, useState } from "react";
import { Package, Download, CheckCircle, Loader2 } from "lucide-react";
import type { Framework, InstallProgress } from "../lib/types";

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

  // Force re-render every second for elapsed time
  const [, setTick] = useState(0);

  useEffect(() => {
    if (!isInstalling) return;
    const interval = setInterval(() => {
      setTick((t) => t + 1);
    }, 1000);
    return () => clearInterval(interval);
  }, [isInstalling]);

  // Calculate elapsed time for running installations
  const getElapsedTime = () => {
    if (!isInstalling || !installProgress?.started_at) return "";
    const elapsed = Date.now() - new Date(installProgress.started_at).getTime();
    const minutes = Math.floor(elapsed / 60000);
    const seconds = Math.floor((elapsed % 60000) / 1000);
    if (minutes < 1) return `${seconds}s`;
    return `${minutes}m ${seconds}s`;
  };

  const getLanguageColor = (lang: string) => {
    switch (lang.toLowerCase()) {
      case "rust":
        return "bg-orange-500/10 text-orange-500 border-orange-500/20";
      case "python":
        return "bg-yellow-500/10 text-yellow-500 border-yellow-500/20";
      case "typescript":
        return "bg-blue-500/10 text-blue-500 border-blue-500/20";
      default:
        return "bg-gray-500/10 text-gray-500 border-gray-500/20";
    }
  };

  const getAdapterColor = (adapter: string) => {
    switch (adapter) {
      case "http":
        return "bg-rose-500/10 text-rose-500";
      case "websocket":
        return "bg-indigo-500/10 text-indigo-500";
      case "cli":
        return "bg-cyan-500/10 text-cyan-500";
      default:
        return "bg-gray-500/10 text-gray-500";
    }
  };

  const getInstallMethodColor = (method: string) => {
    switch (method.toLowerCase()) {
      case "cargo":
        return "bg-teal-500/10 text-teal-500";
      case "npm":
        return "bg-pink-500/10 text-pink-500";
      case "script":
        return "bg-lime-500/10 text-lime-500";
      default:
        return "bg-slate-500/10 text-slate-500";
    }
  };

  return (
    <div className="border border-border rounded-lg p-6 hover:border-green transition-colors">
      {/* Header */}
      <div className="flex items-start justify-between mb-4">
        <div className="flex items-center gap-3">
          <div className="w-12 h-12 rounded bg-purple/10 flex items-center justify-center">
            <Package className="w-6 h-6 text-purple" />
          </div>
          <div>
            <h3 className="text-lg font-semibold text-fg">{framework.name}</h3>
            <p className="text-xs text-fg-muted">{framework.id}</p>
          </div>
        </div>
      </div>

      {/* Description */}
      <p className="text-sm text-fg-muted mb-4 line-clamp-2">
        {framework.description}
      </p>

      {/* Badges */}
      <div className="flex flex-wrap gap-2 mb-4">
        <span
          className={`text-xs px-2 py-1 rounded border ${getLanguageColor(framework.language)}`}
        >
          {framework.language}
        </span>
        <span
          className={`text-xs px-2 py-1 rounded ${getAdapterColor(framework.adapter_type)}`}
        >
          {framework.adapter_type}
        </span>
        <span className={`text-xs px-2 py-1 rounded ${getInstallMethodColor(framework.install_method)}`}>
          {framework.install_method}
        </span>
      </div>

      {/* Requirements */}
      {framework.requirements.length > 0 && (
        <div className="mb-4">
          <p className="text-xs text-fg-muted mb-2">requirements:</p>
          <div className="flex flex-wrap gap-1">
            {framework.requirements.map((req) => (
              <span
                key={req}
                className="text-xs px-2 py-0.5 rounded bg-fg-muted/5 text-fg-muted"
              >
                {req}
              </span>
            ))}
          </div>
        </div>
      )}

      {/* Install Button */}
      <button
        onClick={onInstall}
        disabled={disabled || isSuccess || isAlreadyInstalled}
        className={`
          group w-full px-4 py-2 rounded text-sm font-medium
          flex items-center justify-center gap-2
          transition-all duration-200
          ${
            isAlreadyInstalled
              ? "bg-green/10 text-green cursor-not-allowed"
              : isSuccess
                ? "bg-green/10 text-green cursor-not-allowed"
                : isError
                  ? "bg-red/10 text-red hover:bg-red/20 hover:shadow-md cursor-pointer"
                  : isInstalling
                    ? "border border-yellow text-yellow hover:bg-yellow hover:text-black hover:shadow-lg cursor-pointer"
                    : "border border-white text-white hover:bg-white hover:text-black hover:shadow-lg hover:-translate-y-0.5 active:translate-y-0 cursor-pointer"
          }
          disabled:opacity-50 disabled:cursor-not-allowed
        `}
      >
        {isAlreadyInstalled ? (
          <>
            <CheckCircle className="w-4 h-4" />
            already installed
          </>
        ) : isInstalling ? (
          <>
            <Loader2 className="w-4 h-4 animate-spin" />
            view progress
          </>
        ) : isSuccess ? (
          <>
            <CheckCircle className="w-4 h-4" />
            installed
          </>
        ) : isError ? (
          <>
            <Download className="w-4 h-4 transition-transform group-hover:scale-110" />
            retry install
          </>
        ) : (
          <>
            <Download className="w-4 h-4 transition-transform group-hover:scale-110 group-hover:animate-bounce" />
            install
          </>
        )}
      </button>

      {/* Elapsed time for running installations */}
      {isInstalling && (
        <div className="text-center mt-2">
          <span className="text-xs text-fg-muted">
            running for {getElapsedTime()}
          </span>
        </div>
      )}

      {/* Links */}
      <div className="flex items-center justify-center gap-3 mt-2">
        <a
          href={framework.repository}
          target="_blank"
          rel="noopener noreferrer"
          className="text-xs text-text-secondary hover:text-text transition-colors"
        >
          repository
        </a>
        {framework.website && (
          <>
            <span className="text-xs text-fg-muted">•</span>
            <a
              href={framework.website}
              target="_blank"
              rel="noopener noreferrer"
              className="text-xs text-text-secondary hover:text-text transition-colors"
            >
              website
            </a>
          </>
        )}
      </div>
    </div>
  );
}
