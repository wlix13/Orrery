import { useMemo, useState, type ReactNode } from "react";

export type SortDir = "asc" | "desc";

export interface Column<T> {
  key: string;
  header: ReactNode;
  render: (row: T) => ReactNode;
  align?: "left" | "right";
  /** Applies tabular-nums for consistent digit alignment. */
  numeric?: boolean;
  className?: string;
  /** When set, header is clickable and sorts by this comparator. */
  sort?: (a: T, b: T) => number;
}

interface DataTableProps<T> {
  columns: Column<T>[];
  rows: T[];
  rowKey: (row: T) => string;
  loading?: boolean;
  error?: string | null;
  emptyMessage?: string;
  onRowClick?: (row: T) => void;
  skeletonRows?: number;
  /** Initial sort column key + direction. */
  defaultSort?: { key: string; dir: SortDir };
}

export function DataTable<T>({
  columns,
  rows,
  rowKey,
  loading,
  error,
  emptyMessage = "No data yet.",
  onRowClick,
  skeletonRows = 5,
  defaultSort,
}: DataTableProps<T>) {
  const [sortKey, setSortKey] = useState<string | null>(defaultSort?.key ?? null);
  const [sortDir, setSortDir] = useState<SortDir>(defaultSort?.dir ?? "desc");

  const sortedRows = useMemo(() => {
    if (!sortKey) return rows;
    const col = columns.find((c) => c.key === sortKey);
    if (!col?.sort) return rows;
    const copy = rows.slice();
    copy.sort((a, b) => {
      const cmp = col.sort!(a, b);
      return sortDir === "asc" ? cmp : -cmp;
    });
    return copy;
  }, [rows, columns, sortKey, sortDir]);

  const toggleSort = (key: string) => {
    if (sortKey === key) {
      setSortDir((d) => (d === "asc" ? "desc" : "asc"));
    } else {
      setSortKey(key);
      setSortDir("desc");
    }
  };

  return (
    <div className="overflow-x-auto rounded-lg border border-border/70">
      <table className="w-full min-w-max border-collapse text-sm">
        <thead>
          <tr className="border-b border-border/70 bg-surface-raised/80">
            {columns.map((col) => {
              const sortable = !!col.sort;
              const active = sortKey === col.key;
              return (
                <th
                  key={col.key}
                  className={`micro-label px-3 py-2.5 ${col.align === "right" ? "text-right" : "text-left"} ${col.className ?? ""} ${
                    sortable ? "cursor-pointer select-none hover:text-text-muted" : ""
                  }`}
                  onClick={sortable ? () => toggleSort(col.key) : undefined}
                  aria-sort={active ? (sortDir === "asc" ? "ascending" : "descending") : undefined}
                >
                  <span className="inline-flex items-center gap-1">
                    {col.header}
                    {active && <span className="font-mono text-accent">{sortDir === "asc" ? "↑" : "↓"}</span>}
                  </span>
                </th>
              );
            })}
          </tr>
        </thead>
        <tbody>
          {loading &&
            Array.from({ length: skeletonRows }).map((_, i) => (
              <tr key={`skeleton-${i}`} className="border-b border-border last:border-b-0">
                {columns.map((col) => (
                  <td key={col.key} className="px-3 py-2.5">
                    <div className="skeleton h-4 w-full max-w-24" />
                  </td>
                ))}
              </tr>
            ))}

          {!loading && error && (
            <tr>
              <td colSpan={columns.length} className="px-3 py-6 text-center text-sm text-down">
                {error}
              </td>
            </tr>
          )}

          {!loading && !error && sortedRows.length === 0 && (
            <tr>
              <td colSpan={columns.length} className="px-3 py-6 text-center text-sm text-text-muted">
                {emptyMessage}
              </td>
            </tr>
          )}

          {!loading &&
            !error &&
            sortedRows.map((row) => (
              <tr
                key={rowKey(row)}
                onClick={onRowClick ? () => onRowClick(row) : undefined}
                onKeyDown={
                  onRowClick
                    ? (e) => {
                        if (e.key === "Enter" || e.key === " ") {
                          e.preventDefault();
                          onRowClick(row);
                        }
                      }
                    : undefined
                }
                tabIndex={onRowClick ? 0 : undefined}
                role={onRowClick ? "link" : undefined}
                className={`border-b border-border bg-surface last:border-b-0 ${
                  onRowClick ? "cursor-pointer hover:bg-surface-raised focus:bg-surface-raised focus:outline-none" : ""
                }`}
              >
                {columns.map((col) => (
                  <td
                    key={col.key}
                    className={`px-3 py-2.5 text-text ${col.align === "right" ? "text-right" : "text-left"} ${
                      col.numeric ? "tabular-nums" : ""
                    } ${col.className ?? ""}`}
                  >
                    {col.render(row)}
                  </td>
                ))}
              </tr>
            ))}
        </tbody>
      </table>
    </div>
  );
}
