import { useState, type FormEvent } from "react";
import { useAuth } from "../lib/auth";
import { useTheme } from "../lib/theme";
import { httpClient, isMockToken } from "../api/client";
import { mockClient } from "../api/mock";
import { Panel } from "../components/Panel";

const forcedMock = import.meta.env.VITE_MOCK === "1";

export default function Settings() {
  const { token, apiBase, setCredentials, isMock } = useAuth();
  const { theme, toggleTheme } = useTheme();
  const [tokenInput, setTokenInput] = useState(token);
  const [apiBaseInput, setApiBaseInput] = useState(apiBase);
  const [saved, setSaved] = useState(false);
  const [health, setHealth] = useState<string | null>(null);
  const [healthOk, setHealthOk] = useState<boolean | null>(null);
  const [checking, setChecking] = useState(false);

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    const nextToken = tokenInput.trim();
    const nextBase = apiBaseInput.trim();
    setCredentials(nextToken, nextBase);
    setSaved(true);
    window.setTimeout(() => setSaved(false), 2000);

    setChecking(true);
    setHealth(null);
    setHealthOk(null);
    try {
      const probe = forcedMock || isMockToken(nextToken) ? mockClient : httpClient;
      const ov = await probe.getOverview("1h");
      setHealthOk(true);
      setHealth(`API OK - ${ov.nodes.up}/${ov.nodes.total} nodes up, ${ov.online_users} online.`);
    } catch (err) {
      setHealthOk(false);
      setHealth(err instanceof Error ? err.message : "API check failed");
    } finally {
      setChecking(false);
    }
  };

  return (
    <div className="flex max-w-xl flex-col gap-5">
      <div>
        <h1 className="page-title">Settings</h1>
        <p className="text-xs text-text-muted">API connection and appearance, stored locally in this browser.</p>
      </div>

      <Panel title="API connection">
        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          <label className="block">
            <span className="micro-label mb-1 block">API token</span>
            <input
              type="password"
              value={tokenInput}
              onChange={(e) => setTokenInput(e.target.value)}
              placeholder="Bearer token (or 'mock')"
              className="w-full rounded-lg border border-border bg-bg/60 px-3 py-2.5 font-mono text-sm text-text outline-none focus:border-accent"
            />
            {isMock && <span className="mt-1 block text-xs text-accent">Mock mode is active.</span>}
          </label>

          <label className="block">
            <span className="micro-label mb-1 block">API base URL</span>
            <input
              type="text"
              value={apiBaseInput}
              onChange={(e) => setApiBaseInput(e.target.value)}
              placeholder="(empty = same origin, e.g. self-hosted)"
              className="w-full rounded-lg border border-border bg-bg/60 px-3 py-2.5 font-mono text-sm text-text outline-none focus:border-accent"
            />
          </label>

          <div className="flex flex-wrap items-center gap-3">
            <button
              type="submit"
              disabled={checking}
              className="rounded-md bg-accent px-3 py-2 text-sm font-semibold text-white transition-colors hover:bg-accent-hover disabled:opacity-50"
            >
              {checking ? "Checking…" : "Save"}
            </button>
            {saved && <span className="text-xs text-up">Saved.</span>}
          </div>

          {health && <p className={`text-xs ${healthOk ? "text-up" : "text-down"}`}>{health}</p>}
        </form>
      </Panel>

      <Panel title="Appearance">
        <div className="flex items-center justify-between">
          <div>
            <div className="text-sm text-text">Theme</div>
            <div className="text-xs text-text-muted">Currently {theme}.</div>
          </div>
          <button
            type="button"
            onClick={toggleTheme}
            className="rounded-md border border-border px-3 py-1.5 text-sm text-text-muted hover:bg-surface-raised hover:text-text"
          >
            Switch to {theme === "dark" ? "light" : "dark"}
          </button>
        </div>
      </Panel>
    </div>
  );
}
