import { formatRelativeTime } from "../lib/format";

interface RefreshControlsProps {
  /** Unix seconds of last successful payload, if known. */
  updatedAt?: number | null;
  refreshing?: boolean;
  onRefresh: () => void;
  className?: string;
}

/** Last-updated caption + manual refresh, shared by page headers. */
export function RefreshControls({ updatedAt, refreshing, onRefresh, className }: RefreshControlsProps) {
  const caption = refreshing ? "Updating…" : updatedAt != null ? `Updated ${formatRelativeTime(updatedAt)}` : null;

  return (
    <div className={`flex items-center gap-2 ${className ?? ""}`}>
      {caption !== null && <span className="font-mono text-xs text-text-muted">{caption}</span>}
      <button
        type="button"
        onClick={onRefresh}
        disabled={refreshing}
        className="rounded-md border border-border/80 px-2 py-1 text-xs text-text-muted transition-colors hover:border-accent/40 hover:text-accent disabled:opacity-50"
      >
        Refresh
      </button>
    </div>
  );
}
