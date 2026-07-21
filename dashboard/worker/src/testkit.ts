// Mints Cloudflare Access assertions and serves a matching JWKS, so tests can
// exercise the real verifier instead of stubbing it out.

export const TEAM = "orrery.cloudflareaccess.com";
export const AUD = "aud-tag";
export const CERTS_URL = `https://${TEAM}/cdn-cgi/access/certs`;

export const RS256 = { name: "RSASSA-PKCS1-v1_5", hash: "SHA-256" } as const;

export type AccessJwk = JsonWebKey & { kid: string };

export interface Claims {
  iss?: string;
  aud?: string | string[];
  exp?: number;
  nbf?: number;
  email?: string;
}

export interface MintOptions {
  kid?: string;
  alg?: string;
}

export function b64url(bytes: Uint8Array | string): string {
  const binary = typeof bytes === "string" ? bytes : String.fromCharCode(...bytes);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

export async function generateKeys(): Promise<CryptoKeyPair> {
  const params = { ...RS256, modulusLength: 2048, publicExponent: new Uint8Array([1, 0, 1]) };
  return (await crypto.subtle.generateKey(params, true, ["sign", "verify"])) as CryptoKeyPair;
}

export async function publicJwk(pair: CryptoKeyPair, kid: string): Promise<AccessJwk> {
  const jwk = (await crypto.subtle.exportKey("jwk", pair.publicKey)) as JsonWebKey;
  return { ...jwk, kid, alg: "RS256", use: "sig" } as AccessJwk;
}

export function validClaims(overrides: Claims = {}): Claims {
  const now = Math.floor(Date.now() / 1000);
  return { iss: `https://${TEAM}`, aud: AUD, exp: now + 300, nbf: now - 10, email: "ops@example.com", ...overrides };
}

export async function mint(key: CryptoKey, claims: Claims, opts: MintOptions = {}): Promise<string> {
  const header = b64url(JSON.stringify({ alg: opts.alg ?? "RS256", kid: opts.kid ?? "key-1", typ: "JWT" }));
  const payload = b64url(JSON.stringify(claims));
  const signature = await crypto.subtle.sign(RS256, key, new TextEncoder().encode(`${header}.${payload}`));
  return `${header}.${payload}.${b64url(new Uint8Array(signature))}`;
}
