import { useZoom } from "../lib/useZoom";
import { useFont, FONT_OPTIONS, type FontId } from "../lib/useFont";
import { useTheme, type Theme } from "../lib/useTheme";
import { useLatencyThresholds } from "../lib/useLatencyThresholds";
import { useState, useEffect, useRef } from "react";
import { Minus, Plus, RotateCcw, Moon, Sun, Check } from "lucide-react";
import ApiKeysSection from "./ApiKeysSection";

export default function SettingsPage() {
  const { zoom, setZoom, reset: resetZoom, min, max, step } = useZoom();
  const { font, setFont, reset: resetFont } = useFont();
  const { theme, setTheme } = useTheme();
  const { thresholds, setThresholds, reset: resetThresholds, defaults } = useLatencyThresholds();
  const [thresholdSaved, setThresholdSaved] = useState(false);
  const savedTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const [warnInput, setWarnInput] = useState(String(Math.round(thresholds.warn / 1000)));
  const [errorInput, setErrorInput] = useState(String(Math.round(thresholds.error / 1000)));

  // Clean up pending timer on unmount
  useEffect(() => {
    return () => { if (savedTimerRef.current) clearTimeout(savedTimerRef.current); };
  }, []);

  // Sync inputs when thresholds change externally (e.g., reset)
  useEffect(() => {
    setWarnInput(String(Math.round(thresholds.warn / 1000)));
    setErrorInput(String(Math.round(thresholds.error / 1000)));
  }, [thresholds.warn, thresholds.error]);

  const commitThresholds = (warn: string, error: string) => {
    const w = Math.max(1, Number(warn) || 1);
    const e = Math.max(w + 1, Number(error) || w + 1);
    setThresholds({ warn: w * 1000, error: e * 1000 });
    setThresholdSaved(true);
    if (savedTimerRef.current) clearTimeout(savedTimerRef.current);
    savedTimerRef.current = setTimeout(() => setThresholdSaved(false), 1000);
  };

  return (
    <div className="space-y-6">
      <div className="text-xs text-text-muted">~/settings</div>

      <div>
        <h1 className="text-xl font-bold">
          <span className="text-accent">&gt;</span> settings
        </h1>
        <p className="mt-1 text-xs text-text-muted">
          // dashboard appearance
        </p>
      </div>

      {/* API Keys */}
      <ApiKeysSection />

      {/* Theme + Zoom + Latency */}
      <div className="grid gap-4 md:grid-cols-3">
        {/* Theme */}
        <div className="rounded border border-border bg-surface p-4 space-y-3">
          <div>
            <h3 className="text-xs font-medium text-text">theme</h3>
            <p className="text-[10px] text-text-muted mt-0.5">
              dark or light mode
            </p>
          </div>
          <div className="flex gap-2">
            {([
              { id: "dark" as Theme, label: "dark", Icon: Moon },
              { id: "light" as Theme, label: "light", Icon: Sun },
            ]).map(({ id, label, Icon }) => (
              <button
                key={id}
                onClick={() => setTheme(id)}
                className={`flex items-center gap-2 rounded border px-4 py-2.5 text-xs font-medium transition-colors ${
                  theme === id
                    ? "border-accent bg-accent/5 text-accent"
                    : "border-border hover:border-text-muted/50 text-text-secondary hover:text-text"
                }`}
              >
                <Icon className="h-3.5 w-3.5" />
                {label}
              </button>
            ))}
          </div>
        </div>

        {/* Zoom */}
        <div className="rounded border border-border bg-surface p-4 space-y-3 overflow-hidden">
          <div className="flex items-center justify-between">
            <div>
              <h3 className="text-xs font-medium text-text">zoom level</h3>
              <p className="text-[10px] text-text-muted mt-0.5">
                scales the entire UI
              </p>
            </div>
            {zoom !== 100 && (
              <button
                onClick={resetZoom}
                className="flex items-center gap-1 text-[10px] text-text-muted hover:text-accent transition-colors"
              >
                <RotateCcw className="h-2.5 w-2.5" /> reset
              </button>
            )}
          </div>
          <div className="flex items-center gap-3">
            <button
              onClick={() => setZoom(zoom - step)}
              disabled={zoom <= min}
              className="p-1 rounded border border-border text-text-muted hover:text-text hover:border-text-muted/50 disabled:opacity-30 transition-colors"
            >
              <Minus className="h-3 w-3" />
            </button>
            <input
              type="range"
              min={min}
              max={max}
              step={step}
              value={zoom}
              onChange={(e) => setZoom(Number(e.target.value))}
              className="zoom-slider flex-1 h-1 appearance-none bg-border rounded-full cursor-pointer"
            />
            <button
              onClick={() => setZoom(zoom + step)}
              disabled={zoom >= max}
              className="p-1 rounded border border-border text-text-muted hover:text-text hover:border-text-muted/50 disabled:opacity-30 transition-colors"
            >
              <Plus className="h-3 w-3" />
            </button>
            <span className="text-xs tabular-nums text-text-secondary w-10 text-right">{zoom}%</span>
          </div>
        </div>
        {/* Latency thresholds */}
        <div className="rounded border border-border bg-surface p-4 space-y-3">
          <div className="flex items-center justify-between">
            <div>
              <h3 className="text-xs font-medium text-text">latency thresholds</h3>
              <p className="text-[10px] text-text-muted mt-0.5">
                compare page colors (seconds)
              </p>
            </div>
            {(thresholds.warn !== defaults.warn || thresholds.error !== defaults.error) && (
              <button
                onClick={resetThresholds}
                className="flex items-center gap-1 text-[10px] text-text-muted hover:text-accent transition-colors"
              >
                <RotateCcw className="h-2.5 w-2.5" /> reset
              </button>
            )}
          </div>
          <div className="flex items-center gap-1.5 text-xs flex-wrap">
            <span className="text-accent font-medium">green</span>
            <span className="text-text-muted">&lt;</span>
            <input
              type="text"
              inputMode="numeric"
              value={warnInput}
              onChange={(e) => setWarnInput(e.target.value)}
              onBlur={() => commitThresholds(warnInput, errorInput)}
              onKeyDown={(e) => { if (e.key === "Enter") (e.target as HTMLInputElement).blur(); }}
              className="w-8 rounded border border-border bg-bg px-1 py-0.5 text-center text-xs text-text tabular-nums focus:border-accent focus:outline-none"
            />
            <span className="text-text-muted">&lt;</span>
            <span className="text-yellow font-medium">yellow</span>
            <span className="text-text-muted">&lt;</span>
            <input
              type="text"
              inputMode="numeric"
              value={errorInput}
              onChange={(e) => setErrorInput(e.target.value)}
              onBlur={() => commitThresholds(warnInput, errorInput)}
              onKeyDown={(e) => { if (e.key === "Enter") (e.target as HTMLInputElement).blur(); }}
              className="w-8 rounded border border-border bg-bg px-1 py-0.5 text-center text-xs text-text tabular-nums focus:border-accent focus:outline-none"
            />
            <span className="text-text-muted">&lt;</span>
            <span className="text-red font-medium">red</span>
          </div>
          <div className={`flex items-center gap-1 text-[10px] text-accent transition-opacity ${thresholdSaved ? "opacity-100" : "opacity-0"}`}>
            <Check className="h-2.5 w-2.5" /> saved
          </div>
        </div>
      </div>

      {/* Font */}
      <div className="rounded border border-border bg-surface p-4 space-y-3">
        <div className="flex items-center justify-between">
          <div>
            <h3 className="text-xs font-medium text-text">font family</h3>
            <p className="text-[10px] text-text-muted mt-0.5">
              applied across the entire dashboard
            </p>
          </div>
          {font !== "jetbrains-mono" && (
            <button
              onClick={resetFont}
              className="flex items-center gap-1 text-[10px] text-text-muted hover:text-accent transition-colors"
            >
              <RotateCcw className="h-2.5 w-2.5" /> reset
            </button>
          )}
        </div>

        <div className="grid grid-cols-2 gap-2">
          {FONT_OPTIONS.map((option) => (
            <button
              key={option.id}
              onClick={() => setFont(option.id as FontId)}
              className={`rounded border px-3 py-2.5 text-left transition-colors ${
                font === option.id
                  ? "border-accent bg-accent/5 text-accent"
                  : "border-border hover:border-text-muted/50 text-text-secondary hover:text-text"
              }`}
            >
              <div className="text-xs font-medium" style={{ fontFamily: option.family }}>
                {option.label}
              </div>
              <div className="text-[10px] mt-1 text-text-muted" style={{ fontFamily: option.family }}>
                The quick brown fox jumps over the lazy dog
              </div>
            </button>
          ))}
        </div>
      </div>

    </div>
  );
}
