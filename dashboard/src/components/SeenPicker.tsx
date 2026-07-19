import type { SeenWindow } from "../api/types";

const OPTIONS: SeenWindow[] = ["1h", "6h", "24h"];

interface SeenPickerProps {
  value: SeenWindow;
  onChange: (seen: SeenWindow) => void;
  className?: string;
}

/** Lookback for "hubs seen recently" - independent of the traffic RangePicker. */
export function SeenPicker({ value, onChange, className }: SeenPickerProps) {
  return (
    <div
      className={`inline-flex items-center gap-1.5 ${className ?? ""}`}
      role="group"
      aria-label="Hubs seen within"
    >
      <span className="text-xs text-text-muted">Seen</span>
      <div className="inline-flex items-center gap-0.5 rounded-lg border border-border/80 bg-surface/90 p-0.5">
        {OPTIONS.map((opt) => (
          <button
            key={opt}
            type="button"
            onClick={() => onChange(opt)}
            title={`Hubs with activity in the last ${opt}`}
            aria-label={`Hubs seen in last ${opt}`}
            className={`rounded-md px-2 py-1 font-mono text-[0.7rem] font-medium transition-colors ${
              opt === value
                ? "bg-accent text-white"
                : "text-text-muted hover:bg-surface-raised hover:text-text"
            }`}
            aria-pressed={opt === value}
          >
            {opt}
          </button>
        ))}
      </div>
    </div>
  );
}
