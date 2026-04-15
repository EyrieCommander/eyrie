// Centralised framework status detection.
// Used by FrameworkCard (list view) and FrameworkDetail (detail page).

import type { Framework, InstallProgress } from "./types";

export interface FrameworkStatus {
  /** Binary exists on disk */
  isInstalled: boolean;
  /** Config file exists (onboarding done) */
  isConfigured: boolean;
  /** Binary missing but config or install-success record exists */
  isBinaryMissing: boolean;
  /** Installed but not yet configured */
  needsSetup: boolean;
  /** Installed and configured — fully operational */
  isReady: boolean;
  /** SSE install/uninstall is currently running */
  isRunning: boolean;
  /** Currently installing (not uninstalling) */
  isInstalling: boolean;
  /** Currently uninstalling */
  isUninstalling: boolean;
  /** Last install attempt failed */
  isError: boolean;
  /** Badge label + color for display */
  badge: { label: string; color: "green" | "yellow" | "red" | null } | null;
}

export function getFrameworkStatus(
  framework: Framework,
  installProgress?: InstallProgress | null,
): FrameworkStatus {
  const isInstalled = framework.installed ?? false;
  const isConfigured = framework.configured ?? false;
  const isRunning = installProgress?.status === "running";
  const isUninstalling = isRunning && !!installProgress?.message?.toLowerCase().includes("ninstall");
  const isInstalling = isRunning && !isUninstalling;
  const isError = installProgress?.status === "error" && !isInstalled;
  const isBinaryMissing = !isInstalled && (isConfigured || installProgress?.status === "success");
  const needsSetup = isInstalled && !isConfigured && !isInstalling;
  const isReady = isInstalled && isConfigured;

  let badge: FrameworkStatus["badge"] = null;
  if (isBinaryMissing) {
    badge = { label: "binary missing", color: "yellow" };
  } else if (isInstalling) {
    badge = { label: "installing", color: "yellow" };
  } else if (isUninstalling) {
    badge = { label: "uninstalling", color: "yellow" };
  } else if (isError) {
    badge = { label: "install failed", color: "red" };
  } else if (needsSetup) {
    badge = { label: "needs setup", color: "yellow" };
  } else if (isReady) {
    badge = { label: "ready", color: "green" };
  }

  return {
    isInstalled,
    isConfigured,
    isBinaryMissing,
    needsSetup,
    isReady,
    isRunning,
    isInstalling,
    isUninstalling,
    isError,
    badge,
  };
}
