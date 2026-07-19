import { useMemo } from "react";
import { useAuth } from "../lib/auth";
import { useApiData } from "../lib/useApiData";
import { useAutoRefresh } from "../lib/useAutoRefresh";
import { useFleets } from "../lib/fleets";
import { windowForRange } from "../lib/range";
import { formatBytes, formatDuration } from "../lib/format";
import { STATUS_HELP, STATUS_LABEL } from "../lib/glossary";
import type { NodeRow, NodeStatus, NodeType } from "../api/types";
import { DataTable, type Column } from "../components/DataTable";
import { StatusDot } from "../components/StatusDot";
import { Sparkline } from "../components/Sparkline";
import { RefreshControls } from "../components/RefreshControls";
import { Link, nodeHref, nodesHref, useRouter } from "../lib/router";

interface NodeWithSpark extends NodeRow {
  sparkTimestamps: number[];
  sparkPoints: number[];
  upBytes24h: number;
  downBytes24h: number;
}

const STATUS_FILTERS: Array<NodeStatus | "all"> = ["all", "up", "stale", "down", "off"];

function typeRank(type: NodeType): number {
  return type === "hub" ? 0 : 1;
}

/** Tint for the inline status word; "up" shows the dot only. */
const STATUS_CHIP_CLASS: Record<NodeStatus, string> = {
  up: "",
  stale: "bg-stale/15 text-stale",
  down: "bg-down/15 text-down",
  off: "bg-off/25 text-text-muted",
};

function parseStatus(raw: string | null): NodeStatus | "all" {
  if (raw === "up" || raw === "stale" || raw === "down" || raw === "off") return raw;
  return "all";
}

