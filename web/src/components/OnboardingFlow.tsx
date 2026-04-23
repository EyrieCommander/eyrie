// Unified onboarding flow — the new home route (/) for Eyrie.
//
// Replaces the redirect-to-mission-control that used to live at /. Renders a
// single-line macro timeline across three phases (commander → frameworks →
// projects) and the currently-active phase's content below it.
//
// Phase 0 (commander) auto-advances once the commander endpoint is healthy —
// for now it's a static "ready" placeholder (backend merges in step 4).
// Phase 1 (frameworks) is the meaty piece — 5-sub-step inner flow.
// Phase 2 (projects) is a single-page project form — implemented in step 3.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import MacroTimeline from "./MacroTimeline";
import CommanderPhase from "./phases/CommanderPhase";
import FrameworksPhase from "./phases/FrameworksPhase";
import ProjectsPhase from "./phases/ProjectsPhase";
import { useData } from "../lib/DataContext";
import { fetchFrameworks, fetchKeys, fetchCommanderHistory } from "../lib/api";
import {
  deriveApiKeyState,
  findProviderField,
  getFrameworkStatus,
} from "../lib/frameworkStatus";
import { loadSaved, saveCurrent } from "../lib/onboardingStorage";
import type { Framework, KeyEntry } from "../lib/types";

export type PhaseId = "commander" | "frameworks" | "projects";

const VALID_PHASES: PhaseId[] = ["commander", "frameworks", "projects"];

export type PhaseState = "complete" | "current" | "pending";

export interface PhaseStatus {
  commander: PhaseState;
  frameworks: PhaseState;
  projects: PhaseState;
}

/**
 * Lightweight poll of frameworks + keys for the macro-timeline's
 * "is phase 1 complete?" signal. FrameworksPhase does its own richer fetching
 * (including per-framework config); this is a summary-level data source.
 */
