// Persists the last RangePicker choice so Overview / Node / Users share a
// session default instead of each page hard-coding its own.

import { useCallback, useState } from "react";
import type { Range } from "../api/types";
import { RANGE_OPTIONS } from "./range";

/** Shared by Overview + Node detail (ops "live" windows). */
export const RANGE_KEY_LIVE = "orrery.range.live";
/** Shared by Users + User detail (longer attribution windows). */
export const RANGE_KEY_USERS = "orrery.range.users";

function readStored(key: string, fallback: Range): Range {
  try {
    const stored = localStorage.getItem(key);
    if (stored && (RANGE_OPTIONS as string[]).includes(stored)) return stored as Range;
  } catch {
    // private mode / blocked storage
  }
  return fallback;
}

export function useStoredRange(fallback: Range, storageKey = RANGE_KEY_LIVE): [Range, (range: Range) => void] {
  const [range, setRangeState] = useState<Range>(() => readStored(storageKey, fallback));

  const setRange = useCallback(
    (next: Range) => {
      setRangeState(next);
      try {
        localStorage.setItem(storageKey, next);
      } catch {
        // ignore
      }
    },
    [storageKey],
  );

  return [range, setRange];
}
