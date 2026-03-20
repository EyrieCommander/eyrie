import { useEffect, useState, useRef } from "react";
import { useNavigate } from "react-router-dom";
import { AlertCircle, RefreshCw, Package, Settings, Terminal as TerminalIcon } from "lucide-react";
import FrameworkCard from "./FrameworkCard";
import Terminal from "./Terminal";
import type { Framework, InstallProgress } from "../lib/types";
import { fetchFrameworks, fetchInstallStatus, streamInstall } from "../lib/api";

export default function InstallPage() {
  const navigate = useNavigate();
  const [frameworks, setFrameworks] = useState<Framework[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [installProgress, setInstallProgress] = useState<
    Record<string, InstallProgress>
  >({});
  const [installLogs, setInstallLogs] = useState<Record<string, string[]>>({});
  const [selectedFramework, setSelectedFramework] = useState<string | null>(
    null,
  );
  const [showLogs, setShowLogs] = useState(false);
  const [showTerminal, setShowTerminal] = useState(false);

  const abortControllers = useRef<Record<string, AbortController>>({});
  const logEndRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    loadFrameworks();
    loadInstallStatus();
  }, []);

  const loadInstallStatus = async () => {
    try {
      const statuses = await fetchInstallStatus();
      setInstallProgress(statuses);
    } catch (e) {
      console.error("Failed to load install status:", e);
    }
  };

  // Auto-scroll logs
  useEffect(() => {
    if (showLogs && logEndRef.current) {
      logEndRef.current.scrollIntoView({ behavior: "smooth" });
    }
  }, [installLogs, showLogs]);

  const loadFrameworks = async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await fetchFrameworks();
      setFrameworks(data);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load frameworks");
    } finally {
      setLoading(false);
    }
  };

  const handleInstall = (frameworkId: string) => {
    // Cancel any existing install for this framework
    if (abortControllers.current[frameworkId]) {
      abortControllers.current[frameworkId].abort();
    }

    setSelectedFramework(frameworkId);
    setInstallLogs((prev) => ({ ...prev, [frameworkId]: [] }));
    setShowLogs(true);

    // Only force-restart if the previous install failed (error status)
    // For "running" status, just connect to the existing stream
    const existingProgress = installProgress[frameworkId];
    const shouldForce = existingProgress && existingProgress.status === "error";

    const controller = streamInstall(
      frameworkId,
      undefined,
      (progress) => {
        setInstallProgress((prev) => ({
          ...prev,
          [frameworkId]: progress,
        }));

        // Close logs panel on success
        if (progress.status === "success") {
          setTimeout(() => {
            if (selectedFramework === frameworkId) {
              setShowLogs(false);
            }
          }, 2000);
        }
      },
      (log) => {
        setInstallLogs((prev) => ({
          ...prev,
          [frameworkId]: [...(prev[frameworkId] || []), log],
        }));
      },
      shouldForce, // Force restart if retrying failed/stale installation
    );

    abortControllers.current[frameworkId] = controller;
  };

  const currentLogs = selectedFramework
    ? installLogs[selectedFramework] || []
    : [];
  const currentProgress = selectedFramework
    ? installProgress[selectedFramework]
    : undefined;

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="mb-6">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-bold text-fg mb-1">
              install
            </h1>
            <p className="text-sm text-fg-muted">
              browse and install claw agent frameworks from the registry
            </p>
          </div>
          <button
            onClick={loadFrameworks}
            disabled={loading}
            className="p-2 hover:bg-fg-muted/5 rounded transition-colors disabled:opacity-50"
            title="Refresh frameworks"
          >
            <RefreshCw
              className={`w-5 h-5 text-fg-muted ${loading ? "animate-spin" : ""}`}
            />
          </button>
        </div>
      </div>

      {/* Error Message */}
      {error && (
        <div className="mb-6 rounded border border-red/30 bg-red/5 px-4 py-3 flex items-start gap-2">
          <AlertCircle className="w-4 h-4 text-red mt-0.5 flex-shrink-0" />
          <div className="flex-1">
            <p className="text-sm text-red font-medium">Error loading frameworks</p>
            <p className="text-xs text-red/80 mt-1">{error}</p>
          </div>
        </div>
      )}

      {/* Loading State */}
      {loading && !frameworks.length && (
        <div className="flex items-center justify-center py-12">
          <div className="text-center">
            <RefreshCw className="w-8 h-8 text-fg-muted animate-spin mx-auto mb-3" />
            <p className="text-sm text-fg-muted">Loading frameworks...</p>
          </div>
        </div>
      )}

      {/* Frameworks Grid */}
      {!loading && frameworks.length > 0 && (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4 mb-6">
          {frameworks.map((fw) => (
            <FrameworkCard
              key={fw.id}
              framework={fw}
              installProgress={installProgress[fw.id]}
              onInstall={() => handleInstall(fw.id)}
              disabled={loading}
            />
          ))}
        </div>
      )}

      {/* Empty State */}
      {!loading && !error && frameworks.length === 0 && (
        <div className="flex items-center justify-center py-12">
          <div className="text-center">
            <Package className="w-12 h-12 text-fg-muted/50 mx-auto mb-3" />
            <p className="text-sm text-fg-muted">No frameworks available</p>
            <p className="text-xs text-fg-muted mt-1">
              Check the registry configuration
            </p>
          </div>
        </div>
      )}

      {/* Install Logs Panel */}
      {showLogs && selectedFramework && (
        <div className="fixed bottom-0 left-0 right-0 bg-bg border-t border-border shadow-lg z-50">
          <div className="max-w-7xl mx-auto px-8 py-4">
            <div className="flex items-center justify-between mb-3">
              <div className="flex items-center gap-2">
                <h3 className="text-sm font-semibold text-fg">
                  {currentProgress?.status === "success"
                    ? `${selectedFramework} installed successfully`
                    : `Installing ${selectedFramework}`}
                </h3>
                {currentProgress && currentProgress.status === "running" && (
                  <span className="text-xs text-fg-muted">
                    {currentProgress.phase} ({currentProgress.progress}%)
                  </span>
                )}
              </div>
              <div className="flex items-center gap-2">
                {currentProgress?.status === "success" && (
                  <>
                    <button
                      onClick={() => {
                        setShowTerminal(true);
                        setShowLogs(false);
                      }}
                      className="flex items-center gap-2 px-3 py-1.5 bg-green hover:bg-green/90 text-white rounded text-xs font-medium transition-colors"
                    >
                      <TerminalIcon className="w-3.5 h-3.5" />
                      launch terminal
                    </button>
                    <button
                      onClick={() => navigate(`/agents/${selectedFramework}/config`)}
                      className="flex items-center gap-2 px-3 py-1.5 bg-accent hover:bg-accent-hover text-white rounded text-xs font-medium transition-colors"
                    >
                      <Settings className="w-3.5 h-3.5" />
                      configure agent
                    </button>
                  </>
                )}
                <button
                  onClick={() => setShowLogs(false)}
                  className="text-xs text-fg-muted hover:text-fg transition-colors"
                >
                  close
                </button>
              </div>
            </div>

            <div className="bg-black/90 rounded border border-border p-3 max-h-48 overflow-y-auto font-mono text-xs">
              {currentLogs.length === 0 ? (
                <p className="text-fg-muted">Starting installation...</p>
              ) : (
                currentLogs.map((log, i) => (
                  <div key={i} className="text-fg-muted/90 whitespace-pre-wrap">
                    {log}
                  </div>
                ))
              )}
              <div ref={logEndRef} />
            </div>
          </div>
        </div>
      )}

      {/* Terminal Modal */}
      {showTerminal && selectedFramework && (
        <Terminal
          agentName={selectedFramework}
          onClose={() => setShowTerminal(false)}
        />
      )}
    </div>
  );
}
