import { Minus, Plus } from "lucide-react";

interface ZoomSliderProps {
  zoom: number;
  min: number;
  max: number;
  step: number;
  onChange: (value: number) => void;
  onReset: () => void;
}

export default function ZoomSlider({ zoom, min, max, step, onChange, onReset }: ZoomSliderProps) {
  return (
    <div className="shrink-0 px-3 py-2 border-t border-border">
      <div className="flex items-center gap-1">
        <button
          onClick={() => onChange(zoom - step)}
          disabled={zoom <= min}
          className="shrink-0 p-0.5 text-text-muted/60 hover:text-text disabled:opacity-30 transition-colors"
          aria-label="zoom out"
        >
          <Minus className="h-2.5 w-2.5" />
        </button>

        <input
          type="range"
          min={min}
          max={max}
          step={step}
          value={zoom}
          onChange={(e) => onChange(Number(e.target.value))}
          className="zoom-slider flex-1 min-w-0 h-0.5 appearance-none bg-border rounded-full cursor-pointer"
          aria-label="zoom level"
        />

        <button
          onClick={() => onChange(zoom + step)}
          disabled={zoom >= max}
          className="shrink-0 p-0.5 text-text-muted/60 hover:text-text disabled:opacity-30 transition-colors"
          aria-label="zoom in"
        >
          <Plus className="h-2.5 w-2.5" />
        </button>

        <button
          onClick={onReset}
          className="shrink-0 ml-0.5 text-[9px] tabular-nums text-text-muted/50 hover:text-accent transition-colors"
          title="reset zoom"
        >
          {zoom}%
        </button>
      </div>
    </div>
  );
}
