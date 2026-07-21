// Fetch wrapper for the Orrery HTTP API. Base URL + Bearer token come from
// localStorage (set via the Settings page / token gate). Errors are parsed
// per the API contract: {"error":{"code":"...","message":"..."}}.

import type {
  Me,
  NodeDetail,
  NodeKey,
  NodeRow,
  OnlineNodeUsers,
  Overview,
  Range,
  SeriesQuery,
  SeenWindow,
  SeriesResponse,
  UserDetail,
  UserRow,
} from "./types";
import { ApiRequestError } from "./types";

export const STORAGE_KEYS = {
  token: "orrery.token",
  apiBase: "orrery.apiBase",
  theme: "orrery.theme",
} as const;

export function getStoredToken(): string {
  return localStorage.getItem(STORAGE_KEYS.token) ?? "";
}

export function getStoredApiBase(): string {
  return localStorage.getItem(STORAGE_KEYS.apiBase) ?? "";
}

export function setStoredCredentials(token: string, apiBase: string): void {
  localStorage.setItem(STORAGE_KEYS.token, token);
  localStorage.setItem(STORAGE_KEYS.apiBase, apiBase);
}

export function clearStoredCredentials(): void {
  localStorage.removeItem(STORAGE_KEYS.token);
  localStorage.removeItem(STORAGE_KEYS.apiBase);
}

export function isMockToken(token: string): boolean {
  return token === "mock";
}

/** True when the app should run entirely against the in-memory mock backend. */
export function mockModeEnabled(): boolean {
  return import.meta.env.VITE_MOCK === "1" || isMockToken(getStoredToken());
}

/** Interface implemented by both the real fetch-based client and api/mock.ts. */
export interface OrreryClient {
  getMe(): Promise<Me>;
  getOverview(range: Range): Promise<Overview>;
  getNodes(): Promise<NodeRow[]>;
  getNode(node: NodeKey, range: Range): Promise<NodeDetail>;
  getSeries(query: SeriesQuery): Promise<SeriesResponse>;
  getUsers(range: Range, seen: SeenWindow, fleet?: string): Promise<UserRow[]>;
  getUser(email: string, range: Range, seen: SeenWindow): Promise<UserDetail>;
  getOnline(): Promise<OnlineNodeUsers[]>;
}

function buildQuery(params: Record<string, string | number | boolean | undefined>): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === "") continue;
    search.set(key, String(value));
  }
  const qs = search.toString();
  return qs ? `?${qs}` : "";
}

async function request<T>(path: string): Promise<T> {
  const base = getStoredApiBase().replace(/\/+$/, "");
  const token = getStoredToken();
  const url = `${base}${path}`;

  let res: Response;
  try {
    res = await fetch(url, {
      headers: token ? { Authorization: `Bearer ${token}` } : {},
      // Carries the Cloudflare Access cookie when Access fronts the origin.
      credentials: "include",
    });
  } catch (err) {
    throw new ApiRequestError(
      "network_error",
      err instanceof Error ? err.message : "network request failed",
      0,
    );
  }

  if (!res.ok) {
    let code = "unknown_error";
    let message = `request failed with status ${res.status}`;
    try {
      const body = (await res.json()) as { error?: { code?: string; message?: string } };
      if (body.error) {
        code = body.error.code ?? code;
        message = body.error.message ?? message;
      }
    } catch {
      // response body wasn't JSON; keep the generic message
    }
    throw new ApiRequestError(code, message, res.status);
  }

  return (await res.json()) as T;
}

/** Real HTTP implementation of {@link OrreryClient}. */
export const httpClient: OrreryClient = {
  getMe() {
    return request(`/api/me`);
  },
  getOverview(range) {
    return request(`/api/overview${buildQuery({ range })}`);
  },
  getNodes() {
    return request(`/api/nodes`);
  },
  getNode(node, range) {
    // Node keys are "<fleet>/<id>" and the server routes them as two path
    // segments - encode each segment, keep the slash literal.
    const path = node.split("/").map(encodeURIComponent).join("/");
    return request(`/api/nodes/${path}${buildQuery({ range })}`);
  },
  getSeries(query) {
    const { from, to, step, kind, node, fleet, type, entity, dir, agg } = query;
    return request(
      `/api/series${buildQuery({ from, to, step, kind, node, fleet, type, entity, dir, agg })}`,
    );
  },
  getUsers(range, seen, fleet) {
    return request(`/api/users${buildQuery({ range, seen, fleet })}`);
  },
  getUser(email, range, seen) {
    return request(`/api/users/${encodeURIComponent(email)}${buildQuery({ range, seen })}`);
  },
  getOnline() {
    return request(`/api/online`);
  },
};
