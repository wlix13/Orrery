import { useMemo } from "react";
import { useAuth } from "../lib/auth";
import { useApiData } from "../lib/useApiData";
import { useAutoRefresh } from "../lib/useAutoRefresh";
import { useStoredRange } from "../lib/useStoredRange";
import { useFleets } from "../lib/fleets";
import { dominantNamespace } from "../lib/identity";
import { windowForRange } from "../lib/range";
import { formatBytes, formatCount } from "../lib/format";
import { RangePicker } from "../components/RangePicker";
import { RefreshControls } from "../components/RefreshControls";
import { StatCard } from "../components/StatCard";
import { UserName } from "../components/UserName";
import { Chart, type ChartSeries } from "../components/Chart";
import { DataTable, type Column } from "../components/DataTable";
import { Panel } from "../components/Panel";
import { Link, nodeHref, nodesHref, userHref } from "../lib/router";

const FLEET_SERIES_COLORS = ["--color-series-1", "--color-series-2", "--color-series-3", "--color-series-4"];

interface TrafficChart {
  timestamps: number[];
  series: ChartSeries[];
}

/** "3 stale · 1 down", dropping the zeroes. */
function nodesBreakdown(n: { stale: number; down: number; off: number }): string {
  const parts: string[] = [];
  if (n.stale > 0) parts.push(`${n.stale} stale`);
  if (n.down > 0) parts.push(`${n.down} down`);
  if (n.off > 0) parts.push(`${n.off} off`);
  return parts.length > 0 ? parts.join(" · ") : "all reporting";
}

