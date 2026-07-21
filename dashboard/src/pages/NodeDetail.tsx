import { useAuth } from "../lib/auth";
import { useApiData } from "../lib/useApiData";
import { useAutoRefresh } from "../lib/useAutoRefresh";
import { useStoredRange } from "../lib/useStoredRange";
import { useFleets } from "../lib/fleets";
import { compareUserIds, dominantNamespace } from "../lib/identity";
import { windowForRange } from "../lib/range";
import { formatBytes, formatDuration, formatRelativeTime } from "../lib/format";
import { COLLECT_HELP, COLLECT_LABEL } from "../lib/glossary";
import type { CollectLevel, Range } from "../api/types";
import type { OrreryClient } from "../api/client";
import { RangePicker } from "../components/RangePicker";
import { RefreshControls } from "../components/RefreshControls";
import { Breadcrumb } from "../components/Breadcrumb";
import { StatCard } from "../components/StatCard";
import { StatusDot } from "../components/StatusDot";
import { UserName } from "../components/UserName";
import { Chart, type ChartSeries } from "../components/Chart";
import { DataTable, type Column } from "../components/DataTable";
import { Panel } from "../components/Panel";
import { Link, userHref } from "../lib/router";

const ENTITY_COLORS = [
  "--color-series-1", "--color-series-2", "--color-series-3", "--color-series-4",
  "--color-series-5", "--color-series-6", "--color-series-7", "--color-series-8",
];

interface EntityChartData {
  timestamps: number[];
  series: ChartSeries[];
}

async function fetchEntityChart(
  client: OrreryClient,
  node: string,
  kind: "inbound" | "outbound",
  range: Range,
): Promise<EntityChartData> {
  const { from, to, step } = windowForRange(range);
  const res = await client.getSeries({ from, to, step, kind, node, agg: "entity" });
  const timestamps = Array.from({ length: Math.round((to - from) / step) }, (_, i) => from + i * step);
  const entities = [...new Set(res.series.map((s) => s.entity).filter((e): e is string => !!e))];
  const series: ChartSeries[] = entities.map((entity, i) => {
    const up = res.series.find((s) => s.entity === entity && s.dir === "up")?.points ?? [];
    const down = res.series.find((s) => s.entity === entity && s.dir === "down")?.points ?? [];
    const combined = timestamps.map((_, idx) => (up[idx] ?? 0) + (down[idx] ?? 0));
    return { label: entity, colorVar: ENTITY_COLORS[i % ENTITY_COLORS.length]!, points: combined };
  });
  return { timestamps, series };
}

function EntityChartPanel({
  title,
  data,
  loading,
  error,
  dimmed,
}: {
  title: string;
  data: EntityChartData | null;
  loading: boolean;
  error: string | null;
  dimmed?: boolean;
}) {
  return (
    <Panel title={title} className={dimmed ? "opacity-70 transition-opacity" : undefined}>
      {loading ? (
        <div className="skeleton h-52 w-full" />
      ) : error ? (
        <div className="flex h-52 items-center justify-center text-sm text-down">{error}</div>
      ) : data && data.series.length > 0 ? (
        <Chart timestamps={data.timestamps} series={data.series} stacked height={208} />
      ) : (
        <div className="flex h-52 items-center justify-center text-sm text-text-muted">No traffic for this range.</div>
      )}
    </Panel>
  );
}

