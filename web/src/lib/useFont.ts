import { useState, useEffect, useCallback } from "react";

const STORAGE_KEY = "eyrie-font";

export const FONT_OPTIONS = [
  { id: "jetbrains-mono", label: "JetBrains Mono", family: '"JetBrains Mono", ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace' },
  { id: "sf-mono", label: "SF Mono", family: 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace' },
  { id: "fira-code", label: "Fira Code", family: '"Fira Code", ui-monospace, SFMono-Regular, monospace' },
  { id: "source-code-pro", label: "Source Code Pro", family: '"Source Code Pro", ui-monospace, SFMono-Regular, monospace' },
  { id: "ibm-plex-mono", label: "IBM Plex Mono", family: '"IBM Plex Mono", ui-monospace, Menlo, monospace' },
  { id: "inter", label: "Inter", family: '"Inter", system-ui, -apple-system, sans-serif' },
  { id: "system", label: "System Default", family: 'system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif' },
] as const;

export type FontId = (typeof FONT_OPTIONS)[number]["id"];

const DEFAULT_FONT: FontId = "jetbrains-mono";

function readFont(): FontId {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored && FONT_OPTIONS.some((f) => f.id === stored)) return stored as FontId;
  } catch {
    // localStorage unavailable
  }
  return DEFAULT_FONT;
}

function applyFont(fontId: FontId) {
  const option = FONT_OPTIONS.find((f) => f.id === fontId);
  if (option) {
    document.body.style.fontFamily = option.family;
  }
}

export function useFont() {
  const [font, setFontState] = useState(readFont);

  useEffect(() => {
    applyFont(font);
    try {
      localStorage.setItem(STORAGE_KEY, font);
    } catch {
      // localStorage unavailable
    }
  }, [font]);

  const setFont = useCallback((id: FontId) => {
    if (FONT_OPTIONS.some((f) => f.id === id)) setFontState(id);
  }, []);

  const reset = useCallback(() => setFontState(DEFAULT_FONT), []);

  return { font, setFont, reset };
}
