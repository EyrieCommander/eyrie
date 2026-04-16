// Centralised framework status detection.
// Used by FrameworkCard (list view), FrameworkDetail (detail page), and the
// new FrameworksPhase (onboarding flow) to compute a single source of truth
// for whether a framework is installable, configurable, has a key, ready, etc.

import type { ConfigField, Framework, InstallProgress, KeyEntry } from "./types";

/**
 * Providers that don't need a user-supplied API key. If a framework's selected
 * provider is in this set, the API-key sub-step auto-skips.
 */
export const LOCAL_PROVIDERS = new Set([
  "ollama",
  "vercel-ai-gateway",
  "local",
]);

export interface ApiKeyState {
  /** The provider name as it appears in the framework's config (lowercased). */
  provider: string;
  /** KeyVault has a non-empty key for this provider. */
  hasKey: boolean;
  /** Provider is in {@link LOCAL_PROVIDERS} — UI should mark the api-key step
   *  as skipped rather than incomplete. */
  isLocal: boolean;
}

export interface FrameworkStatus {
  /** Binary exists on disk */
  isInstalled: boolean;
  /** Config file exists (onboarding done) */
  isConfigured: boolean;
  /** Binary missing but config or install-success record exists */
  isBinaryMissing: boolean;
  /** Installed but not yet configured */
  needsSetup: boolean;
  /** KeyVault has a key (or a local provider bypasses the need). */
  hasApiKey: boolean;
  /** Installed + configured but no key and not a local provider. */
  needsApiKey: boolean;
  /** The provider detected from the framework's config, if any. */
  apiKeyProvider: string | null;
  /** True when the detected provider is local and doesn't need a key. */
  skipApiKey: boolean;
  /** Installed, configured, AND has (or skips) the api key — fully operational. */
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
  badge: {
    label: string;
    color: "green" | "yellow" | "red" | "blue" | null;
  } | null;
}

/**
 * Find the config field that names the LLM provider for this framework.
 * Checks common key names; returns null if none of them exist.
 */
export function findProviderField(framework: Framework): ConfigField | null {
  const fields = framework.config_schema?.common_fields ?? [];
  return (
    fields.find(
      (f) =>
        f.key === "provider" ||
        f.key === "default_provider" ||
        f.key === "agents.defaults.provider",
    ) ?? null
  );
}

/**
 * Given a provider name (as it appears in the config file, e.g. "openrouter")
 * and the current KeyVault entries, derive an {@link ApiKeyState}.
 *
 * Returns null when the caller doesn't know which provider is selected yet —
 * {@link getFrameworkStatus} will then fall back to the pre-api-key-awareness
 * behaviour (treat key as satisfied).
 */
export function deriveApiKeyState(
  providerFromConfig: string | null | undefined,
  keys: KeyEntry[] | null | undefined,
): ApiKeyState | null {
  if (!providerFromConfig) return null;
  const provider = providerFromConfig.toLowerCase();
  if (LOCAL_PROVIDERS.has(provider)) {
    return { provider, hasKey: true, isLocal: true };
  }
  const entry = keys?.find((k) => k.provider.toLowerCase() === provider);
  return { provider, hasKey: !!entry?.has_key, isLocal: false };
}

export function getFrameworkStatus(
  framework: Framework,
  installProgress?: InstallProgress | null,
  apiKey?: ApiKeyState | null,
): FrameworkStatus {
  const isInstalled = framework.installed ?? false;
  const isConfigured = framework.configured ?? false;
  const isRunning = installProgress?.status === "running";
  const isUninstalling =
    isRunning && !!installProgress?.message?.toLowerCase().includes("ninstall");
  const isInstalling = isRunning && !isUninstalling;
  const isError = installProgress?.status === "error" && !isInstalled;
  const isBinaryMissing =
    !isInstalled && (isConfigured || installProgress?.status === "success");
  const needsSetup = isInstalled && !isConfigured && !isInstalling;

  // API key awareness. When the caller didn't pass info (legacy callers that
  // haven't been updated yet), treat the key as satisfied so the existing
  // "ready at install+configure" behaviour is preserved.
  const hasApiKey = apiKey ? apiKey.hasKey || apiKey.isLocal : true;
  const skipApiKey = apiKey?.isLocal ?? false;
  const apiKeyProvider = apiKey?.provider ?? null;
  const needsApiKey = isInstalled && isConfigured && !hasApiKey;
  const isReady = isInstalled && isConfigured && hasApiKey;

  let badge: FrameworkStatus["badge"] = null;
  if (isBinaryMissing) {
    badge = { label: "binary missing", color: "yellow" };
  } else if (isInstalling) {
    badge = { label: "installing", color: "blue" };
  } else if (isUninstalling) {
    badge = { label: "uninstalling", color: "blue" };
  } else if (isError) {
    badge = { label: "install failed", color: "red" };
  } else if (needsSetup) {
    badge = { label: "needs setup", color: "yellow" };
  } else if (needsApiKey) {
    badge = { label: "needs api key", color: "yellow" };
  } else if (isReady) {
    badge = { label: "ready", color: "green" };
  }

  return {
    isInstalled,
    isConfigured,
    isBinaryMissing,
    needsSetup,
    hasApiKey,
    needsApiKey,
    apiKeyProvider,
    skipApiKey,
    isReady,
    isRunning,
    isInstalling,
    isUninstalling,
    isError,
    badge,
  };
}
