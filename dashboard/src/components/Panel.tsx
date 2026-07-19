import type { ReactNode } from "react";

interface PanelProps {
  title: string;
  action?: ReactNode;
  children: ReactNode;
  className?: string;
}

/** Section chrome for charts/tables - title-weight hierarchy, quiet border. */
export function Panel({ title, action, children, className }: PanelProps) {
  return (
    <section
      className={`rounded-xl border border-border/80 bg-surface/90 p-4 shadow-[inset_0_1px_0_0_color-mix(in_srgb,var(--color-text)_4%,transparent)] ${className ?? ""}`}
    >
      <div className="mb-3 flex items-baseline justify-between gap-3">
        <h2 className="text-sm font-medium tracking-tight text-text">{title}</h2>
        {action}
      </div>
      {children}
    </section>
  );
}
