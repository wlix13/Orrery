import type { CollectLevel, NodeStatus } from "../api/types";

export const STATUS_LABEL: Record<NodeStatus, string> = {
  up: "Up",
  stale: "Stale",
  down: "Down",
  off: "Off",
};

/** One-line ops glossary for status dots / tooltips. */
export const STATUS_HELP: Record<NodeStatus, string> = {
  up: "Last poll succeeded recently.",
  stale: "Missed recent poll(s) - node may be slow or intermittently unreachable.",
  down: "Unreachable or consecutive poll failures.",
  off: "Collection disabled (collect: off) - calm state, not an outage.",
};

export const COLLECT_LABEL: Record<CollectLevel, string> = {
  full: "full",
  traffic: "traffic",
  off: "off",
};

export const COLLECT_HELP: Record<CollectLevel, string> = {
  full: "Tags + per-user traffic + online users + sys stats.",
  traffic: "Inbound/outbound tags + sys stats only (no online-user RPC).",
  off: "Node is registered but never polled.",
};
