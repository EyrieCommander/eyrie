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

import { useEffect, useMemo, useState } from "react";
import MacroTimeline from "./MacroTimeline";
import CommanderPhase from "./phases/CommanderPhase";
import FrameworksPhase from "./phases/FrameworksPhase";
import ProjectsPhase from "./phases/ProjectsPhase";
import { useData } from "../lib/DataContext";
import { fetchFrameworks, fetchKeys } from "../lib/api";
import {
  deriveApiKeyState,
  findProviderField,
  getFrameworkStatus,
} from "../lib/frameworkStatus";
import type { Framework, KeyEntry } from "../lib/types";

export type PhaseId = "commander" | "frameworks" | "projects";

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

  const frameworksReady = useMemo(
    () => anyFrameworkReady(frameworks, keys),
    [frameworks, keys],
  );
  const projectsComplete = projects.length > 0;

  // Default-land on the first-incomplete phase (frameworks until one is ready,
  // then projects until one exists, then projects again as the home base).
  const defaultActive: PhaseId = frameworksReady
    ? projectsComplete
      ? "projects"
      : "projects"
    : "frameworks";
  const [active, setActive] = useState<PhaseId>(defaultActive);
  // Only auto-reposition the user if they haven't picked a phase manually.
  const [touched, setTouched] = useState(false);
  useEffect(() => {
    if (!touched) setActive(defaultActive);
  }, [defaultActive, touched]);

  const status = useMemo<PhaseStatus>(() => {
    // Commander is always complete for now (backend merges in step 4).
    const commander: PhaseState = "complete";
    const frameworks: PhaseState = frameworksReady
      ? "complete"
      : active === "frameworks"
        ? "current"
        : "pending";
    const projects: PhaseState = projectsComplete
      ? "complete"
      : frameworksReady
        ? active === "projects"
          ? "current"
          : "current"
        : "pending";
    return { commander, frameworks, projects };
  }, [active, frameworksReady, projectsComplete]);

  const handleSelect = (id: PhaseId) => {
    setTouched(true);
    setActive(id);
  };

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

      {active === "commander" && <CommanderPhase />}
      {active === "frameworks" && <FrameworksPhase />}
      {active === "projects" && <ProjectsPhase />}
    </div>
  );
}
