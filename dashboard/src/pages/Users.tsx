import { useMemo, useState } from "react";
import { useAuth } from "../lib/auth";
import { useApiData } from "../lib/useApiData";
import { useAutoRefresh } from "../lib/useAutoRefresh";
import { RANGE_KEY_USERS, useStoredRange } from "../lib/useStoredRange";
import { useFleets } from "../lib/fleets";
import { compareUserIds, dominantNamespace } from "../lib/identity";
import { formatBytes, formatRelativeTime } from "../lib/format";
import type { SeenWindow, UserRow } from "../api/types";
import { RangePicker } from "../components/RangePicker";
import { SeenPicker } from "../components/SeenPicker";
import { RefreshControls } from "../components/RefreshControls";
import { UserName } from "../components/UserName";
import { DataTable, type Column } from "../components/DataTable";
import { Link, nodeHref, userHref, useRouter } from "../lib/router";

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

export default function Users() {
  const { client } = useAuth();
  const { nodeLabel, soleFleet } = useFleets();
  const { navigate } = useRouter();
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

  const users = useApiData(() => client.getUsers(range, seen), [client, range, seen]);

  useAutoRefresh(() => users.refresh(), 30_000);

  // One namespace convention only holds within a single fleet.
  const mainNamespace = useMemo(
    () => (soleFleet === null ? null : dominantNamespace((users.data ?? []).map((u) => u.email))),
    [users.data, soleFleet],
  );

  const columns: Column<UserRow>[] = [
    {
      key: "email",
      header: "User",
      sort: (a, b) => compareUserIds(a.email, b.email),
      render: (r) => (
        <div className="flex items-center gap-2">
          <span
            className={`inline-block h-2 w-2 shrink-0 rounded-full ${r.online_now ? "bg-up" : "bg-border"}`}
            aria-hidden
          />
          <Link href={userHref(r.email)} className="font-medium text-accent hover:underline">
            <UserName email={r.email} mainNamespace={mainNamespace} />
          </Link>
        </div>
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
    {
      key: "online",
      header: "Online",
      sort: (a, b) => Number(a.online_now) - Number(b.online_now),
      render: (r) =>
        r.online_now ? (
          <span className="rounded-full bg-up/15 px-2 py-0.5 text-xs font-medium text-up">online</span>
        ) : (
          <span className="text-xs text-text-faint">offline</span>
        ),
    },
    {
      key: "hubs",
      header: (
        <span title={`Hubs with traffic or online presence in the last ${seen}. Sorted by most recent.`}>
          Hubs ({seen})
        </span>
      ),
      sort: (a, b) => a.hubs.length - b.hubs.length,
      render: (r) =>
        r.hubs.length === 0 ? (
          <span className="text-xs text-text-faint">—</span>
        ) : (
          <div className="flex flex-wrap items-center gap-1">
            {r.hubs.map((h) => (
              <Link
                key={h.node}
                href={nodeHref(h.node)}
                onClick={(e) => e.stopPropagation()}
                title={`Last seen ${formatRelativeTime(h.last_seen)}`}
                className="inline-flex items-center gap-1 rounded bg-surface-raised px-1.5 py-0.5 text-xs text-text-muted hover:text-accent"
              >
                <span>{nodeLabel(h.node)}</span>
                <span className="font-mono text-[0.65rem] text-text-faint">{formatRelativeTime(h.last_seen)}</span>
              </Link>
            ))}
          </div>
        ),
    },
  ];

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="page-title">Users</h1>
          <p className="text-xs text-text-muted">
            {users.data
              ? `${users.data.length} users · traffic over ${range} · hubs active in last ${seen}` +
                (mainNamespace ? ` · @${mainNamespace} implied` : "")
              : " "}
          </p>
          <RefreshControls refreshing={users.refreshing} onRefresh={() => users.refresh()} className="mt-0.5" />
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <SeenPicker value={seen} onChange={setSeen} />
          <RangePicker value={range} onChange={setRange} />
        </div>
      </div>

      <DataTable
        columns={columns}
        rows={users.data ?? []}
        rowKey={(r) => r.email}
        loading={users.loading}
        error={users.error}
        onRowClick={(r) => navigate(userHref(r.email))}
        emptyMessage="No user activity in this range."
        skeletonRows={8}
        defaultSort={{ key: "down", dir: "desc" }}
      />
    </div>
  );
}
