// Dev mock backend: implements OrreryClient with plausible generated data
// (sine-ish daily traffic waves + noise) so the dashboard is fully explorable
// without a running collector. Enabled via VITE_MOCK=1 or token === "mock".
//
// Data is generated deterministically from (node, entity, timestamp) hashes
// rather than stored/mutated, so repeated calls (auto-refresh, navigation)
// stay internally consistent without a fake database.

import type {
  Direction,
  EntityTraffic,
  NodeDetail,
  NodeKey,
  NodeRow,
  NodeStatus,
  NodeType,
  OnlineIp,
  OnlineNodeUsers,
  Overview,
  Range,
  SeenWindow,
  SeriesLine,
  SeriesQuery,
  SeriesResponse,
  UserDetail,
  UserRow,
} from "./types";
import type { OrreryClient } from "./client";
import { windowForRange } from "../lib/range";

// ---------------------------------------------------------------------------
// Deterministic PRNG helpers
// ---------------------------------------------------------------------------

function hashStr(s: string): number {
  let h = 0;
  for (let i = 0; i < s.length; i++) {
    h = (Math.imul(31, h) + s.charCodeAt(i)) | 0;
  }
  return h >>> 0;
}

/** Deterministic pseudo-random float in [0,1) from two integer seeds. */
function pseudoRandom(a: number, b: number): number {
  let seed = (a ^ Math.imul(b, 2654435761)) >>> 0;
  seed = Math.imul(seed ^ (seed >>> 15), 1 | seed);
  seed = (seed + Math.imul(seed ^ (seed >>> 7), 61 | seed)) ^ seed;
  return ((seed ^ (seed >>> 14)) >>> 0) / 4294967296;
}

// ---------------------------------------------------------------------------
// Fixed fleet/node/user fixtures (2 fleets, 8 nodes, 12 users)
// ---------------------------------------------------------------------------

interface MockNode {
  id: string;
  fleet: string;
  type: NodeType;
  region: string;
  status: NodeStatus;
  collect: "full" | "traffic" | "off";
  inbounds: string[];
  outbounds: string[];
}

const ALL_NODES: MockNode[] = [
  { id: "hub01", fleet: "main", type: "hub", region: "eu-west", status: "up", collect: "full", inbounds: ["vless-in", "vmess-in", "trojan-in"], outbounds: ["direct-out", "block-out"] },
  { id: "hub02", fleet: "main", type: "hub", region: "eu-west", status: "up", collect: "full", inbounds: ["vless-in", "vmess-in", "trojan-in"], outbounds: ["direct-out", "block-out"] },
  { id: "exit01", fleet: "main", type: "exit", region: "eu-west", status: "up", collect: "traffic", inbounds: ["relay-in"], outbounds: ["direct-out", "block-out"] },
  { id: "exit02", fleet: "main", type: "exit", region: "eu-central", status: "stale", collect: "traffic", inbounds: ["relay-in"], outbounds: ["direct-out", "block-out"] },
  { id: "hub01", fleet: "secondary", type: "hub", region: "us-east", status: "up", collect: "full", inbounds: ["vless-in", "vmess-in", "trojan-in"], outbounds: ["direct-out", "block-out"] },
  { id: "hub02", fleet: "secondary", type: "hub", region: "us-west", status: "up", collect: "full", inbounds: ["vless-in", "vmess-in", "trojan-in"], outbounds: ["direct-out", "block-out"] },
  { id: "exit01", fleet: "secondary", type: "exit", region: "ap-south", status: "down", collect: "traffic", inbounds: ["relay-in"], outbounds: ["direct-out", "block-out"] },
  { id: "exit02", fleet: "secondary", type: "exit", region: "ap-east", status: "off", collect: "off", inbounds: [], outbounds: [] },
];
// VITE_MOCK_SINGLE_FLEET=1 keeps only "main", to exercise the single-fleet UI.
const NODES: MockNode[] =
  import.meta.env.VITE_MOCK_SINGLE_FLEET === "1" ? ALL_NODES.filter((n) => n.fleet === "main") : ALL_NODES;

