import { useZoom } from "../lib/useZoom";
import { useFont, FONT_OPTIONS, type FontId } from "../lib/useFont";
import { useTheme, type Theme } from "../lib/useTheme";
import { Minus, Plus, RotateCcw, Moon, Sun } from "lucide-react";

export default function SettingsPage() {
  const { zoom, setZoom, reset: resetZoom, min, max, step } = useZoom();
  const { font, setFont, reset: resetFont } = useFont();
  const { theme, setTheme } = useTheme();

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

      {/* Theme */}
      <div className="rounded border border-border bg-surface p-4 space-y-3">
        <div>
          <h3 className="text-xs font-medium text-text">theme</h3>
          <p className="text-[10px] text-text-muted mt-0.5">
            switch between dark and light mode
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

      {/* Zoom */}
      <div className="rounded border border-border bg-surface p-4 space-y-3">
        <div className="flex items-center justify-between">
          <div>
            <h3 className="text-xs font-medium text-text">zoom level</h3>
            <p className="text-[10px] text-text-muted mt-0.5">
              scales the entire UI — also accessible from the sidebar
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
    </div>
  );
}
