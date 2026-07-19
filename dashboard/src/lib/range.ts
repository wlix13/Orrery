// Maps a RangePicker selection to /api/series `from`/`to`/`step` params.
// Step is chosen to keep series around 200-400 points:
//   1h -> 60s, 6h -> 60s, 24h -> 300s, 7d -> 3600s, 30d -> 3600s, 90d -> 86400s.

import type { Range } from "../api/types";

export const RANGE_OPTIONS: Range[] = ["1h", "6h", "24h", "7d", "30d", "90d"];

const RANGE_SECONDS: Record<Range, number> = {
  "1h": 3600,
  "6h": 6 * 3600,
  "24h": 24 * 3600,
  "7d": 7 * 86400,
  "30d": 30 * 86400,
  "90d": 90 * 86400,
};

const RANGE_STEP_SECONDS: Record<Range, number> = {
  "1h": 60,
  "6h": 60,
  "24h": 300,
  "7d": 3600,
  "30d": 3600,
  "90d": 86400,
};

export function rangeSeconds(range: Range): number {
  return RANGE_SECONDS[range];
}

export function stepForRange(range: Range): number {
  return RANGE_STEP_SECONDS[range];
}

export interface RangeWindow {
  from: number;
  to: number;
  step: number;
}

/** Computes the current [from, to, step] window for a range, anchored to now. */
export function windowForRange(range: Range, nowSeconds = Math.floor(Date.now() / 1000)): RangeWindow {
  const step = stepForRange(range);
  const to = Math.floor(nowSeconds / step) * step;
  const from = to - rangeSeconds(range);
  return { from, to, step };
}

export function rangeLabel(range: Range): string {
  switch (range) {
    case "1h":
      return "1 hour";
    case "6h":
      return "6 hours";
    case "24h":
      return "24 hours";
    case "7d":
      return "7 days";
    case "30d":
      return "30 days";
    case "90d":
      return "90 days";
  }
}