function nodeKey(n: MockNode): NodeKey {
  return `${n.fleet}/${n.id}`;
}

function findNode(key: NodeKey): MockNode | undefined {
  return NODES.find((n) => nodeKey(n) === key);
}

const USER_NAMES = [
  "amelia", "benedict", "carys", "dmitri", "elin", "farrukh",
  "greta", "hiroshi", "ines", "jovan", "katya", "liam",
];

// Each fleet runs its own namespace, so a multi-fleet demo shows several.
const FLEET_NAMESPACE: Record<string, string> = {
  main: "users.example.net",
  secondary: "team.example.org",
};

interface MockUser {
  email: string;
  fleet: string;
}

// Identities are `local@namespace`; the trailing two are guests living in
// another user's namespace rather than a fleet's.
const ALL_USERS: MockUser[] = [
  ...USER_NAMES.map((name, i) => {
    const fleet = i % 2 === 0 ? "main" : "secondary";
    return { email: `${name}@${FLEET_NAMESPACE[fleet]}`, fleet };
  }),
  { email: "amelia-server@amelia", fleet: "main" },
  { email: "jovan-laptop@jovan", fleet: "secondary" },
];

const FLEETS = [...new Set(NODES.map((n) => n.fleet))];
const MOCK_USERS = ALL_USERS.filter((u) => FLEETS.includes(u.fleet));
const USERS = MOCK_USERS.map((u) => u.email);

/** Hub nodes a user has been seen on (1-2 of their own fleet's hubs). */
function userHubNodes(email: string): MockNode[] {
  const fleet = MOCK_USERS.find((u) => u.email === email)?.fleet;
  const homeHubs = NODES.filter((n) => n.type === "hub" && n.fleet === fleet);
  if (homeHubs.length === 0) return [];

  const seed = hashStr(email);
  const usesBoth = pseudoRandom(seed, 1) > 0.5 && homeHubs.length > 1;

  return usesBoth ? homeHubs : [homeHubs[seed % homeHubs.length]!];
}

function isUserOnline(email: string, nowSeconds: number): boolean {
  const bucket = Math.floor(nowSeconds / 300); // stable for 5 minutes at a time
  return pseudoRandom(hashStr(email), bucket) > 0.35;
}

function userIps(email: string, nowSeconds: number): OnlineIp[] {
  const seed = hashStr(email);
  const count = 1 + Math.floor(pseudoRandom(seed, 2) * 2); // 1-2 IPs
  const ips: OnlineIp[] = [];
  for (let i = 0; i < count; i++) {
    const octetSeed = hashStr(`${email}:${i}`);
    // Unsigned shifts: hashStr can exceed 2^31, and `>>` yields negative octets.
    const a = 10 + (octetSeed % 50);
    const b = (octetSeed >>> 8) % 256;
    const c = (octetSeed >>> 16) % 256;
    const d = 1 + (octetSeed % 253);
    ips.push({
      ip: `${a}.${b}.${c}.${d}`,
      last_seen: nowSeconds - Math.floor(pseudoRandom(seed, i + 10) * 120),
    });
  }
  return ips;
}

// ---------------------------------------------------------------------------
// Traffic wave generation
// ---------------------------------------------------------------------------

const DAY_SECONDS = 86400;

/** Baseline bytes/sec + amplitude for a given (node, entity, dir) triple. */
function waveParams(node: string, entity: string, dir: Direction) {
  const seed = hashStr(`${node}:${entity}:${dir}`);
  const baseline = 20_000 + (seed % 180_000); // 20-200 KB/s baseline
  const amplitude = baseline * (0.5 + pseudoRandom(seed, 3) * 0.35);
  const phase = pseudoRandom(seed, 4) * Math.PI * 2;
  // downlink is typically a few times larger than uplink for proxy traffic
  const scale = dir === "down" ? 3.2 : 1;
  return { baseline: baseline * scale, amplitude: amplitude * scale, phase, seed };
}

