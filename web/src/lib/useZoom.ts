import { useState, useEffect, useCallback } from "react";

const STORAGE_KEY = "eyrie-zoom";
const DEFAULT_ZOOM = 100;
const MIN_ZOOM = 75;
const MAX_ZOOM = 150;
const STEP = 10;

function readZoom(): number {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored) {
      const val = Number(stored);
      if (!Number.isNaN(val) && val >= MIN_ZOOM && val <= MAX_ZOOM) return val;
    }
  } catch {
    // localStorage unavailable
  }
  return DEFAULT_ZOOM;
}

function applyZoom(zoom: number) {
  // Suppress transitions so rem-dependent properties snap instantly
  document.documentElement.classList.add("zoom-transitioning");
  document.documentElement.style.fontSize = `${zoom}%`;
  // Re-enable after the browser has repainted with new values
  requestAnimationFrame(() => {
    document.documentElement.classList.remove("zoom-transitioning");
  });
}

export function useZoom() {
  const [zoom, setZoomState] = useState(readZoom);

  useEffect(() => {
    applyZoom(zoom);
    try {
      localStorage.setItem(STORAGE_KEY, String(zoom));
    } catch {
      // localStorage unavailable
    }
  }, [zoom]);

  const setZoom = useCallback((value: number) => {
    setZoomState(Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, value)));
  }, []);

  const reset = useCallback(() => setZoomState(DEFAULT_ZOOM), []);

  return { zoom, setZoom, reset, min: MIN_ZOOM, max: MAX_ZOOM, step: STEP };
}