export default function Overview() {
  const { client } = useAuth();
  const { nodeLabel, soleFleet } = useFleets();
  const [range, setRange] = useStoredRange("1h");

  const overview = useApiData(() => client.getOverview(range), [client, range]);

  // Sorted so each series keeps its colour across refreshes.
  const fleetNames = useMemo(
    () => (overview.data?.fleets.map((f) => f.fleet) ?? []).sort(),
    [overview.data],
  );
  const fleetKey = fleetNames.join(",");
  const perFleet = fleetNames.length > 1;

  const traffic = useApiData(async (): Promise<TrafficChart> => {
    if (fleetNames.length === 0) return { timestamps: [], series: [] };
    const { from, to, step } = windowForRange(range);
    const timestamps = Array.from({ length: Math.round((to - from) / step) }, (_, i) => from + i * step);

    // One fleet stacks into a single band, so split by direction instead.
    if (!perFleet) {
      const res = await client.getSeries({ from, to, step, kind: "inbound", type: "hub", agg: "total" });
      const up = res.series.find((s) => s.dir === "up")?.points ?? [];
      const down = res.series.find((s) => s.dir === "down")?.points ?? [];
      return {
        timestamps,
        series: [
          { label: "Uplink", colorVar: "--color-uplink", points: timestamps.map((_, i) => up[i] ?? 0) },
          { label: "Downlink", colorVar: "--color-downlink", points: timestamps.map((_, i) => down[i] ?? 0) },
        ],
      };
    }

    const results = await Promise.all(
      fleetNames.map((fleet) => client.getSeries({ from, to, step, kind: "inbound", type: "hub", fleet, agg: "total" })),
    );
    const series: ChartSeries[] = results.map((res, i) => {
      const up = res.series.find((s) => s.dir === "up")?.points ?? [];
      const down = res.series.find((s) => s.dir === "down")?.points ?? [];
      const total = timestamps.map((_, idx) => (up[idx] ?? 0) + (down[idx] ?? 0));
      return { label: fleetNames[i]!, colorVar: FLEET_SERIES_COLORS[i % FLEET_SERIES_COLORS.length]!, points: total };
    });
    return { timestamps, series };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [client, range, fleetKey]);

  const online = useApiData(async () => {
    const { from, to, step } = windowForRange(range);
    const res = await client.getSeries({ from, to, step, kind: "online" });
    const timestamps = Array.from({ length: Math.round((to - from) / step) }, (_, i) => from + i * step);
    const points = res.series[0]?.points ?? [];
    // Built here, not inline in JSX, to keep the array identity stable.
    const series: ChartSeries[] = [{ label: "Online users", colorVar: "--color-series-5", points, fill: true }];
    return { timestamps, series, points, peak: points.reduce((max, v) => Math.max(max, v ?? 0), 0) };
  }, [client, range]);

  const refreshAll = () => {
    overview.refresh();
    traffic.refresh();
    online.refresh();
  };

  useAutoRefresh(refreshAll, 30_000);

  const refreshing = overview.refreshing || traffic.refreshing || online.refreshing;

  const topUserNamespace = useMemo(
    () => (soleFleet === null ? null : dominantNamespace(overview.data?.top_users.map((u) => u.email) ?? [])),
    [overview.data, soleFleet],
  );

  const topUserColumns: Column<{ email: string; up_bytes: number; down_bytes: number }>[] = [
    {
      key: "email",
      header: "User",
      render: (r) => (
        <Link href={userHref(r.email)} className="text-accent hover:underline">
          <UserName email={r.email} mainNamespace={topUserNamespace} />
        </Link>
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

  const topNodeColumns: Column<{ node: string; up_bytes: number; down_bytes: number }>[] = [
    { key: "node", header: "Node", render: (r) => <Link href={nodeHref(r.node)} className="text-accent hover:underline">{nodeLabel(r.node)}</Link> },
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

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="page-title">Overview</h1>
          <RefreshControls
            updatedAt={overview.data?.generated_at}
            refreshing={refreshing}
            onRefresh={refreshAll}
            className="mt-0.5"
          />
        </div>
        <RangePicker value={range} onChange={setRange} />
      </div>

      {overview.error && (
        <div className="rounded-lg border border-down/30 bg-down/10 px-4 py-3 text-sm text-down">
          Failed to load overview: {overview.error}
        </div>
      )}

      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <StatCard
          label="Nodes"
          loading={overview.loading}
          value={overview.data ? `${overview.data.nodes.up}/${overview.data.nodes.total}` : undefined}
          sub={overview.data ? nodesBreakdown(overview.data.nodes) : undefined}
        />
        <StatCard
          label="Online users"
          loading={overview.loading}
          value={overview.data ? formatCount(overview.data.online_users) : undefined}
          sub="currently online on hubs"
        />
        <StatCard
          label="Uplink"
          loading={overview.loading}
          value={overview.data ? formatBytes(overview.data.totals.up_bytes) : undefined}
          sub={`over ${range}`}
          className="border-l-2 border-l-uplink"
        />
        <StatCard
          label="Downlink"
          loading={overview.loading}
          value={overview.data ? formatBytes(overview.data.totals.down_bytes) : undefined}
          sub={`over ${range}`}
          className="border-l-2 border-l-downlink"
        />
      </div>

      {overview.data && overview.data.fleets.length > 1 && (
        <div
          className={`grid grid-cols-1 gap-3 sm:grid-cols-2 ${
            overview.data.fleets.length >= 3 ? "lg:grid-cols-3" : ""
          }`}
        >
          {overview.data.fleets.map((f) => (
            <Link
              key={f.fleet}
              href={nodesHref({ fleet: f.fleet })}
              className="flex h-16 items-center justify-between rounded-xl border border-border/80 bg-surface/90 px-4 transition-colors hover:border-accent/50 hover:bg-surface-raised"
            >
              <div>
                <div className="text-sm font-medium text-text">{f.fleet}</div>
                <div className="text-xs text-text-muted">{f.nodes_up}/{f.nodes_total} nodes up</div>
              </div>
              <div className="text-right text-xs tabular-nums">
                <div className="text-uplink">&uarr; {formatBytes(f.up_bytes)}</div>
                <div className="text-downlink">&darr; {formatBytes(f.down_bytes)}</div>
              </div>
            </Link>
          ))}
        </div>
      )}

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <Panel
          title={perFleet ? "Hub inbound traffic by fleet" : "Hub inbound traffic"}
          className={`lg:col-span-2 ${refreshing ? "opacity-70 transition-opacity" : ""}`}
        >
          {traffic.loading ? (
            <div className="skeleton h-56 w-full" />
          ) : traffic.error ? (
            <div className="flex h-56 items-center justify-center text-sm text-down">{traffic.error}</div>
          ) : traffic.data && traffic.data.series.length > 0 ? (
            <Chart timestamps={traffic.data.timestamps} series={traffic.data.series} stacked height={224} />
          ) : (
            <div className="flex h-56 items-center justify-center text-sm text-text-muted">No traffic data.</div>
          )}
        </Panel>

        <Panel
          title="Online users over time"
          action={
            online.data && online.data.points.length > 0 ? (
              <span className="text-xs tabular-nums text-text-faint">peak {formatCount(online.data.peak)}</span>
            ) : undefined
          }
          className={refreshing ? "opacity-70 transition-opacity" : ""}
        >
          <div className="flex h-56 flex-col justify-end">
            {online.loading ? (
              <div className="skeleton h-full w-full" />
            ) : online.data && online.data.points.length > 0 ? (
              <Chart
                timestamps={online.data.timestamps}
                series={online.data.series}
                height={224}
                valueFormatter={formatCount}
                summary="peak"
                showLegend={false}
                integerAxis
              />
            ) : (
              <div className="flex h-full items-center justify-center text-xs text-text-muted">No data.</div>
            )}
          </div>
        </Panel>
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Panel title="Top users">
          <DataTable
            columns={topUserColumns}
            rows={overview.data?.top_users ?? []}
            rowKey={(r) => r.email}
            loading={overview.loading}
            error={overview.error}
            emptyMessage="No user traffic in this range."
            defaultSort={{ key: "down", dir: "desc" }}
          />
        </Panel>
        <Panel title="Top nodes">
          <DataTable
            columns={topNodeColumns}
            rows={overview.data?.top_nodes ?? []}
            rowKey={(r) => r.node}
            loading={overview.loading}
            error={overview.error}
            emptyMessage="No node traffic in this range."
            defaultSort={{ key: "down", dir: "desc" }}
          />
        </Panel>
      </div>
    </div>
  );
}
