// Fires `onTick` on an interval, skipping ticks while the tab is hidden and
// firing once immediately when the tab becomes visible again (so data isn't
// stale right after switching back). Used across pages for the 30s
// auto-refresh requirement.

import { useEffect, useRef } from "react";

export function useAutoRefresh(onTick: () => void, intervalMs: number, enabled = true): void {
  const onTickRef = useRef(onTick);
  onTickRef.current = onTick;

  useEffect(() => {
    if (!enabled) return;

    const id = window.setInterval(() => {
      if (!document.hidden) onTickRef.current();
    }, intervalMs);

    const onVisibility = () => {
      if (!document.hidden) onTickRef.current();
    };
    document.addEventListener("visibilitychange", onVisibility);

    return () => {
      window.clearInterval(id);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [intervalMs, enabled]);
}
