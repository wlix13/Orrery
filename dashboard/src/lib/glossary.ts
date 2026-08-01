import type { CollectLevel, NodeStatus } from "../api/types";
import type { IPScope } from "./ip";

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

export const IP_SCOPE_LABEL: Record<IPScope, string> = {
  private: "private",
  cgnat: "cgnat",
  "link-local": "link-local",
  loopback: "loopback",
};

/** Why a non-routable address shows up as an online IP. */
export const IP_SCOPE_HELP: Record<IPScope, string> = {
  private:
    "Not the client's public IP. WireGuard inbounds report the peer's tunnel address, which is fixed per peer.",
  cgnat: "Carrier-grade NAT range - a tunnel address or an upstream NAT, not a routable client IP.",
  "link-local": "Link-local address - the client's source never left the node's own link.",
  loopback: "Loopback - traffic reached the inbound through something local to the node.",
};
