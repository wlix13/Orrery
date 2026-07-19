import type { Range } from "../api/types";
import { RANGE_OPTIONS, rangeLabel } from "../lib/range";

interface RangePickerProps {
  value: Range;
  onChange: (range: Range) => void;
  className?: string;
}

export function RangePicker({ value, onChange, className }: RangePickerProps) {
  return (
    <div
      className={`inline-flex items-center gap-0.5 rounded-lg border border-border/80 bg-surface/90 p-0.5 ${className ?? ""}`}
      role="group"
      aria-label="Time range"
    >
      {RANGE_OPTIONS.map((range) => (
        <button
          key={range}
          type="button"
          onClick={() => onChange(range)}
          title={rangeLabel(range)}
          aria-label={rangeLabel(range)}
          className={`rounded-md px-2.5 py-1.5 font-mono text-[0.7rem] font-medium transition-colors ${
            range === value
              ? "bg-accent text-white"
              : "text-text-muted hover:bg-surface-raised hover:text-text"
          }`}
          aria-pressed={range === value}
        >
          {range}
        </button>
      ))}
    </div>
  );
}
