// The single uPlot wrapper for the whole app; Sparkline.tsx is a preset on
// top of it rather than a second mount point.
//
// Three constraints shape this file:
//  1. Canvas cannot resolve `var(--token)`, so series/axis colors are CSS
//     custom property names resolved via getComputedStyle at mount, and the
//     effect re-runs on theme change.
//  2. uPlot has no stacked-area mode: plot cumulative sums per layer, then
//     declare `bands` painting between consecutive lines, top-down.
//  3. The hover readout is a React tooltip snapped to the bucket index, so
//     it re-renders per bucket rather than per mousemove. Cursors are synced
//     across non-compact charts, and the tooltip follows that sync; uPlot
//     publishes pointer-leave as an off-plot position, clearing all of them.

import { Fragment, useEffect, useRef, useState } from "react";
import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";
import { useTheme } from "../lib/theme";
import { formatBucketLabel, formatBucketSize, formatBytes } from "../lib/format";

export interface ChartSeries {
  label: string;
  /** CSS custom property name, e.g. "--color-uplink" (not a resolved color). */
  colorVar: string;
  points: number[];
  /** Area fill under the line at ~15% opacity. Ignored (implied) when stacked. */
  fill?: boolean;
}

interface ChartProps {
  timestamps: number[];
  series: ChartSeries[];
  height?: number;
  stacked?: boolean;
  valueFormatter?: (v: number) => string;
  /** Sparkline mode: no axes, no legend, no cursor. */
  compact?: boolean;
  /** How the legend condenses a series: "sum" for deltas, "peak" for gauges. */
  summary?: "sum" | "peak";
  showLegend?: boolean;
  /** Constrains y ticks to whole numbers. */
  integerAxis?: boolean;
  className?: string;
}

// Drag-to-zoom is off; the 30s auto-refresh rebuilds the plot and drops it.
// Spelled out in full because uPlot merges `cursor` shallowly, and a partial
// `drag` would drop `click`, which it still invokes after press-move-release.
const NO_DRAG: uPlot.Cursor.Drag = {
  x: false,
  y: false,
  setScale: false,
  dist: 0,
  uni: undefined,
  click: (_self, e) => {
    e.stopPropagation();
  },
};

/** 1/2/5 x 10^n, the y-axis increments offered when integerAxis is set. */
const INTEGER_INCRS: number[] = Array.from({ length: 7 }, (_, exp) => [1, 2, 5].map((m) => m * 10 ** exp))
  .flat()
  .sort((a, b) => a - b);

interface SeriesMeta {
  label: string;
  color: string;
  summary: string;
}

/** Snapped cursor position, in CSS px relative to the component's own box. */
interface Hover {
  idx: number;
  left: number;
  top: number;
  /** Cursor past the midpoint: draw the tooltip to its left instead. */
  flip: boolean;
}

function resolveColor(varName: string): string {
  const value = getComputedStyle(document.documentElement).getPropertyValue(varName).trim();
  return value || "#888888";
}

function withAlpha(color: string, alphaHex: string): string {
  return `${color}${alphaHex}`;
}

/** Elementwise running sums: cum[k] = points[0] + ... + points[k]. */
function cumulativeLayers(series: ChartSeries[], length: number): number[][] {
  const cum: number[][] = [];
  let running = new Array<number>(length).fill(0);
  for (const s of series) {
    running = running.map((v, i) => v + (s.points[i] ?? 0));
    cum.push(running.slice());
  }
  return cum;
}

function summarize(points: number[], mode: "sum" | "peak"): number {
  if (mode === "peak") {
    let peak = 0;
    for (const v of points) peak = Math.max(peak, v ?? 0);
    return peak;
  }
  let sum = 0;
  for (const v of points) sum += v ?? 0;
  return sum;
}

/** Sizes the y-axis gutter to the widest tick label. */
function axisSizeFor(values: string[] | null): number {
  const longest = (values ?? []).reduce<number>((max, v) => Math.max(max, String(v).length), 0);
  return Math.max(40, Math.min(88, longest * 7 + 16));
}

