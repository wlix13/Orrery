import type { ReactNode } from "react";
import { Link } from "../lib/router";

export interface Crumb {
  label: string;
  href?: string;
}

/** Compact trail above detail-page titles: Nodes / main/jpA00 */
export function Breadcrumb({ crumbs }: { crumbs: Crumb[] }) {
  const nodes: ReactNode[] = [];
  crumbs.forEach((crumb, i) => {
    if (i > 0) {
      nodes.push(
        <span key={`sep-${i}`} className="text-text-faint/80">
          /
        </span>,
      );
    }
    nodes.push(
      crumb.href ? (
        <Link key={crumb.label} href={crumb.href} className="hover:text-accent">
          {crumb.label}
        </Link>
      ) : (
        <span key={crumb.label} className="max-w-[20rem] truncate text-text-muted">
          {crumb.label}
        </span>
      ),
    );
  });
  return <nav className="mb-1.5 flex flex-wrap items-center gap-1.5 font-mono text-xs text-text-faint">{nodes}</nav>;
}
