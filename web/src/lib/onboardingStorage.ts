import type { PhaseId } from "../components/OnboardingFlow";

export const ONBOARDING_STORAGE_KEY = "eyrie-onboarding";

export interface OnboardingState {
  phase?: string;
  fw?: string;
  step?: string;
}

/** Read the saved onboarding position from localStorage. */
export function loadSaved(): OnboardingState {
  try {
    const raw = localStorage.getItem(ONBOARDING_STORAGE_KEY);
    return raw ? JSON.parse(raw) : {};
  } catch {
    return {};
  }
}

/** Persist onboarding position to localStorage so navigating away and back
 *  restores where the user left off. */
export function saveCurrent(phase: PhaseId, fw?: string | null, step?: string | null) {
  try {
    const obj: Record<string, string> = { phase };
    if (fw) obj.fw = fw;
    if (step) obj.step = step;
    localStorage.setItem(ONBOARDING_STORAGE_KEY, JSON.stringify(obj));
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