/** Instantaneous synthetic rate (bytes/sec) at time t for one series. */
function rateAt(node: string, entity: string, dir: Direction, t: number): number {
  const { baseline, amplitude, phase, seed } = waveParams(node, entity, dir);
  const wave = Math.sin((t / DAY_SECONDS) * 2 * Math.PI + phase);
  const noise = (pseudoRandom(seed, Math.floor(t / 60)) - 0.5) * amplitude * 0.4;
  return Math.max(0, baseline + amplitude * wave + noise);
}

/** Dense bucketed byte totals aligned to from + i*step, i in [0, (to-from)/step). */
function buildPoints(node: string, entity: string, dir: Direction, from: number, to: number, step: number): number[] {
  const points: number[] = [];
  for (let t = from; t < to; t += step) {
    const mid = t + step / 2;
    points.push(Math.round(rateAt(node, entity, dir, mid) * step));
  }
  return points;
}

function sumPoints(a: number[], b: number[]): number[] {
  return a.map((v, i) => v + (b[i] ?? 0));
}

function totalBytes(node: string, entity: string, dir: Direction, from: number, to: number, step: number): number {
  return buildPoints(node, entity, dir, from, to, step).reduce((a, b) => a + b, 0);
}

// ---------------------------------------------------------------------------
// Sys-stat snapshot (stable-ish per node, drifts slowly)
// ---------------------------------------------------------------------------

function sysStats(n: MockNode, nowSeconds: number) {
  const seed = hashStr(nodeKey(n));
  const uptimeBase = 3 * 86400 + (seed % (14 * 86400));
  return {
    uptime_s: n.status === "down" || n.status === "off" ? 0 : uptimeBase,
    num_goroutine: 40 + (seed % 60),
    alloc_bytes: (30 + (seed % 90)) * 1024 * 1024,
    sys_bytes: (80 + (seed % 150)) * 1024 * 1024,
    num_gc: 100 + (Math.floor(nowSeconds / 3600) + seed) % 5000,
  };
}

function lastOkErr(n: MockNode, nowSeconds: number): { last_ok: number | null; last_err: string | null } {
  const seed = hashStr(nodeKey(n));
  if (n.status === "up") {
    return { last_ok: nowSeconds - (5 + (seed % 45)), last_err: null };
  }
  if (n.status === "stale") {
    return { last_ok: nowSeconds - (150 + (seed % 130)), last_err: null };
  }
  if (n.status === "off") {
    // Intentionally disabled - never polled, so no error and no last_ok.
    return { last_ok: null, last_err: null };
  }
  return {
    last_ok: nowSeconds - (600 + (seed % 1800)),
    last_err: "dial ssh: connect: connection refused",
  };
}

function toNodeRow(n: MockNode, nowSeconds: number): NodeRow {
  const stats = sysStats(n, nowSeconds);
  const { last_ok, last_err } = lastOkErr(n, nowSeconds);
  return {
    node: nodeKey(n),
    fleet: n.fleet,
    id: n.id,
    region: n.region,
    type: n.type,
    hostname: `${n.id}.${n.fleet}.example.net`,
    status: n.status,
    last_ok,
    last_err,
    collect: n.collect,
    ...stats,
  };
}

// ---------------------------------------------------------------------------
// OrreryClient implementation
// ---------------------------------------------------------------------------

function now(): number {
  return Math.floor(Date.now() / 1000);
}

function entityTotals(node: MockNode, entities: string[], from: number, to: number, step: number): EntityTraffic[] {
  return entities.map((entity) => ({
    entity,
    up_bytes: totalBytes(nodeKey(node), entity, "up", from, to, step),
    down_bytes: totalBytes(nodeKey(node), entity, "down", from, to, step),
  }));
}

