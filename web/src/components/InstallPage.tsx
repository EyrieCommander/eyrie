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

  useEffect(() => {
    if (showLogs && logEndRef.current) {
      logEndRef.current.scrollIntoView({ behavior: "smooth" });
    }
  }, [installLogs, showLogs]);

  const loadFrameworks = async (refresh = false) => {
    try {
      setLoading(true);
      setError(null);
      const data = await fetchFrameworks(refresh);
      setFrameworks(data);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load frameworks");
    } finally {
      setLoading(false);
    }
  };

  const handleManage = (frameworkId: string) => {
    setSelectedFramework(frameworkId);
    setInstallProgress((prev) => ({
      ...prev,
      [frameworkId]: { framework_id: frameworkId, phase: "complete", status: "success", progress: 100 },
    }));
    setInstallLogs((prev) => ({ ...prev, [frameworkId]: [`${frameworkId} is installed and ready.`] }));
    setShowLogs(true);
  };

  const handleInstall = (frameworkId: string) => {
    if (abortControllers.current[frameworkId]) {
      abortControllers.current[frameworkId].abort();
    }

    setSelectedFramework(frameworkId);
    setInstallLogs((prev) => ({ ...prev, [frameworkId]: [] }));
    setShowLogs(true);

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
      shouldForce,
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
    <div className="flex flex-col h-full space-y-6">
      <div className="text-xs text-text-muted">~/install</div>

      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-bold">
            <span className="text-accent">&gt;</span> install
          </h1>
          <p className="mt-1 text-xs text-text-muted">
            // browse and install agent frameworks from the registry
          </p>
        </div>
        <button
          onClick={() => loadFrameworks(true)}
          disabled={loading}
          className="flex items-center gap-2 text-xs text-text-muted transition-colors hover:text-text disabled:opacity-50"
        >
          <RefreshCw className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} />
          $ refresh
        </button>
      </div>

      {error && (
        <div className="rounded border border-red/30 bg-red/5 px-4 py-3 flex items-start gap-2">
          <AlertCircle className="h-3.5 w-3.5 text-red mt-0.5 flex-shrink-0" />
          <div>
            <p className="text-xs text-red font-medium">failed to load frameworks</p>
            <p className="text-[10px] text-red/80 mt-0.5">{error}</p>
          </div>
        </div>
      )}

      {loading && !frameworks.length && (
        <div className="py-12 text-center text-xs text-text-muted">
          loading frameworks...
        </div>
      )}

      {!loading && frameworks.length > 0 && (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {frameworks.map((fw) => (
            <FrameworkCard
              key={fw.id}
              framework={fw}
              installProgress={installProgress[fw.id]}
              onInstall={() => handleInstall(fw.id)}
              onManage={() => handleManage(fw.id)}
              disabled={loading}
            />
          ))}
        </div>
      )}

      {!loading && !error && frameworks.length === 0 && (
        <div className="rounded border border-border bg-surface p-8 text-center text-xs text-text-muted">
          <Package className="h-8 w-8 text-text-muted/30 mx-auto mb-2" />
          no frameworks available — check registry configuration
        </div>
      )}

      {/* Install logs panel */}
      {showLogs && selectedFramework && (
        <div className="fixed bottom-0 left-0 right-0 bg-bg border-t border-border shadow-lg z-50">
          <div className="max-w-5xl mx-auto px-8 py-4">
            <div className="flex items-center justify-between mb-3">
              <div className="flex items-center gap-2">
                <h3 className="text-xs font-semibold text-text">
                  {currentProgress?.status === "success"
                    ? `${selectedFramework} installed`
                    : `installing ${selectedFramework}`}
                </h3>
                {currentProgress && currentProgress.status === "running" && (
                  <span className="text-[10px] text-text-muted">
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
                      className="flex items-center gap-1.5 px-3 py-1.5 bg-accent hover:bg-accent-hover text-white rounded text-xs font-medium transition-colors"
                    >
                      <TerminalIcon className="h-3 w-3" />
                      launch terminal
                    </button>
                    <button
                      onClick={() => navigate(`/agents/${selectedFramework}/config`)}
                      className="flex items-center gap-1.5 px-3 py-1.5 border border-border text-text-secondary hover:text-text rounded text-xs font-medium transition-colors"
                    >
                      <Settings className="h-3 w-3" />
                      configure
                    </button>
                  </>
                )}
                <button
                  onClick={() => setShowLogs(false)}
                  className="text-xs text-text-muted hover:text-text transition-colors"
                >
                  close
                </button>
              </div>
            </div>

            <div className="rounded border border-border bg-surface p-3 max-h-48 overflow-y-auto font-mono text-[10px]">
              {currentLogs.length === 0 ? (
                <p className="text-text-muted">starting installation...</p>
              ) : (
                currentLogs.map((log, i) => (
                  <div key={i} className="text-text-secondary whitespace-pre-wrap">
                    {log}
                  </div>
                ))
              )}
              <div ref={logEndRef} />
            </div>
          </div>
        </div>
      )}

      {showTerminal && selectedFramework && (
        <Terminal
          agentName={selectedFramework}
          onClose={() => setShowTerminal(false)}
        />
      )}
    </div>
  );
}
