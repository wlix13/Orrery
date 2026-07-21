// Small data-fetching hook shared by every page: tracks loading/error state
// and re-fetches when `deps` change or `refresh()` is called. Loading only
// flips true on the very first fetch - later refreshes keep last-good data
// on screen and surface `refreshing` instead of flashing a skeleton.

import { useCallback, useEffect, useRef, useState } from "react";

export interface AsyncState<T> {
  data: T | null;
  loading: boolean;
  /** True while a non-initial fetch is in flight (range change, auto-refresh). */
  refreshing: boolean;
  error: string | null;
  refresh: () => void;
}

export function useApiData<T>(fetcher: () => Promise<T>, deps: unknown[]): AsyncState<T> {
  const [data, setData] = useState<T | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [nonce, setNonce] = useState(0);
  const hasLoadedRef = useRef(false);
  const fetcherRef = useRef(fetcher);
  fetcherRef.current = fetcher;

  useEffect(() => {
    let cancelled = false;
    if (!hasLoadedRef.current) {
      setLoading(true);
    } else {
      setRefreshing(true);
    }

    fetcherRef
      .current()
      .then((result) => {
        if (cancelled) return;
        hasLoadedRef.current = true;
        setData(result);
        setError(null);
        setLoading(false);
        setRefreshing(false);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : "Request failed");
        setLoading(false);
        setRefreshing(false);
      });

    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, nonce]);

  const refresh = useCallback(() => setNonce((n) => n + 1), []);

  return { data, loading, refreshing, error, refresh };
}
