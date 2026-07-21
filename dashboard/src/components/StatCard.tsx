import type { ReactNode } from "react";

interface StatCardProps {
  label: string;
  value?: ReactNode;
  sub?: ReactNode;
  loading?: boolean;
  className?: string;
}

/** Fixed-height metric tile: open face, mono value, quiet label. */
export function StatCard({ label, value, sub, loading, className }: StatCardProps) {
  return (
    <div
      className={`flex h-[5.5rem] min-w-0 flex-col justify-between overflow-hidden rounded-xl border border-border/80 bg-surface/90 px-4 py-3 ${className ?? ""}`}
    >
      <span className="micro-label">{label}</span>
      {loading ? (
        <div className="skeleton h-7 w-20" />
      ) : (
        <span className="truncate font-mono text-xl font-semibold tracking-tight text-text sm:text-2xl">{value}</span>
      )}
      {sub !== undefined && (
        <span className="truncate text-xs text-text-muted">{loading ? "" : sub}</span>
      )}
    </div>
  );
}
