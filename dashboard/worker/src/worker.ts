// Cloudflare Worker: thin auth proxy in front of the collector's HTTP API.
//
// Only /api/* reaches this fetch handler (wrangler.jsonc's run_worker_first);
// everything else is served as a static asset (or the SPA fallback). For
// /api/*, we:
//   1. authenticate the visitor - a verified Cloudflare Access assertion, or a
//      bearer token matching DASHBOARD_TOKEN,
//   2. swap that for a collector token before forwarding to COLLECTOR_URL,
//   3. stamp Cache-Control: no-store on the response.

import { ACCESS_HEADER, verifyAccessJwt } from "./access";

export interface Env {
  ASSETS: Fetcher;
  COLLECTOR_URL: string;
  COLLECTOR_TOKEN: string;
  // JSON object mapping an Access email to the collector token used on its behalf.
  COLLECTOR_TOKENS?: string;
  DASHBOARD_TOKEN?: string;
  ACCESS_TEAM_DOMAIN?: string;
  ACCESS_AUD?: string;
}

// email is set when Cloudflare Access authenticated the request.
type Auth = { ok: true; email: string | null } | { ok: false; response: Response };

// Matches the collector's error shape so the SPA parses both the same way.
function jsonError(code: string, message: string, status: number): Response {
  return new Response(JSON.stringify({ error: { code, message } }), {
    status,
    headers: { "content-type": "application/json", "cache-control": "no-store" },
  });
}

// Best-effort constant-time compare, mirroring the collector's bearer-token check.
function timingSafeEqual(a: string, b: string): boolean {
  const aBytes = new TextEncoder().encode(a);
  const bBytes = new TextEncoder().encode(b);
  const len = Math.max(aBytes.length, bBytes.length);
  let diff = aBytes.length ^ bBytes.length;
  for (let i = 0; i < len; i++) {
    diff |= (aBytes[i] ?? 0) ^ (bBytes[i] ?? 0);
  }
  return diff === 0;
}

function bearerToken(request: Request): string | null {
  const header = request.headers.get("Authorization") ?? "";
  const match = /^Bearer (.+)$/.exec(header);
  return match ? match[1]! : null;
}

// Access first when configured, then DASHBOARD_TOKEN; neither configured serves nobody.
async function authenticate(request: Request, env: Env): Promise<Auth> {
  const accessConfigured = Boolean(env.ACCESS_TEAM_DOMAIN && env.ACCESS_AUD);
  if (!accessConfigured && !env.DASHBOARD_TOKEN) {
    return {
      ok: false,
      response: jsonError("server_misconfigured", "neither DASHBOARD_TOKEN nor Cloudflare Access is configured", 500),
    };
  }

  const assertion = request.headers.get(ACCESS_HEADER);
  if (accessConfigured && assertion) {
    try {
      return { ok: true, email: await verifyAccessJwt(assertion, env.ACCESS_TEAM_DOMAIN!, env.ACCESS_AUD!) };
    } catch (err) {
      console.log("access assertion rejected:", err instanceof Error ? err.message : err);
      return { ok: false, response: jsonError("unauthorized", "invalid Cloudflare Access assertion", 401) };
    }
  }

  const provided = env.DASHBOARD_TOKEN ? bearerToken(request) : null;
  if (!provided || !timingSafeEqual(provided, env.DASHBOARD_TOKEN!)) {
    return { ok: false, response: jsonError("unauthorized", "missing or invalid credentials", 401) };
  }

  return { ok: true, email: null };
}

// Null when COLLECTOR_TOKENS is set but unparseable, so a bad mapping fails the
// request instead of falling back to the default token.
function tokenMap(env: Env): Record<string, string> | null {
  if (!env.COLLECTOR_TOKENS) return {};

  try {
    const parsed = JSON.parse(env.COLLECTOR_TOKENS) as Record<string, string>;
    return Object.fromEntries(Object.entries(parsed).map(([email, token]) => [email.toLowerCase(), token]));
  } catch {
    return null;
  }
}

// Names the Access visitor in /api/me; the fleet scope stays the collector's answer.
async function meWithIdentity(upstream: Response, email: string): Promise<Response> {
  const body = (await upstream.json()) as Record<string, unknown>;
  return new Response(JSON.stringify({ ...body, name: email, method: "cloudflare_access" }), {
    status: upstream.status,
    headers: { "content-type": "application/json", "cache-control": "no-store" },
  });
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);

    if (!url.pathname.startsWith("/api/")) {
      return env.ASSETS.fetch(request);
    }

    const auth = await authenticate(request, env);
    if (!auth.ok) return auth.response;

    if (!env.COLLECTOR_URL) {
      return jsonError("server_misconfigured", "COLLECTOR_URL is not configured", 500);
    }

    const tokens = tokenMap(env);
    if (!tokens) {
      return jsonError("server_misconfigured", "COLLECTOR_TOKENS is not valid JSON", 500);
    }

    const collectorToken = (auth.email ? tokens[auth.email.toLowerCase()] : undefined) ?? env.COLLECTOR_TOKEN;
    if (!collectorToken) {
      return jsonError("forbidden", "no collector token is mapped to this identity", 403);
    }

    // Raw path+query, so percent-encoding survives URL.pathname's decode-on-read.
    const pathAndQuery = url.href.slice(url.origin.length);
    const base = env.COLLECTOR_URL.replace(/\/+$/, "");
    const upstreamUrl = new URL(pathAndQuery, `${base}/`);

    const upstreamHeaders = new Headers(request.headers);
    upstreamHeaders.set("Authorization", `Bearer ${collectorToken}`);
    upstreamHeaders.delete("Cookie"); // no cookie auth upstream
    // The Worker speaks to the collector as itself; visitor identity stops here.
    upstreamHeaders.delete(ACCESS_HEADER);
    upstreamHeaders.delete("Cf-Access-Authenticated-User-Email");

    let upstreamResponse: Response;
    try {
      upstreamResponse = await fetch(
        new Request(upstreamUrl.href, {
          method: request.method,
          headers: upstreamHeaders,
          body: request.method === "GET" || request.method === "HEAD" ? undefined : request.body,
          redirect: "manual",
        }),
      );
    } catch (err) {
      return jsonError("upstream_unreachable", err instanceof Error ? err.message : "collector unreachable", 502);
    }

    if (auth.email && url.pathname === "/api/me" && upstreamResponse.ok) {
      return meWithIdentity(upstreamResponse, auth.email);
    }

    const responseHeaders = new Headers(upstreamResponse.headers);
    responseHeaders.set("Cache-Control", "no-store");

    return new Response(upstreamResponse.body, {
      status: upstreamResponse.status,
      statusText: upstreamResponse.statusText,
      headers: responseHeaders,
    });
  },
} satisfies ExportedHandler<Env>;