function nodeInboundTotal(node: MockNode, from: number, to: number, step: number): { up: number; down: number } {
  let up = 0;
  let down = 0;
  for (const entity of node.inbounds) {
    up += totalBytes(nodeKey(node), entity, "up", from, to, step);
    down += totalBytes(nodeKey(node), entity, "down", from, to, step);
  }
  return { up, down };
}

function userTotalsOnNode(email: string, node: MockNode, from: number, to: number, step: number): { up: number; down: number } {
  // Users are pseudo-entities within a hub node's "user" kind traffic.
  return {
    up: totalBytes(nodeKey(node), email, "up", from, to, step),
    down: totalBytes(nodeKey(node), email, "down", from, to, step),
  };
}

export const mockClient: OrreryClient = {
  async getMe() {
    return { name: "mock", method: "token" as const, fleets: null };
  },
  async getOverview(range: Range): Promise<Overview> {
    const { from, to, step } = windowForRange(range);
    const nowS = now();

    const total = NODES.length;
    const up = NODES.filter((n) => n.status === "up").length;
    const stale = NODES.filter((n) => n.status === "stale").length;
    const down = NODES.filter((n) => n.status === "down").length;
    const off = NODES.filter((n) => n.status === "off").length;

    const fleetNames = [...new Set(NODES.map((n) => n.fleet))];
    const fleets = fleetNames.map((fleet) => {
      const fleetNodes = NODES.filter((n) => n.fleet === fleet);
      const hubNodes = fleetNodes.filter((n) => n.type === "hub");
      let upBytes = 0;
      let downBytes = 0;
      for (const hn of hubNodes) {
        const t = nodeInboundTotal(hn, from, to, step);
        upBytes += t.up;
        downBytes += t.down;
      }
      return {
        fleet,
        nodes_up: fleetNodes.filter((n) => n.status === "up").length,
        nodes_total: fleetNodes.length,
        up_bytes: upBytes,
        down_bytes: downBytes,
      };
    });

    const totals = fleets.reduce(
      (acc, f) => ({ up_bytes: acc.up_bytes + f.up_bytes, down_bytes: acc.down_bytes + f.down_bytes }),
      { up_bytes: 0, down_bytes: 0 },
    );

    const onlineUsers = USERS.filter((e) => isUserOnline(e, nowS)).length;

    const topUsers = USERS.map((email) => {
      const hubs = userHubNodes(email);
      let upBytes = 0;
      let downBytes = 0;
      for (const h of hubs) {
        const t = userTotalsOnNode(email, h, from, to, step);
        upBytes += t.up;
        downBytes += t.down;
      }
      return { email, up_bytes: upBytes, down_bytes: downBytes };
    })
      .sort((a, b) => b.up_bytes + b.down_bytes - (a.up_bytes + a.down_bytes))
      .slice(0, 10);

    const topNodes = NODES.filter((n) => n.collect !== "off")
      .map((n) => {
        const t = nodeInboundTotal(n, from, to, step);
        return { node: nodeKey(n), up_bytes: t.up, down_bytes: t.down };
      })
      .sort((a, b) => b.up_bytes + b.down_bytes - (a.up_bytes + a.down_bytes))
      .slice(0, 10);

    return {
      generated_at: nowS,
      nodes: { total, up, stale, down, off },
      online_users: onlineUsers,
      totals,
      fleets,
      top_users: topUsers,
      top_nodes: topNodes,
    };
  },

  async getNodes(): Promise<NodeRow[]> {
    const nowS = now();
    return NODES.map((n) => toNodeRow(n, nowS));
  },

  async getNode(node: NodeKey, range: Range): Promise<NodeDetail> {
    const n = findNode(node);
    if (!n) throw new Error(`mock: unknown node ${node}`);
    const { from, to, step } = windowForRange(range);
    const nowS = now();
    const users =
      n.type === "hub" && n.collect === "full"
        ? USERS.filter((email) => userHubNodes(email).some((h) => nodeKey(h) === node)).map((email) => {
            const t = userTotalsOnNode(email, n, from, to, step);
            return { email, up_bytes: t.up, down_bytes: t.down };
          })
        : [];
    return {
      ...toNodeRow(n, nowS),
      inbounds: entityTotals(n, n.inbounds, from, to, step),
      outbounds: entityTotals(n, n.outbounds, from, to, step),
      users,
    };
  },

  async getSeries(query: SeriesQuery): Promise<SeriesResponse> {
    const { from, to, step, kind, node, fleet, type, entity, dir, agg = "none" } = query;

    let candidates = NODES.filter((n) => n.collect !== "off");
    if (node) candidates = candidates.filter((n) => nodeKey(n) === node);
    if (fleet) candidates = candidates.filter((n) => n.fleet === fleet);

    if (kind === "online") {
      // Only node/fleet filters apply; gauge count of online users per node.
      const gaugePoints = (n: MockNode): number[] => {
        const pts: number[] = [];
        const hubUsers = USERS.filter((e) => userHubNodes(e).some((h) => nodeKey(h) === nodeKey(n)));
        for (let t = from; t < to; t += step) {
          const bucket = Math.floor(t / 300);
          const count = hubUsers.filter((e) => pseudoRandom(hashStr(e), bucket) > 0.35).length;
          pts.push(count);
        }
        return pts;
      };
      const hubCandidates = candidates.filter((n) => n.type === "hub");
      if (node) {
        const n = hubCandidates[0];
        return { from, to, step, series: n ? [{ node: nodeKey(n), points: gaugePoints(n) }] : [] };
      }
      if (agg === "node") {
        return { from, to, step, series: hubCandidates.map((n) => ({ node: nodeKey(n), points: gaugePoints(n) })) };
      }
      // total: sum across candidate hub nodes into a single line
      const combined = hubCandidates.reduce<number[]>((acc, n) => {
        const pts = gaugePoints(n);
        return acc.length ? sumPoints(acc, pts) : pts;
      }, []);
      return { from, to, step, series: combined.length ? [{ points: combined }] : [] };
    }

    if (type) candidates = candidates.filter((n) => n.type === type);

    const dirs: Direction[] = dir ? [dir] : ["up", "down"];

    // Build (node, entity) pairs depending on kind.
    type Pair = { n: MockNode; e: string };
    let pairs: Pair[];
    if (kind === "user") {
      pairs = candidates
        .filter((n) => n.type === "hub" && n.collect === "full")
        .flatMap((n) => USERS.filter((e) => userHubNodes(e).some((h) => nodeKey(h) === nodeKey(n))).map((e) => ({ n, e })));
    } else if (kind === "outbound") {
      pairs = candidates.flatMap((n) => n.outbounds.map((e) => ({ n, e })));
    } else {
      pairs = candidates.flatMap((n) => n.inbounds.map((e) => ({ n, e })));
    }
    if (entity) pairs = pairs.filter((p) => p.e === entity);

    const series: SeriesLine[] = [];

    if (agg === "none") {
      for (const { n, e } of pairs) {
        for (const d of dirs) {
          series.push({ node: nodeKey(n), entity: e, dir: d, points: buildPoints(nodeKey(n), e, d, from, to, step) });
        }
      }
    } else if (agg === "entity") {
      const byEntity = new Map<string, Pair[]>();
      for (const p of pairs) {
        const list = byEntity.get(p.e) ?? [];
        list.push(p);
        byEntity.set(p.e, list);
      }
      for (const [e, ps] of byEntity) {
        for (const d of dirs) {
          const pts = ps.reduce<number[]>((acc, p) => {
            const cur = buildPoints(nodeKey(p.n), p.e, d, from, to, step);
            return acc.length ? sumPoints(acc, cur) : cur;
          }, []);
          series.push({ entity: e, dir: d, points: pts });
        }
      }
    } else if (agg === "node") {
      const byNode = new Map<string, Pair[]>();
      for (const p of pairs) {
        const key = nodeKey(p.n);
        const list = byNode.get(key) ?? [];
        list.push(p);
        byNode.set(key, list);
      }
      for (const [nk, ps] of byNode) {
        for (const d of dirs) {
          const pts = ps.reduce<number[]>((acc, p) => {
            const cur = buildPoints(nk, p.e, d, from, to, step);
            return acc.length ? sumPoints(acc, cur) : cur;
          }, []);
          series.push({ node: nk, dir: d, points: pts });
        }
      }
    } else {
      // total
      for (const d of dirs) {
        const pts = pairs.reduce<number[]>((acc, p) => {
          const cur = buildPoints(nodeKey(p.n), p.e, d, from, to, step);
          return acc.length ? sumPoints(acc, cur) : cur;
        }, []);
        if (pts.length) series.push({ dir: d, points: pts });
      }
    }

    return { from, to, step, series };
  },

  async getUsers(range: Range, seen: SeenWindow, fleet?: string): Promise<UserRow[]> {
    const { from, to, step } = windowForRange(range);
    const seenWin = windowForRange(seen);
    const nowS = now();
    return USERS.map((email) => {
      let hubs = userHubNodes(email);
      if (fleet) hubs = hubs.filter((h) => h.fleet === fleet);
      let up = 0;
      let down = 0;
      for (const h of hubs) {
        const t = userTotalsOnNode(email, h, from, to, step);
        up += t.up;
        down += t.down;
      }
      const seenHubs = hubs
        .map((h) => {
          const t = userTotalsOnNode(email, h, seenWin.from, seenWin.to, seenWin.step);
          if (t.up + t.down === 0 && !isUserOnline(email, nowS)) return null;
          return { node: nodeKey(h), last_seen: nowS - (hashStr(email + nodeKey(h)) % 3600) };
        })
        .filter((h): h is { node: string; last_seen: number } => !!h)
        .sort((a, b) => b.last_seen - a.last_seen);
      return {
        email,
        up_bytes: up,
        down_bytes: down,
        online_now: isUserOnline(email, nowS),
        hubs: seenHubs,
      };
    }).filter((u) => !fleet || u.hubs.length > 0 || u.up_bytes + u.down_bytes > 0);
  },

  async getUser(email: string, range: Range, seen: SeenWindow): Promise<UserDetail> {
    const { from, to, step } = windowForRange(range);
    const seenWin = windowForRange(seen);
    const nowS = now();
    const hubs = userHubNodes(email);
    const nodes = hubs.map((h) => {
      const t = userTotalsOnNode(email, h, from, to, step);
      return { node: nodeKey(h), up_bytes: t.up, down_bytes: t.down };
    });
    const online = isUserOnline(email, nowS);
    const seen_hubs = hubs
      .map((h) => {
        const t = userTotalsOnNode(email, h, seenWin.from, seenWin.to, seenWin.step);
        if (t.up + t.down === 0 && !online) return null;
        return { node: nodeKey(h), last_seen: nowS - (hashStr(email + nodeKey(h)) % 3600) };
      })
      .filter((h): h is { node: string; last_seen: number } => !!h)
      .sort((a, b) => b.last_seen - a.last_seen);
    return {
      email,
      up_bytes: nodes.reduce((a, n) => a + n.up_bytes, 0),
      down_bytes: nodes.reduce((a, n) => a + n.down_bytes, 0),
      online_now: online,
      nodes,
      seen_hubs,
      ips: online ? userIps(email, nowS) : [],
    };
  },

  async getOnline(): Promise<OnlineNodeUsers[]> {
    const nowS = now();
    const result: OnlineNodeUsers[] = [];
    for (const n of NODES.filter((n) => n.type === "hub" && n.collect === "full")) {
      for (const email of USERS.filter((e) => userHubNodes(e).some((h) => nodeKey(h) === nodeKey(n)))) {
        if (!isUserOnline(email, nowS)) continue;
        result.push({ node: nodeKey(n), email, ips: userIps(email, nowS) });
      }
    }
    return result;
  },
};
