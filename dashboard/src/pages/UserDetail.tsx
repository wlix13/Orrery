import { useMemo, useState } from "react";
import { useAuth } from "../lib/auth";
import { useApiData } from "../lib/useApiData";
import { useAutoRefresh } from "../lib/useAutoRefresh";
import { RANGE_KEY_USERS, useStoredRange } from "../lib/useStoredRange";
import { useFleets } from "../lib/fleets";
import { splitUserId } from "../lib/identity";
import { windowForRange } from "../lib/range";
import { formatBytes, formatRelativeTime } from "../lib/format";
import type { Range, SeenWindow } from "../api/types";
import type { OrreryClient } from "../api/client";
import { RangePicker } from "../components/RangePicker";
import { SeenPicker } from "../components/SeenPicker";
import { RefreshControls } from "../components/RefreshControls";
import { Breadcrumb } from "../components/Breadcrumb";
import { StatCard } from "../components/StatCard";
import { Chart, type ChartSeries } from "../components/Chart";
import { DataTable, type Column } from "../components/DataTable";
import { Panel } from "../components/Panel";
import { Link, nodeHref } from "../lib/router";

const NODE_COLORS = [
  "--color-series-1", "--color-series-2", "--color-series-3", "--color-series-4",
  "--color-series-5", "--color-series-6", "--color-series-7", "--color-series-8",
];

const SEEN_KEY = "orrery.seen.users";

function readSeen(): SeenWindow {
  try {
    const v = localStorage.getItem(SEEN_KEY);
    if (v === "1h" || v === "6h" || v === "24h") return v;
  } catch {
    // ignore
  }
  return "6h";
}

interface UserChartData {
  timestamps: number[];
  series: ChartSeries[];
}

/** One row per hub: traffic over the range, union'd with presence in the seen window. */
interface HubRow {
  node: string;
  up_bytes: number;
  down_bytes: number;
  last_seen: number | null;
}

async function fetchUserChart(client: OrreryClient, email: string, range: Range): Promise<UserChartData> {
  const { from, to, step } = windowForRange(range);
  const res = await client.getSeries({ from, to, step, kind: "user", entity: email, agg: "node" });
  const timestamps = Array.from({ length: Math.round((to - from) / step) }, (_, i) => from + i * step);
  const nodes = [...new Set(res.series.map((s) => s.node).filter((n): n is string => !!n))];
  const series: ChartSeries[] = nodes.map((node, i) => {
    const up = res.series.find((s) => s.node === node && s.dir === "up")?.points ?? [];
    const down = res.series.find((s) => s.node === node && s.dir === "down")?.points ?? [];
    const combined = timestamps.map((_, idx) => (up[idx] ?? 0) + (down[idx] ?? 0));
    return { label: node, colorVar: NODE_COLORS[i % NODE_COLORS.length]!, points: combined };
  });
  return { timestamps, series };
}

