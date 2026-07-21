// Cloudflare Access JWT verification: RS256 assertions against the team's
// public keys at /cdn-cgi/access/certs.

export const ACCESS_HEADER = "Cf-Access-Jwt-Assertion";

const KEY_TTL_MS = 60 * 60 * 1000;
const REFRESH_FLOOR_MS = 60 * 1000;
const CLOCK_SKEW_S = 60;

interface Jwks {
  keys: Map<string, CryptoKey>;
  fetchedAt: number;
}

interface AccessHeader {
  alg?: string;
  kid?: string;
}

interface AccessClaims {
  iss?: string;
  aud?: string | string[];
  exp?: number;
  nbf?: number;
  email?: string;
}

const jwksCache = new Map<string, Jwks>();

function decodeSegment(segment: string): Uint8Array {
  const base64 = segment.replace(/-/g, "+").replace(/_/g, "/");
  const binary = atob(base64 + "=".repeat((4 - (base64.length % 4)) % 4));
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}

function decodeJson<T>(segment: string): T {
  return JSON.parse(new TextDecoder().decode(decodeSegment(segment))) as T;
}

async function fetchJwks(url: string): Promise<Jwks> {
  const response = await fetch(url);
  if (!response.ok) {
    throw new Error(`access certs: HTTP ${response.status}`);
  }

  const body = (await response.json()) as { keys?: JsonWebKey[] };
  const keys = new Map<string, CryptoKey>();

  for (const jwk of body.keys ?? []) {
    const kid = (jwk as { kid?: string }).kid;
    if (!kid || jwk.kty !== "RSA") continue;

    keys.set(
      kid,
      await crypto.subtle.importKey("jwk", jwk, { name: "RSASSA-PKCS1-v1_5", hash: "SHA-256" }, false, ["verify"]),
    );
  }

  if (keys.size === 0) {
    throw new Error("access certs: no usable RSA keys");
  }

  return { keys, fetchedAt: Date.now() };
}

// Refetches on expiry, and on an unknown kid at most once per REFRESH_FLOOR_MS
// so forged kids cannot drive the certs endpoint.
async function signingKey(teamDomain: string, kid: string): Promise<CryptoKey> {
  const url = `https://${teamDomain}/cdn-cgi/access/certs`;
  let jwks = jwksCache.get(url);

  const age = jwks ? Date.now() - jwks.fetchedAt : Infinity;
  if (age > KEY_TTL_MS || (!jwks?.keys.has(kid) && age > REFRESH_FLOOR_MS)) {
    jwks = await fetchJwks(url);
    jwksCache.set(url, jwks);
  }

  const key = jwks?.keys.get(kid);
  if (!key) {
    throw new Error(`no Access signing key for kid ${kid}`);
  }

  return key;
}

function audienceMatches(aud: string | string[] | undefined, want: string): boolean {
  if (typeof aud === "string") return aud === want;
  return Array.isArray(aud) && aud.includes(want);
}

/** Verifies an Access assertion and returns the authenticated email. */
export async function verifyAccessJwt(assertion: string, teamDomain: string, audience: string): Promise<string> {
  const parts = assertion.split(".");
  if (parts.length !== 3) {
    throw new Error("malformed assertion");
  }

  const [rawHeader, rawPayload, rawSignature] = parts as [string, string, string];

  const header = decodeJson<AccessHeader>(rawHeader);
  if (header.alg !== "RS256") {
    throw new Error(`unexpected algorithm ${header.alg}`);
  }
  if (!header.kid) {
    throw new Error("assertion has no kid");
  }

  const key = await signingKey(teamDomain, header.kid);
  const signed = new TextEncoder().encode(`${rawHeader}.${rawPayload}`);
  const valid = await crypto.subtle.verify("RSASSA-PKCS1-v1_5", key, decodeSegment(rawSignature), signed);
  if (!valid) {
    throw new Error("signature does not verify");
  }

  const claims = decodeJson<AccessClaims>(rawPayload);
  const now = Math.floor(Date.now() / 1000);

  if (claims.iss !== `https://${teamDomain}`) {
    throw new Error(`unexpected issuer ${claims.iss}`);
  }
  if (!audienceMatches(claims.aud, audience)) {
    throw new Error("audience mismatch");
  }
  if (typeof claims.exp !== "number" || now > claims.exp + CLOCK_SKEW_S) {
    throw new Error("assertion expired");
  }
  if (typeof claims.nbf === "number" && now < claims.nbf - CLOCK_SKEW_S) {
    throw new Error("assertion not yet valid");
  }
  if (!claims.email) {
    throw new Error("assertion carries no email");
  }

  return claims.email;
}