function useFrameworksSummary() {
  const [frameworks, setFrameworks] = useState<Framework[]>([]);
  const [keys, setKeys] = useState<KeyEntry[]>([]);

  useEffect(() => {
    let cancelled = false;
    const load = () =>
      Promise.allSettled([fetchFrameworks(), fetchKeys()]).then(
        ([fwRes, keyRes]) => {
          if (cancelled) return;
          if (fwRes.status === "fulfilled") setFrameworks(fwRes.value);
          if (keyRes.status === "fulfilled") setKeys(keyRes.value);
        },
      );
    load();
    const id = setInterval(load, 30_000);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, []);

  return { frameworks, keys };
}

/**
 * Is at least one framework fully ready (installed + configured + has key / no
 * key needed)? That's phase-1 completion per the plan.
 */
function anyFrameworkReady(frameworks: Framework[], keys: KeyEntry[]): boolean {
  return frameworks.some((fw) => {
    const providerField = findProviderField(fw);
    // We don't have the user's config on this summary path, so fall back to
    // the schema default when deciding which key to check. This is best-effort
    // — if the user overrode the provider in their config, the summary may
    // say "ready" based on the schema default instead. FrameworksPhase is the
    // authoritative view for the chosen framework; this is only for the macro
    // timeline's bird's-eye.
    const providerGuess =
      providerField && typeof providerField.default === "string"
        ? providerField.default
        : null;
    const apiKeyState = deriveApiKeyState(providerGuess, keys);
    const status = getFrameworkStatus(fw, null, apiKeyState);
    return status.isReady;
  });
}

export default function OnboardingFlow() {
  const { projects } = useData();
  const { frameworks, keys } = useFrameworksSummary();
  const [searchParams, setSearchParams] = useSearchParams();

  // Commander health: polls continuously so adding or deleting a key
  // is reflected promptly. Fast (3s) while unhealthy, slow (15s) once up.
  const [commanderHealthy, setCommanderHealthy] = useState<boolean | null>(null);
  const prevHealthy = useRef<boolean | null>(null);
  useEffect(() => {
    let cancelled = false;
    const interval = commanderHealthy === true ? 15_000 : 3_000;
    const check = () => {
      fetchCommanderHistory()
        .then(() => {
          if (cancelled) return;
          if (prevHealthy.current !== true) {
            prevHealthy.current = true;
            setCommanderHealthy(true);
          }
        })
        .catch(() => {
          if (cancelled) return;
          if (prevHealthy.current !== false) {
            prevHealthy.current = false;
            setCommanderHealthy(false);
          }
        });
    };
    check();
    const id = setInterval(check, interval);
    return () => { cancelled = true; clearInterval(id); };
  }, [commanderHealthy]);

  const frameworksReady = useMemo(
    () => anyFrameworkReady(frameworks, keys),
    [frameworks, keys],
  );
  const projectsComplete = projects.length > 0;

  // ── Phase from URL → localStorage → computed default ──────────────
  // Priority: URL param > localStorage > auto-computed default.
  // The URL is the source of truth once set; localStorage is the fallback
  // for when the user navigates away and returns to / with no params.
  const computeDefault = useCallback((): PhaseId => {
    return commanderHealthy === false ? "commander"
      : frameworksReady ? "projects"
        : "frameworks";
  }, [commanderHealthy, frameworksReady]);

  const [active, setActive] = useState<PhaseId>(() => {
    const urlPhase = searchParams.get("phase");
    if (urlPhase && VALID_PHASES.includes(urlPhase as PhaseId)) return urlPhase as PhaseId;
    const saved = loadSaved();
    if (saved.phase && VALID_PHASES.includes(saved.phase as PhaseId)) return saved.phase as PhaseId;
    return computeDefault();
  });

  // When no URL param AND no saved state, auto-position to the first
  // phase that needs work. Once the user has interacted (URL param exists
  // or they clicked a phase), stop auto-repositioning.
  const hasSavedPhase = useMemo(() => !!loadSaved().phase, []);
  const hasExplicitPosition = searchParams.has("phase") || hasSavedPhase;
  useEffect(() => {
    if (!hasExplicitPosition && active !== "commander") {
      setActive(computeDefault());
    }
  }, [computeDefault, hasExplicitPosition, active]);

  // Sync URL params whenever active phase changes. Uses `replace` so
  // each step doesn't add a browser-history entry.
  const updateParams = useCallback(
    (updates: Record<string, string | null>) => {
      setSearchParams((prev) => {
        const next = new URLSearchParams(prev);
        for (const [k, v] of Object.entries(updates)) {
          if (v === null) next.delete(k);
          else next.set(k, v);
        }
        return next;
      }, { replace: true });
    },
    [setSearchParams],
  );

  const handleSelect = useCallback(
    (id: PhaseId) => {
      setActive(id);
      // Switching phases: keep fw/step if going to frameworks, clear otherwise
      if (id !== "frameworks") {
        updateParams({ phase: id, fw: null, step: null });
        saveCurrent(id);
      } else {
        updateParams({ phase: id });
        saveCurrent(id, searchParams.get("fw"), searchParams.get("step"));
      }
    },
    [updateParams, searchParams],
  );

  const status = useMemo<PhaseStatus>(() => {
    const commanderDone = commanderHealthy === true;
    const commander: PhaseState =
      commanderDone ? "complete"
        : commanderHealthy === false ? "current"
          : "pending";
    const frameworks: PhaseState = frameworksReady
      ? "complete"
      : commanderDone
        ? "current"
        : "pending";
    const projects: PhaseState = projectsComplete
      ? "complete"
      : frameworksReady
        ? "current"
        : "pending";
    return { commander, frameworks, projects };
  }, [commanderHealthy, frameworksReady, projectsComplete]);

  return (
    <div className="space-y-6">
      <header>
        <div className="text-xs text-text-muted">~/home</div>
        <h1 className="mt-1 text-xl font-bold">
          <span className="text-accent">&gt;</span> let's get eyrie set up
        </h1>
        <p className="mt-1 text-xs text-text-muted">
          // install a framework, set up an API key, launch a project. ask the
          commander (right) if you get stuck.
        </p>
      </header>

      <MacroTimeline active={active} status={status} onSelect={handleSelect} />

      {active === "commander" && <CommanderPhase onContinue={() => handleSelect("frameworks")} />}
      {active === "frameworks" && <FrameworksPhase onNavigate={handleSelect} />}
      {active === "projects" && <ProjectsPhase />}
    </div>
  );
}