export default function UserDetail({ email }: { email: string }) {
  const { client } = useAuth();
  const { nodeLabel } = useFleets();
  const identity = splitUserId(email);
  const [range, setRange] = useStoredRange("30d", RANGE_KEY_USERS);
  const [seen, setSeenState] = useState<SeenWindow>(readSeen);

  const setSeen = (next: SeenWindow) => {
    setSeenState(next);
    try {
      localStorage.setItem(SEEN_KEY, next);
    } catch {
      // ignore
    }
  };

  const detail = useApiData(() => client.getUser(email, range, seen), [client, email, range, seen]);
  const chart = useApiData(() => fetchUserChart(client, email, range), [client, email, range]);

  const refreshAll = () => {
    detail.refresh();
    chart.refresh();
  };

  useAutoRefresh(refreshAll, 30_000);

  const refreshing = detail.refreshing || chart.refreshing;

  // Relabelled here, not in the fetcher, so a late fleet list doesn't refetch.
  const chartSeries = useMemo(
    () => chart.data?.series.map((s) => ({ ...s, label: nodeLabel(s.label) })) ?? [],
    [chart.data, nodeLabel],
  );

  const hubRows = useMemo<HubRow[]>(() => {
    const data = detail.data;
    if (!data) return [];

    const lastSeen = new Map(data.seen_hubs.map((h) => [h.node, h.last_seen]));
    const rows = new Map<string, HubRow>(
      data.nodes.map((n) => [n.node, { ...n, last_seen: lastSeen.get(n.node) ?? null }]),
    );

    for (const hub of data.seen_hubs) {
      if (!rows.has(hub.node)) {
        rows.set(hub.node, { node: hub.node, up_bytes: 0, down_bytes: 0, last_seen: hub.last_seen });
      }
    }

    return [...rows.values()];
  }, [detail.data]);

  const hubColumns: Column<HubRow>[] = [
    {
      key: "node",
      header: "Hub",
      sort: (a, b) => a.node.localeCompare(b.node),
      render: (r) => (
        <div className="flex items-center gap-2">
          <span
            className={`inline-block h-2 w-2 shrink-0 rounded-full ${r.last_seen === null ? "bg-border" : "bg-up"}`}
            aria-hidden
          />
          <Link href={nodeHref(r.node)} className="text-accent hover:underline">
            {nodeLabel(r.node)}
          </Link>
        </div>
      ),
    },
    {
      key: "last_seen",
      header: <span title={`Traffic or online presence within the last ${seen}`}>Last seen</span>,
      align: "right",
      sort: (a, b) => (a.last_seen ?? -1) - (b.last_seen ?? -1),
      render: (r) =>
        r.last_seen === null ? (
          <span className="text-text-faint" title={`Nothing in the last ${seen}`}>
            —
          </span>
        ) : (
          <span className="text-text-muted">{formatRelativeTime(r.last_seen)}</span>
        ),
    },
    {
      key: "up",
      header: "Uplink",
      align: "right",
      numeric: true,
      sort: (a, b) => a.up_bytes - b.up_bytes,
      render: (r) => <span className="text-uplink">{formatBytes(r.up_bytes)}</span>,
    },
    {
      key: "down",
      header: "Downlink",
      align: "right",
      numeric: true,
      sort: (a, b) => a.down_bytes - b.down_bytes,
      render: (r) => <span className="text-downlink">{formatBytes(r.down_bytes)}</span>,
    },
  ];

  const ipColumns: Column<{ ip: string; last_seen: number }>[] = [
    { key: "ip", header: "IP", sort: (a, b) => a.ip.localeCompare(b.ip), render: (r) => <span className="tabular-nums">{r.ip}</span> },
    {
      key: "last_seen",
      header: "Last seen",
      align: "right",
      sort: (a, b) => a.last_seen - b.last_seen,
      render: (r) => formatRelativeTime(r.last_seen),
    },
  ];

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <Breadcrumb crumbs={[{ label: "Users", href: "/users" }, { label: email }]} />
          {/* Namespace is a separate token so long identities wrap at the "@". */}
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <h1 className="page-title font-mono text-xl break-all sm:text-2xl">{identity.local}</h1>
            {identity.namespace !== null && (
              <span className="rounded bg-surface-raised px-1.5 py-0.5 font-mono text-xs text-text-faint">
                @{identity.namespace}
              </span>
            )}
            {detail.data && (
              <span
                className={`rounded-full px-2 py-0.5 text-xs font-medium ${
                  detail.data.online_now ? "bg-up/15 text-up" : "bg-border/40 text-text-faint"
                }`}
              >
                {detail.data.online_now ? "online" : "offline"}
              </span>
            )}
          </div>
          <RefreshControls refreshing={refreshing} onRefresh={refreshAll} className="mt-0.5" />
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <SeenPicker value={seen} onChange={setSeen} />
          <RangePicker value={range} onChange={setRange} />
        </div>
      </div>

      {detail.error && (
        <div className="rounded-lg border border-down/30 bg-down/10 px-4 py-3 text-sm text-down">
          Failed to load user: {detail.error}
        </div>
      )}

      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <StatCard
          label="Uplink"
          loading={detail.loading}
          value={detail.data ? formatBytes(detail.data.up_bytes) : undefined}
          sub={`over ${range}`}
          className="border-l-2 border-l-uplink"
        />
        <StatCard
          label="Downlink"
          loading={detail.loading}
          value={detail.data ? formatBytes(detail.data.down_bytes) : undefined}
          sub={`over ${range}`}
          className="border-l-2 border-l-downlink"
        />
        <StatCard
          label={`Hubs (${seen})`}
          loading={detail.loading}
          value={detail.data ? detail.data.seen_hubs.length : undefined}
          sub="recent traffic or online"
        />
        <StatCard
          label="Active IPs"
          loading={detail.loading}
          value={detail.data ? detail.data.ips.length : undefined}
        />
      </div>

      <Panel title="Traffic by hub" className={refreshing ? "opacity-70 transition-opacity" : undefined}>
        {chart.loading ? (
          <div className="skeleton h-52 w-full" />
        ) : chart.error ? (
          <div className="flex h-52 items-center justify-center text-sm text-down">{chart.error}</div>
        ) : chart.data && chartSeries.length > 0 ? (
          <Chart timestamps={chart.data.timestamps} series={chartSeries} stacked height={208} />
        ) : (
          <div className="flex h-52 items-center justify-center text-sm text-text-muted">No traffic for this range.</div>
        )}
      </Panel>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Panel
          title="Hubs"
          action={
            <span className="text-xs whitespace-nowrap text-text-faint">
              seen {seen} · traffic {range}
            </span>
          }
        >
          <DataTable
            columns={hubColumns}
            rows={hubRows}
            rowKey={(r) => r.node}
            loading={detail.loading}
            error={detail.error}
            emptyMessage="No hub activity in this range."
            defaultSort={{ key: "last_seen", dir: "desc" }}
          />
        </Panel>
        <Panel title="Online IPs">
          <DataTable
            columns={ipColumns}
            rows={detail.data?.ips ?? []}
            rowKey={(r) => r.ip}
            loading={detail.loading}
            error={detail.error}
            emptyMessage="No active sessions."
            defaultSort={{ key: "last_seen", dir: "desc" }}
          />
        </Panel>
      </div>
    </div>
  );
}
