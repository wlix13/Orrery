import { useState, type FormEvent } from "react";
import { useAuth } from "../lib/auth";
import { useTheme } from "../lib/theme";

/** Centered gate when no token is configured. "mock" enables the in-memory backend. */
export function TokenGate() {
  const { setCredentials } = useAuth();
  const { theme, toggleTheme } = useTheme();
  const [token, setToken] = useState("");
  const [apiBase, setApiBase] = useState("");

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (!token.trim()) return;
    setCredentials(token.trim(), apiBase.trim());
  };

  return (
    <div className="relative flex min-h-screen items-center justify-center p-4">
      <button
        type="button"
        onClick={toggleTheme}
        className="absolute top-4 right-4 rounded-md border border-border/80 bg-surface/80 px-2.5 py-1 text-xs text-text-muted backdrop-blur-sm hover:text-text"
        aria-label="Toggle theme"
      >
        {theme === "dark" ? "Light" : "Dark"}
      </button>

      <form
        onSubmit={handleSubmit}
        className="w-full max-w-sm rounded-2xl border border-border/80 bg-surface/95 p-7 shadow-[inset_0_1px_0_0_color-mix(in_srgb,var(--color-text)_5%,transparent)]"
      >
        <div className="mb-5 flex items-start gap-3">
          <svg width="28" height="28" viewBox="0 0 24 24" className="mt-0.5 shrink-0 text-accent" aria-hidden>
            <circle cx="12" cy="12" r="2.2" fill="currentColor" />
            <g className="brand-orbit origin-center" style={{ transformOrigin: "12px 12px" }}>
              <circle cx="12" cy="12" r="8" fill="none" stroke="currentColor" strokeOpacity="0.35" strokeWidth="1" />
              <circle cx="12" cy="4" r="1.6" fill="currentColor" fillOpacity="0.85" />
            </g>
          </svg>
          <div>
            <h1 className="text-xl font-semibold tracking-tight text-text">Orrery</h1>
            <p className="mt-1 text-sm text-text-muted">
              Fleet metrics for the Conglomerate proxy mesh. Enter a collector token, or{" "}
              <code className="rounded bg-surface-raised px-1 py-0.5 font-mono text-xs text-accent">mock</code> to
              explore.
            </p>
          </div>
        </div>

        <label className="mb-3 block">
          <span className="micro-label mb-1.5 block">API token</span>
          <input
            type="password"
            autoFocus
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder="Bearer token"
            className="w-full rounded-lg border border-border bg-bg/60 px-3 py-2.5 font-mono text-sm text-text outline-none transition-colors focus:border-accent"
          />
        </label>

        <label className="mb-6 block">
          <span className="micro-label mb-1.5 block">API base URL</span>
          <input
            type="text"
            value={apiBase}
            onChange={(e) => setApiBase(e.target.value)}
            placeholder="(empty = same origin)"
            className="w-full rounded-lg border border-border bg-bg/60 px-3 py-2.5 font-mono text-sm text-text outline-none transition-colors focus:border-accent"
          />
        </label>

        <button
          type="submit"
          disabled={!token.trim()}
          className="w-full rounded-lg bg-accent px-3 py-2.5 text-sm font-semibold text-white transition-colors hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-40"
        >
          Continue
        </button>
      </form>
    </div>
  );
}
