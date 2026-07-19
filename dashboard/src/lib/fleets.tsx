// Fleet awareness for display only: node keys stay `fleet/id` in the API and
// router, but single-fleet deployments render them unprefixed.
//
// The list is config, not telemetry: fetched once per session and seeded from
// localStorage so a cold load doesn't paint the prefix and then drop it.

import {
  createContext,
  createElement,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useAuth } from "./auth";
import type { NodeKey } from "../api/types";

const CACHE_KEY = "orrery.fleets";

export interface FleetsState {
  /** Sorted list of configured fleets; empty until the first load resolves. */
  fleets: string[];
  /** The only configured fleet, or null when there are none or several. */
  soleFleet: string | null;
  /** Display form of a `fleet/id` key, unprefixed in single-fleet deployments. */
  nodeLabel: (node: NodeKey) => string;
}

const FleetsContext = createContext<FleetsState | null>(null);

function readCache(scope: string): string[] {
  try {
    const raw = localStorage.getItem(CACHE_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw) as { scope?: string; fleets?: unknown };
    if (parsed.scope !== scope || !Array.isArray(parsed.fleets)) return [];
    return parsed.fleets.filter((f): f is string => typeof f === "string");
  } catch {
    return [];
  }
}

function writeCache(scope: string, fleets: string[]): void {
  try {
    localStorage.setItem(CACHE_KEY, JSON.stringify({ scope, fleets }));
  } catch {
    // storage unavailable; the list still works in-memory
  }
}

function sameList(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((v, i) => v === b[i]);
}

export function FleetsProvider({ children }: { children: ReactNode }) {
  const { client, apiBase, isMock } = useAuth();
  const scope = isMock ? "mock" : apiBase;
  const [fleets, setFleets] = useState<string[]>(() => readCache(scope));
  const loadedScope = useRef(scope);

  useEffect(() => {
    let cancelled = false;

    // A new collector must not keep the previous one's labels, even if the fetch fails.
    if (loadedScope.current !== scope) {
      loadedScope.current = scope;
      setFleets(readCache(scope));
    }

    client
      .getNodes()
      .then((nodes) => {
        if (cancelled) return;
        const next = [...new Set(nodes.map((n) => n.fleet))].sort();
        // Hold the array identity when unchanged: nodeLabel feeds memoised
        // chart series, and a new identity costs a uPlot teardown + rebuild.
        setFleets((prev) => (sameList(prev, next) ? prev : next));
        writeCache(scope, next);
      })
      .catch(() => {
        // Labelling only; pages surface their own load errors.
      });

    return () => {
      cancelled = true;
    };
  }, [client, scope]);

  const value = useMemo<FleetsState>(() => {
    const soleFleet = fleets.length === 1 ? fleets[0]! : null;
    const prefix = soleFleet === null ? null : `${soleFleet}/`;
    return {
      fleets,
      soleFleet,
      nodeLabel: (node) => (prefix !== null && node.startsWith(prefix) ? node.slice(prefix.length) : node),
    };
  }, [fleets]);

  return createElement(FleetsContext.Provider, { value }, children);
}

export function useFleets(): FleetsState {
  const ctx = useContext(FleetsContext);
  if (!ctx) throw new Error("useFleets must be used within a FleetsProvider");
  return ctx;
}
