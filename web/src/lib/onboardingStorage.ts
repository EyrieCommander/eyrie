import type { PhaseId } from "../components/OnboardingFlow";

export const ONBOARDING_STORAGE_KEY = "eyrie-onboarding";

export interface OnboardingState {
  phase?: string;
  fw?: string;
  step?: string;
  /** Per-framework API key confirmation (keyed by framework id). */
  apiKeyConfirmed?: Record<string, boolean>;
}

/** Read the saved onboarding position from localStorage. */
export function loadSaved(): OnboardingState {
  try {
    const raw = localStorage.getItem(ONBOARDING_STORAGE_KEY);
    if (!raw) return {};
    const parsed = JSON.parse(raw);
    if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) return {};
    return parsed;
  } catch {
    return {};
  }
}

/** Persist onboarding position to localStorage so navigating away and back
 *  restores where the user left off. */
export function saveCurrent(phase: PhaseId, fw?: string | null, step?: string | null) {
  try {
    // Merge into existing state to preserve fields like apiKeyConfirmed
    const saved = loadSaved() as Record<string, unknown>;
    saved.phase = phase;
    if (fw) saved.fw = fw; else delete saved.fw;
    if (step) saved.step = step; else delete saved.step;
    localStorage.setItem(ONBOARDING_STORAGE_KEY, JSON.stringify(saved));
  } catch { /* quota / private mode */ }
}

/** Merge fw/step into the existing saved state without overwriting phase. */
export function saveSubStep(fw: string | null, step: string | null) {
  try {
    const saved = loadSaved();
    if (fw) (saved as Record<string, string>).fw = fw; else delete saved.fw;
    if (step) (saved as Record<string, string>).step = step; else delete saved.step;
    localStorage.setItem(ONBOARDING_STORAGE_KEY, JSON.stringify(saved));
  } catch { /* quota / private mode */ }
}

/** Check if the API key step has been confirmed for a specific framework. */
export function isApiKeyConfirmed(fwId: string): boolean {
  const saved = loadSaved();
  return saved.apiKeyConfirmed?.[fwId] === true;
}

/** Save API key confirmation for a specific framework. */
export function setApiKeyConfirmedFor(fwId: string, confirmed: boolean) {
  try {
    const saved = loadSaved() as Record<string, unknown>;
    const map = (saved.apiKeyConfirmed as Record<string, boolean>) ?? {};
    if (confirmed) {
      map[fwId] = true;
    } else {
      delete map[fwId];
    }
    saved.apiKeyConfirmed = map;
    localStorage.setItem(ONBOARDING_STORAGE_KEY, JSON.stringify(saved));
  } catch { /* quota / private mode */ }
}
