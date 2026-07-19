import { beforeAll, beforeEach, expect, test, vi } from "vitest";
import worker, { type Env } from "./worker";
import { ACCESS_HEADER } from "./access";
import { AUD, CERTS_URL, TEAM, generateKeys, mint, publicJwk, validClaims, type AccessJwk } from "./testkit";

const COLLECTOR = "https://collector.example.internal:9800";

let keys: CryptoKeyPair;
let jwks: { keys: AccessJwk[] };
let upstream: Request[];

// Echoes what the collector would return, and records what reached it.
function stubFetch() {
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = input instanceof Request ? input : new Request(String(input), init);
    if (request.url === CERTS_URL) {
      return new Response(JSON.stringify(jwks), { headers: { "content-type": "application/json" } });
    }

    upstream.push(request);
    return new Response(JSON.stringify({ name: "ops", method: "token", fleets: ["main"] }), {
      headers: { "content-type": "application/json" },
    });
  });
}

function env(overrides: Partial<Env> = {}): Env {
  return {
    ASSETS: { fetch: async () => new Response("asset") } as unknown as Fetcher,
    COLLECTOR_URL: COLLECTOR,
    COLLECTOR_TOKEN: "default-token",
    ...overrides,
  };
}

function call(e: Env, headers: Record<string, string> = {}, path = "/api/me"): Promise<Response> {
  return worker.fetch(new Request(`https://dash.example.com${path}`, { headers }), e);
}

async function accessHeader(email: string): Promise<Record<string, string>> {
  return { [ACCESS_HEADER]: await mint(keys.privateKey, validClaims({ email })) };
}

const withAccess = (overrides: Partial<Env> = {}) => env({ ACCESS_TEAM_DOMAIN: TEAM, ACCESS_AUD: AUD, ...overrides });

beforeAll(async () => {
  keys = await generateKeys();
  jwks = { keys: [await publicJwk(keys, "key-1")] };
});

beforeEach(() => {
  upstream = [];
  stubFetch();
});

test("serves nobody when no credential is configured", async () => {
  const res = await call(env({ ACCESS_TEAM_DOMAIN: "", ACCESS_AUD: "" }));
  expect(res.status).toBe(500);
  expect(upstream).toHaveLength(0);
});

test("rejects a wrong bearer token", async () => {
  const res = await call(env({ DASHBOARD_TOKEN: "right" }), { Authorization: "Bearer wrong" });
  expect(res.status).toBe(401);
  expect(upstream).toHaveLength(0);
});

test("swaps a valid dashboard token for the collector token", async () => {
  const res = await call(env({ DASHBOARD_TOKEN: "right" }), { Authorization: "Bearer right" });
  expect(res.status).toBe(200);
  expect(upstream[0]!.headers.get("Authorization")).toBe("Bearer default-token");
  expect(upstream[0]!.url).toBe(`${COLLECTOR}/api/me`);
});

// A broken assertion is a signal, not a shrug: a valid bearer token alongside
// it must not rescue the request.
test("rejects an invalid Access assertion instead of falling through", async () => {
  const e = withAccess({ DASHBOARD_TOKEN: "right" });
  const res = await call(e, { [ACCESS_HEADER]: "not.a.jwt", Authorization: "Bearer right" });

  expect(res.status).toBe(401);
  expect(upstream).toHaveLength(0);
});

test("keeps the dashboard token usable alongside Access", async () => {
  const res = await call(withAccess({ DASHBOARD_TOKEN: "right" }), { Authorization: "Bearer right" });
  expect(res.status).toBe(200);
});

test("maps an Access identity to its own collector token", async () => {
  const e = withAccess({ COLLECTOR_TOKENS: JSON.stringify({ "CaaS@Example.com": "caas-token" }) });
  const res = await call(e, await accessHeader("caas@example.com"));

  expect(res.status).toBe(200);
  expect(upstream[0]!.headers.get("Authorization")).toBe("Bearer caas-token");
});

test("falls back to COLLECTOR_TOKEN for an unmapped identity", async () => {
  const e = withAccess({ COLLECTOR_TOKENS: JSON.stringify({ "caas@example.com": "caas-token" }) });
  await call(e, await accessHeader("ops@example.com"));

  expect(upstream[0]!.headers.get("Authorization")).toBe("Bearer default-token");
});

test("refuses an unmapped identity when there is no fallback token", async () => {
  const e = withAccess({ COLLECTOR_TOKEN: "", COLLECTOR_TOKENS: JSON.stringify({ "caas@example.com": "caas-token" }) });
  const res = await call(e, await accessHeader("ops@example.com"));

  expect(res.status).toBe(403);
  expect(upstream).toHaveLength(0);
});

test("fails closed on an unparseable token map", async () => {
  const res = await call(withAccess({ COLLECTOR_TOKENS: "{oops" }), await accessHeader("ops@example.com"));

  expect(res.status).toBe(500);
  expect(upstream).toHaveLength(0);
});

test("never forwards the visitor's identity upstream", async () => {
  const headers = { ...(await accessHeader("ops@example.com")), Cookie: "CF_Authorization=x" };
  await call(withAccess(), headers);

  expect(upstream[0]!.headers.get(ACCESS_HEADER)).toBeNull();
  expect(upstream[0]!.headers.get("Cf-Access-Authenticated-User-Email")).toBeNull();
  expect(upstream[0]!.headers.get("Cookie")).toBeNull();
});

test("names the Access visitor in /api/me but keeps the collector's scope", async () => {
  const res = await call(withAccess(), await accessHeader("ops@example.com"));

  expect(await res.json()).toEqual({ name: "ops@example.com", method: "cloudflare_access", fleets: ["main"] });
});

test("leaves other routes untouched", async () => {
  const res = await call(withAccess(), await accessHeader("ops@example.com"), "/api/nodes");

  expect(await res.json()).toEqual({ name: "ops", method: "token", fleets: ["main"] });
});
