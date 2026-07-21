import type { NodeStatus } from "../api/types";
import { STATUS_HELP, STATUS_LABEL } from "../lib/glossary";

const STATUS_COLOR_CLASS: Record<NodeStatus, string> = {
  up: "bg-up",
  stale: "bg-stale",
  down: "bg-down",
  off: "bg-off",
};

interface StatusDotProps {
  status: NodeStatus;
  showLabel?: boolean;
  className?: string;
}

export function StatusDot({ status, showLabel = true, className }: StatusDotProps) {
  const label = STATUS_LABEL[status];
  const help = STATUS_HELP[status];
  return (
    <span
      className={`inline-flex items-center gap-1.5 ${className ?? ""}`}
      title={`${label}: ${help}`}
      aria-label={`${label}: ${help}`}
    >
      <span
        className={`inline-block h-2 w-2 shrink-0 rounded-full ${STATUS_COLOR_CLASS[status]}`}
        aria-hidden
      />
      {showLabel && <span className="text-sm text-text-muted">{label}</span>}
    </span>
  );
}
