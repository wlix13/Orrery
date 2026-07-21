// TypeScript mirror of the collector's HTTP API contract.
// Field names match the Go server's JSON exactly - do not rename.

export type Range = "1h" | "6h" | "24h" | "7d" | "30d" | "90d";
/** Lookback for hubs-seen (presence/activity), independent of traffic range. */
export type SeenWindow = "1h" | "6h" | "24h";

export type NodeStatus = "up" | "stale" | "down" | "off";
export type NodeType = "hub" | "exit";
export type CollectLevel = "full" | "traffic" | "off";
export type Direction = "up" | "down";
export type SeriesKind = "inbound" | "outbound" | "user" | "online";
export type SeriesAgg = "none" | "entity" | "node" | "total";

/** `fleet/id`, used as the `node` path/query param everywhere. */
export type NodeKey = string;

export interface ApiError {
  error: {
    code: string;
    message: string;
  };
}

export class ApiRequestError extends Error {
  readonly code: string;
  readonly status: number;

  constructor(code: string, message: string, status: number) {
    super(message);
    this.name = "ApiRequestError";
    this.code = code;
    this.status = status;
  }
}

// GET /api/me
export interface Me {
  name: string;
  method: "token" | "cloudflare_access" | "anonymous";
  /** null means every fleet. */
  fleets: string[] | null;
}

// GET /api/overview?range=24h
export interface FleetSummary {
  fleet: string;
  nodes_up: number;
  nodes_total: number;
  up_bytes: number;
  down_bytes: number;
}

export interface TopUser {
  email: string;
  up_bytes: number;
  down_bytes: number;
}

export interface TopNode {
  node: NodeKey;
  up_bytes: number;
  down_bytes: number;
}

export interface Overview {
  generated_at: number;
  nodes: {
    total: number;
    up: number;
    stale: number;
    down: number;
    off: number; // intentionally disabled (collect: off) - not an alarm
  };
  online_users: number;
  totals: {
    up_bytes: number;
    down_bytes: number;
  };
  fleets: FleetSummary[];
  top_users: TopUser[]; // <=10, hubs only
  top_nodes: TopNode[]; // <=10, inbound traffic
}

// GET /api/nodes
export interface NodeRow {
  node: NodeKey;
  fleet: string;
  id: string;
  region: string;
  type: NodeType;
  hostname: string;
  status: NodeStatus;
  last_ok: number | null;
  last_err: string | null;
  collect: CollectLevel;
  uptime_s: number;
  num_goroutine: number;
  alloc_bytes: number;
  sys_bytes: number;
  num_gc: number;
}

// GET /api/nodes/{node}?range=24h
export interface EntityTraffic {
  entity: string;
  up_bytes: number;
  down_bytes: number;
}

export interface UserTraffic {
  email: string;
  up_bytes: number;
  down_bytes: number;
}

export interface NodeDetail extends NodeRow {
  inbounds: EntityTraffic[];
  outbounds: EntityTraffic[];
  users: UserTraffic[];
}

// GET /api/series
export interface SeriesQuery {
  from: number;
  to: number;
  step: number;
  kind: SeriesKind;
  node?: NodeKey;
  fleet?: string;
  type?: NodeType;
  entity?: string;
  dir?: Direction;
  agg?: SeriesAgg;
}

export interface SeriesLine {
  node?: NodeKey;
  entity?: string;
  dir?: Direction;
  points: number[]; // dense, aligned to from + i*step; null gaps encoded as 0 by server? see lib/format
}

export interface SeriesResponse {
  from: number;
  to: number;
  step: number;
  series: SeriesLine[];
}

// GET /api/users?range=30d&seen=6h&fleet=
export interface UserHubSeen {
  node: NodeKey;
  last_seen: number; // unix seconds
}

export interface UserRow {
  email: string;
  up_bytes: number;
  down_bytes: number;
  online_now: boolean;
  hubs: UserHubSeen[];
}

// GET /api/users/{email}?range=30d&seen=6h
export interface UserNodeBreakdown {
  node: NodeKey;
  up_bytes: number;
  down_bytes: number;
}

export interface OnlineIp {
  ip: string;
  last_seen: number;
}

export interface UserDetail {
  email: string;
  up_bytes: number;
  down_bytes: number;
  online_now: boolean;
  nodes: UserNodeBreakdown[];
  seen_hubs: UserHubSeen[];
  ips: OnlineIp[];
}

// GET /api/online
export interface OnlineNodeUsers {
  node: NodeKey;
  email: string;
  ips: OnlineIp[];
}