export default function NodeDetail({ nodeKey }: { nodeKey: string }) {
  const { client } = useAuth();
  const { nodeLabel } = useFleets();
  const [range, setRange] = useStoredRange("1h");

  const detail = useApiData(() => client.getNode(nodeKey, range), [client, nodeKey, range]);
  const inboundChart = useApiData(() => fetchEntityChart(client, nodeKey, "inbound", range), [client, nodeKey, range]);
  const outboundChart = useApiData(() => fetchEntityChart(client, nodeKey, "outbound", range), [client, nodeKey, range]);

  const refreshAll = () => {
    detail.refresh();
    inboundChart.refresh();
    outboundChart.refresh();
  };

  useAutoRefresh(refreshAll, 30_000);

  const refreshing = detail.refreshing || inboundChart.refreshing || outboundChart.refreshing;
  const collect = detail.data?.collect as CollectLevel | undefined;

  const entityColumns: Column<{ entity: string; up_bytes: number; down_bytes: number }>[] = [
    { key: "entity", header: "Tag", sort: (a, b) => a.entity.localeCompare(b.entity), render: (r) => r.entity },
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

  // One node means one fleet, so a dominant namespace is unambiguous here.
  const userNamespace = dominantNamespace(detail.data?.users.map((u) => u.email) ?? []);

  const userColumns: Column<{ email: string; up_bytes: number; down_bytes: number }>[] = [
    {
      key: "email",
      header: "User",
      sort: (a, b) => compareUserIds(a.email, b.email),
      render: (r) => (
        <Link href={userHref(r.email)} className="text-accent hover:underline">
          <UserName email={r.email} mainNamespace={userNamespace} />
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

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <Breadcrumb crumbs={[{ label: "Nodes", href: "/nodes" }, { label: nodeLabel(nodeKey) }]} />
          <div className="flex items-center gap-2">
            <h1 className="page-title">{nodeLabel(nodeKey)}</h1>
            {detail.data && <StatusDot status={detail.data.status} />}
          </div>
          <p className="text-xs text-text-muted">{detail.data?.hostname ?? " "}</p>
          <RefreshControls refreshing={refreshing} onRefresh={refreshAll} className="mt-0.5" />
        </div>
        <RangePicker value={range} onChange={setRange} />
      </div>

      {detail.error && (
        <div className="rounded-lg border border-down/30 bg-down/10 px-4 py-3 text-sm text-down">
          Failed to load node: {detail.error}
        </div>
      )}

      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <StatCard
          label="Type / Region"
          loading={detail.loading}
          value={detail.data ? <span className="capitalize">{detail.data.type}</span> : undefined}
          sub={detail.data?.region}
        />
        <StatCard
          label="Uptime"
          loading={detail.loading}
          value={
            detail.data
              ? detail.data.status === "down" || detail.data.status === "off"
                ? "—"
                : formatDuration(detail.data.uptime_s)
              : undefined
          }
          sub={
            collect ? (
              <span
                className="rounded bg-surface-raised px-1.5 py-0.5 text-text-muted"
                title={COLLECT_HELP[collect]}
              >
                collect: {COLLECT_LABEL[collect]}
              </span>
            ) : undefined
          }
        />
        <StatCard
          label="Memory"
          loading={detail.loading}
          value={detail.data ? formatBytes(detail.data.alloc_bytes) : undefined}
          sub={detail.data ? `sys ${formatBytes(detail.data.sys_bytes)}` : undefined}
        />
        <StatCard
          label="Last poll"
          loading={detail.loading}
          value={detail.data ? formatRelativeTime(detail.data.last_ok) : undefined}
          sub={
            detail.data?.last_err ? (
              <span className="block truncate" title={detail.data.last_err}>
                {detail.data.last_err}
              </span>
            ) : undefined
          }
        />
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <EntityChartPanel title="Inbound by tag" data={inboundChart.data} loading={inboundChart.loading} error={inboundChart.error} dimmed={refreshing} />
        <EntityChartPanel title="Outbound by tag" data={outboundChart.data} loading={outboundChart.loading} error={outboundChart.error} dimmed={refreshing} />
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Panel title="Inbound totals">
          <DataTable
            columns={entityColumns}
            rows={detail.data?.inbounds ?? []}
            rowKey={(r) => r.entity}
            loading={detail.loading}
            error={detail.error}
            emptyMessage="No inbound tags."
            defaultSort={{ key: "down", dir: "desc" }}
          />
        </Panel>
        <Panel title="Outbound totals">
          <DataTable
            columns={entityColumns}
            rows={detail.data?.outbounds ?? []}
            rowKey={(r) => r.entity}
            loading={detail.loading}
            error={detail.error}
            emptyMessage="No outbound tags."
            defaultSort={{ key: "down", dir: "desc" }}
          />
        </Panel>
      </div>

      <Panel title={detail.data?.type === "exit" ? "Per-hub attribution" : "Users"}>
        <DataTable
          columns={userColumns}
          rows={detail.data?.users ?? []}
          rowKey={(r) => r.email}
          loading={detail.loading}
          error={detail.error}
          emptyMessage={
            detail.data?.collect === "off" ? "Collection disabled for this node." : "No per-user data collected."
          }
          defaultSort={{ key: "down", dir: "desc" }}
        />
      </Panel>

      <Panel title="System stats">
        {detail.loading ? (
          <div className="skeleton h-16 w-full" />
        ) : detail.data ? (
          <dl className="grid grid-cols-2 gap-4 text-sm sm:grid-cols-4">
            <div>
              <dt className="micro-label">Goroutines</dt>
              <dd className="tabular-nums text-text">{detail.data.num_goroutine}</dd>
            </div>
            <div>
              <dt className="micro-label">GC cycles</dt>
              <dd className="tabular-nums text-text">{detail.data.num_gc}</dd>
            </div>
            <div>
              <dt className="micro-label">Alloc</dt>
              <dd className="tabular-nums text-text">{formatBytes(detail.data.alloc_bytes)}</dd>
            </div>
            <div>
              <dt className="micro-label">Sys</dt>
              <dd className="tabular-nums text-text">{formatBytes(detail.data.sys_bytes)}</dd>
            </div>
          </dl>
        ) : (
          <div className="text-sm text-text-muted">Unavailable.</div>
        )}
      </Panel>
    </div>
  );
}