export default function Nodes() {
  const { client } = useAuth();
  const { nodeLabel } = useFleets();
  const { navigate, search } = useRouter();
  const params = useMemo(() => new URLSearchParams(search), [search]);
  const fleetFilter = params.get("fleet") ?? "all";
  const statusFilter = parseStatus(params.get("status"));

  const nodes = useApiData(() => client.getNodes(), [client]);

  const traffic = useApiData(async () => {
    const { from, to, step } = windowForRange("24h");
    const res = await client.getSeries({ from, to, step, kind: "inbound", agg: "node" });
    const timestamps = Array.from({ length: Math.round((to - from) / step) }, (_, i) => from + i * step);
    return { timestamps, series: res.series };
  }, [client]);

  const refreshAll = () => {
    nodes.refresh();
    traffic.refresh();
  };

  useAutoRefresh(refreshAll, 30_000);

  const fleets = useMemo(() => {
    const set = new Set(nodes.data?.map((n) => n.fleet) ?? []);
    return [...set].sort();
  }, [nodes.data]);

  const rows = useMemo<NodeWithSpark[]>(() => {
    if (!nodes.data) return [];
    const timestamps = traffic.data?.timestamps ?? [];
    return nodes.data
      .filter((n) => (fleetFilter === "all" ? true : n.fleet === fleetFilter))
      .filter((n) => (statusFilter === "all" ? true : n.status === statusFilter))
      .map((n) => {
        const up = traffic.data?.series.find((s) => s.node === n.node && s.dir === "up")?.points ?? [];
        const down = traffic.data?.series.find((s) => s.node === n.node && s.dir === "down")?.points ?? [];
        const combined = timestamps.map((_, i) => (up[i] ?? 0) + (down[i] ?? 0));
        return {
          ...n,
          sparkTimestamps: timestamps,
          sparkPoints: combined,
          upBytes24h: up.reduce((a, b) => a + b, 0),
          downBytes24h: down.reduce((a, b) => a + b, 0),
        };
      });
  }, [nodes.data, traffic.data, fleetFilter, statusFilter]);

  const applyFilters = (fleet: string, status: NodeStatus | "all") => {
    navigate(
      nodesHref({
        fleet: fleet === "all" ? undefined : fleet,
        status: status === "all" ? undefined : status,
      }),
    );
  };

  const columns: Column<NodeWithSpark>[] = [
    {
      // Status renders inside this cell rather than owning a column of its own.
      key: "node",
      header: "Node",
      sort: (a, b) => a.node.localeCompare(b.node),
      render: (r) => (
        <div className="flex items-start gap-2">
          <StatusDot status={r.status} showLabel={false} className="mt-1.5" />
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-1.5">
              <Link href={nodeHref(r.node)} className="font-medium text-accent hover:underline">
                {nodeLabel(r.node)}
              </Link>
              {r.status !== "up" && (
                <span
                  className={`rounded px-1.5 py-0.5 text-[0.65rem] font-medium ${STATUS_CHIP_CLASS[r.status]}`}
                  title={STATUS_HELP[r.status]}
                >
                  {STATUS_LABEL[r.status].toLowerCase()}
                </span>
              )}
            </div>
            <div className="text-xs text-text-muted">{r.hostname}</div>
          </div>
        </div>
      ),
    },
    {
      key: "type",
      header: "Type",
      // Hubs first, then exits, node key as a stable tiebreak.
      sort: (a, b) => typeRank(a.type) - typeRank(b.type) || a.node.localeCompare(b.node),
      render: (r) => <span className="capitalize text-text-muted">{r.type}</span>,
    },
    { key: "region", header: "Region", sort: (a, b) => a.region.localeCompare(b.region), render: (r) => r.region },
    {
      key: "uptime",
      header: "Uptime",
      align: "right",
      numeric: true,
      sort: (a, b) => a.uptime_s - b.uptime_s,
      render: (r) => (r.status === "down" || r.status === "off" ? "—" : formatDuration(r.uptime_s)),
    },
    {
      key: "mem",
      header: "Mem",
      align: "right",
      numeric: true,
      sort: (a, b) => a.alloc_bytes - b.alloc_bytes,
      render: (r) => formatBytes(r.alloc_bytes),
    },
    {
      key: "traffic24h",
      header: "24h traffic",
      align: "right",
      numeric: true,
      sort: (a, b) => a.upBytes24h + a.downBytes24h - (b.upBytes24h + b.downBytes24h),
      render: (r) => (
        <div className="leading-tight">
          <div className="text-uplink">&uarr; {formatBytes(r.upBytes24h)}</div>
          <div className="text-downlink">&darr; {formatBytes(r.downBytes24h)}</div>
        </div>
      ),
    },
    {
      key: "spark",
      header: "",
      render: (r) =>
        r.sparkPoints.some((v) => v > 0) ? (
          <Sparkline timestamps={r.sparkTimestamps} points={r.sparkPoints} colorVar="--color-series-1" height={28} className="w-24" />
        ) : (
          <span className="text-xs text-text-faint">—</span>
        ),
      className: "w-24",
    },
  ];

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="page-title">Nodes</h1>
          <p className="text-xs text-text-muted">
            {nodes.data ? `${rows.length} shown · traffic sparklines are 24h inbound` : " "}
          </p>
          <RefreshControls
            refreshing={nodes.refreshing || traffic.refreshing}
            onRefresh={refreshAll}
            className="mt-0.5"
          />
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {fleets.length > 1 && (
            <label className="flex items-center gap-1.5 text-xs text-text-muted">
              Fleet
              <select
                value={fleetFilter}
                onChange={(e) => applyFilters(e.target.value, statusFilter)}
                className="rounded-md border border-border bg-surface px-2 py-1 text-xs text-text outline-none focus:border-accent"
              >
                <option value="all">All</option>
                {fleets.map((f) => (
                  <option key={f} value={f}>
                    {f}
                  </option>
                ))}
              </select>
            </label>
          )}
          <label className="flex items-center gap-1.5 text-xs text-text-muted">
            Status
            <select
              value={statusFilter}
              onChange={(e) => applyFilters(fleetFilter, e.target.value as NodeStatus | "all")}
              className="rounded-md border border-border bg-surface px-2 py-1 text-xs text-text outline-none focus:border-accent"
            >
              {STATUS_FILTERS.map((s) => (
                <option key={s} value={s}>
                  {s === "all" ? "All" : s}
                </option>
              ))}
            </select>
          </label>
        </div>
      </div>

      <DataTable
        columns={columns}
        rows={rows}
        rowKey={(r) => r.node}
        loading={nodes.loading}
        error={nodes.error}
        onRowClick={(r) => navigate(nodeHref(r.node))}
        emptyMessage="No nodes match these filters."
        skeletonRows={6}
        defaultSort={{ key: "type", dir: "asc" }}
      />
    </div>
  );
}
