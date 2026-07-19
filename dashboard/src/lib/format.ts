// Formatting helpers: bytes, rates, relative time, durations.
// All numeric/byte table columns use these so figures are consistent
// and render with tabular-nums (see styles.css).

const BYTE_UNITS = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"] as const;

/** Human-formatted byte count, e.g. "1.2 GiB". 1 decimal place, base-1024. */
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes)) return "—";
  const sign = bytes < 0 ? "-" : "";
  let value = Math.abs(bytes);
  let unitIndex = 0;
  while (value >= 1024 && unitIndex < BYTE_UNITS.length - 1) {
    value /= 1024;
    unitIndex += 1;
  }
  const decimals = unitIndex === 0 ? 0 : 1;
  return `${sign}${value.toFixed(decimals)} ${BYTE_UNITS[unitIndex]}`;
}

/** Bytes-per-second formatted as a rate, e.g. "3.4 MiB/s". */
export function formatRate(bytesPerSecond: number): string {
  return `${formatBytes(bytesPerSecond)}/s`;
}

/** Compact relative time, e.g. "3m ago", "2h ago", "just now". */
export function formatRelativeTime(unixSeconds: number | null | undefined, now = Date.now()): string {
  if (unixSeconds === null || unixSeconds === undefined) return "never";
  const deltaS = Math.round(now / 1000 - unixSeconds);
  if (deltaS < 5) return "just now";
  if (deltaS < 60) return `${deltaS}s ago`;
  const deltaM = Math.floor(deltaS / 60);
  if (deltaM < 60) return `${deltaM}m ago`;
  const deltaH = Math.floor(deltaM / 60);
  if (deltaH < 24) return `${deltaH}h ago`;
  const deltaD = Math.floor(deltaH / 24);
  return `${deltaD}d ago`;
}

/** Duration in seconds formatted compactly, e.g. "3d 4h", "12m", "45s". */
export function formatDuration(totalSeconds: number): string {
  if (!Number.isFinite(totalSeconds) || totalSeconds < 0) return "—";
  const s = Math.floor(totalSeconds);
  const days = Math.floor(s / 86400);
  const hours = Math.floor((s % 86400) / 3600);
  const minutes = Math.floor((s % 3600) / 60);
  const seconds = s % 60;

  if (days > 0) return `${days}d ${hours}h`;
  if (hours > 0) return `${hours}h ${minutes}m`;
  if (minutes > 0) return `${minutes}m ${seconds}s`;
  return `${seconds}s`;
}

/** Formats a plain count with locale thousands separators. */
export function formatCount(n: number): string {
  return n.toLocaleString("en-US");
}

/** Unix seconds -> absolute local timestamp for tooltips/legends. */
export function formatTimestamp(unixSeconds: number): string {
  return new Date(unixSeconds * 1000).toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/** Chart step width, e.g. 60 -> "1m", 3600 -> "1h", 86400 -> "1d". */
export function formatBucketSize(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return "";
  if (seconds % 86400 === 0) return `${seconds / 86400}d`;
  if (seconds % 3600 === 0) return `${seconds / 3600}h`;
  if (seconds % 60 === 0) return `${seconds / 60}m`;
  return `${seconds}s`;
}

/** Start of a chart bucket; day-wide buckets drop the clock. */
export function formatBucketLabel(unixSeconds: number, stepSeconds: number): string {
  if (stepSeconds >= 86400) {
    return new Date(unixSeconds * 1000).toLocaleDateString(undefined, { month: "short", day: "numeric" });
  }
  return formatTimestamp(unixSeconds);
}
