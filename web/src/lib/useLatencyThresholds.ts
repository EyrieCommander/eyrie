import { useState, useEffect, useCallback } from "react";

const STORAGE_KEY = "eyrie-latency-thresholds";

export interface LatencyThresholds {
  warn: number;  // ms — above this is yellow
  error: number; // ms — above this is red
}

const DEFAULTS: LatencyThresholds = { warn: 12000, error: 24000 };

function read(): LatencyThresholds {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw) {
      const parsed = JSON.parse(raw);
      if (parsed.warn > 0 && parsed.error > 0) return parsed;
    }
  } catch {}
  return DEFAULTS;
}

export function useLatencyThresholds() {
  const [thresholds, setThresholdsState] = useState(read);

  useEffect(() => {
    try { localStorage.setItem(STORAGE_KEY, JSON.stringify(thresholds)); } catch {}
  }, [thresholds]);

  const setThresholds = useCallback((t: LatencyThresholds) => {
    // Ensure error > warn
    setThresholdsState({ warn: t.warn, error: Math.max(t.error, t.warn + 1000) });
  }, []);

  const reset = useCallback(() => setThresholdsState(DEFAULTS), []);

  return { thresholds, setThresholds, reset, defaults: DEFAULTS };
}

/** Returns the color class for a latency value. */
export function latencyColor(ms: number, thresholds: LatencyThresholds): string {
  if (ms > thresholds.error) return "text-red";
  if (ms > thresholds.warn) return "text-yellow";
  return "text-accent";
}