export function Chart({
  timestamps,
  series,
  height = 220,
  stacked = false,
  valueFormatter = formatBytes,
  compact = false,
  summary = "sum",
  showLegend = true,
  integerAxis = false,
  className,
}: ChartProps) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const plotRef = useRef<HTMLDivElement | null>(null);
  const { theme } = useTheme();
  const [meta, setMeta] = useState<SeriesMeta[]>([]);
  const [hover, setHover] = useState<Hover | null>(null);
  const hoverRef = useRef<Hover | null>(null);

  const step = timestamps.length > 1 ? (timestamps[1] ?? 0) - (timestamps[0] ?? 0) : 0;

  useEffect(() => {
    const host = hostRef.current;
    const el = plotRef.current;
    if (!host || !el) return;

    const borderColor = resolveColor("--color-border");
    const axisColor = resolveColor("--color-text-faint");

    const dataSeries = stacked ? cumulativeLayers(series, timestamps.length) : series.map((s) => s.points);
    const data = [timestamps, ...dataSeries] as uPlot.AlignedData;

    const bands: uPlot.Band[] = [];
    if (stacked) {
      for (let k = series.length - 1; k >= 1; k--) {
        bands.push({ series: [k + 1, k], fill: withAlpha(resolveColor(series[k]!.colorVar), "26") });
      }
    }

    const seriesOpts: uPlot.Series[] = [
      { label: "time" },
      ...series.map((s, i): uPlot.Series => {
        const color = resolveColor(s.colorVar);
        const isBottomStackedLayer = stacked && i === 0;
        return {
          label: s.label,
          stroke: color,
          width: 1.5,
          fill: stacked ? (isBottomStackedLayer ? withAlpha(color, "26") : undefined) : s.fill ? withAlpha(color, "26") : undefined,
          points: { show: false },
        };
      }),
    ];

    const clearHover = () => {
      if (hoverRef.current === null) return;
      hoverRef.current = null;
      setHover(null);
    };

    const opts: uPlot.Options = {
      width: el.clientWidth || 300,
      height,
      padding: compact ? [2, 2, 2, 2] : [12, 8, 0, 0],
      cursor: compact
        ? { show: false }
        : { y: false, drag: NO_DRAG, sync: { key: "orrery-dashboard", scales: ["x", null] } },
      legend: { show: false },
      scales: { x: { time: true } },
      bands: stacked ? bands : undefined,
      axes: compact
        ? [{ show: false }, { show: false }]
        : [
            {
              stroke: axisColor,
              grid: { stroke: borderColor, width: 1 },
              ticks: { stroke: borderColor, width: 1 },
            },
            {
              stroke: axisColor,
              grid: { stroke: borderColor, width: 1 },
              ticks: { show: false },
              size: (_u, values) => axisSizeFor(values),
              incrs: integerAxis ? INTEGER_INCRS : undefined,
              values: (_u, vals) => vals.map((v) => valueFormatter(v)),
            },
          ],
      series: seriesOpts,
      hooks: compact
        ? undefined
        : {
            setCursor: [
              (u) => {
                const idx = u.cursor.idx;
                if (idx == null) {
                  clearHover();
                  return;
                }
                if (hoverRef.current?.idx === idx) return;

                const x = u.valToPos(u.data[0]![idx] as number, "x");
                const overRect = u.over.getBoundingClientRect();
                const hostRect = host.getBoundingClientRect();
                const next: Hover = {
                  idx,
                  left: overRect.left - hostRect.left + x,
                  top: overRect.top - hostRect.top + 6,
                  flip: x > u.over.clientWidth / 2,
                };
                hoverRef.current = next;
                setHover(next);
              },
            ],
          },
    };

    setMeta(
      compact
        ? []
        : series.map((s) => {
            const value = valueFormatter(summarize(s.points, summary));
            return {
              label: s.label,
              color: resolveColor(s.colorVar),
              summary: summary === "peak" ? `peak ${value}` : value,
            };
          }),
    );

    const plot = new uPlot(opts, data, el);

    const resizeObserver = new ResizeObserver((entries) => {
      const width = entries[0]?.contentRect.width;
      if (!width || width <= 0) return;

      plot.setSize({ width, height });
      // setSize fires no setCursor, so the tooltip would keep stale coordinates.
      clearHover();
    });
    resizeObserver.observe(el);

    return () => {
      resizeObserver.disconnect();
      plot.destroy();
      clearHover();
    };
    // theme is a dependency so canvas colors (resolved from CSS vars) refresh on toggle
  }, [timestamps, series, stacked, height, valueFormatter, compact, summary, integerAxis, theme]);

  const hoveredTs = hover ? timestamps[hover.idx] : undefined;
  const tooltip =
    !compact && hover && hoveredTs !== undefined
      ? (() => {
          const idx = hover.idx;
          const rows = series.map((s, i) => ({
            label: s.label,
            color: meta[i]?.color ?? "#888888",
            value: s.points[idx] ?? 0,
          }));
          // Stacked layers paint bottom-up; list top-down to match the bands.
          return { ts: hoveredTs, left: hover.left, top: hover.top, flip: hover.flip, rows: stacked ? rows.reverse() : rows };
        })()
      : null;

  const tooltipTotal = tooltip ? tooltip.rows.reduce((sum, r) => sum + r.value, 0) : 0;

  return (
    <div className={`relative ${className ?? ""}`} style={{ width: "100%" }} ref={hostRef}>
      <div ref={plotRef} style={{ height, width: "100%" }} />

      {tooltip && (
        <div
          className="pointer-events-none absolute z-20"
          style={{
            left: tooltip.left + (tooltip.flip ? -10 : 10),
            top: tooltip.top,
            transform: tooltip.flip ? "translateX(-100%)" : undefined,
          }}
        >
          {/* max-w keeps the box inside a phone viewport; labels truncate. */}
          <div className="min-w-36 max-w-[min(15rem,calc(100vw-2.5rem))] rounded-lg border border-border bg-surface-raised/95 px-2.5 py-2 shadow-lg">
            <div className="mb-1.5 flex items-baseline gap-1.5 text-[0.7rem] whitespace-nowrap">
              <span className="font-medium text-text">{formatBucketLabel(tooltip.ts, step)}</span>
              {step > 0 && <span className="text-text-faint">{formatBucketSize(step)}</span>}
            </div>
            <div className="grid grid-cols-[auto_1fr_auto] items-center gap-x-2 gap-y-0.5 text-xs">
              {tooltip.rows.map((r) => (
                <Fragment key={r.label}>
                  <span className="h-2 w-2 shrink-0 rounded-sm" style={{ backgroundColor: r.color }} aria-hidden />
                  <span className="truncate text-text-muted">{r.label}</span>
                  <span className="tabular-nums text-text">{valueFormatter(r.value)}</span>
                </Fragment>
              ))}
            </div>
            {tooltip.rows.length > 1 && (
              <div className="mt-1 flex items-baseline justify-between gap-4 border-t border-border/70 pt-1 text-xs">
                <span className="text-text-muted">Total</span>
                <span className="tabular-nums text-text">{valueFormatter(tooltipTotal)}</span>
              </div>
            )}
          </div>
        </div>
      )}

      {!compact && showLegend && meta.length > 0 && (
        <ul className="mt-2 flex flex-wrap gap-x-3 gap-y-1.5 text-xs text-text-muted">
          {meta.map((item) => (
            <li key={item.label} className="inline-flex items-center gap-1.5 tabular-nums">
              <span
                className="inline-block h-2 w-2 shrink-0 rounded-sm"
                style={{ backgroundColor: item.color }}
                aria-hidden
              />
              <span className="text-text">{item.label}</span>
              <span className="text-text-faint">{item.summary}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
