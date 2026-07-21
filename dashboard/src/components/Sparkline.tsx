// Thin preset over Chart for compact inline trend lines (tables, stat cards).

import { useMemo } from "react";
import { Chart, type ChartSeries } from "./Chart";
import { formatBytes } from "../lib/format";

interface SparklineProps {
  timestamps: number[];
  points: number[];
  colorVar?: string;
  height?: number;
  className?: string;
  valueFormatter?: (v: number) => string;
}

export function Sparkline({
  timestamps,
  points,
  colorVar = "--color-accent",
  height = 32,
  className,
  valueFormatter = formatBytes,
}: SparklineProps) {
  // Memoised: Chart keys its uPlot instance off the series array identity.
  const series = useMemo<ChartSeries[]>(
    () => [{ label: "value", colorVar, points, fill: true }],
    [colorVar, points],
  );

  return (
    <Chart
      timestamps={timestamps}
      series={series}
      height={height}
      compact
      valueFormatter={valueFormatter}
      className={className}
    />
  );
}
