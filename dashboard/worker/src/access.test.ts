import { beforeAll, expect, test, vi } from "vitest";
import { verifyAccessJwt } from "./access";
import {
  AUD,
  CERTS_URL,
  TEAM,
  b64url,
  generateKeys,
  mint,
  publicJwk,
  validClaims,
  type AccessJwk,
  type Claims,
  type MintOptions,
} from "./testkit";

let keys: CryptoKeyPair;
let foreignKeys: CryptoKeyPair;

const verify = (assertion: string) => verifyAccessJwt(assertion, TEAM, AUD);
const assertion = (claims: Claims = {}, opts: MintOptions = {}) => mint(keys.privateKey, validClaims(claims), opts);

beforeAll(async () => {
  keys = await generateKeys();
  foreignKeys = await generateKeys();

  const jwks: { keys: AccessJwk[] } = { keys: [await publicJwk(keys, "key-1")] };
  vi.stubGlobal("fetch", async (input: RequestInfo | URL) => {
    if (String(input) !== CERTS_URL) throw new Error(`unexpected fetch: ${input}`);
    return new Response(JSON.stringify(jwks), { headers: { "content-type": "application/json" } });
  });
});

test("accepts a valid assertion", async () => {
  await expect(verify(await assertion())).resolves.toBe("ops@example.com");
});

test("accepts an array audience containing ours", async () => {
  await expect(verify(await assertion({ aud: ["other", AUD] }))).resolves.toBe("ops@example.com");
});

test.each([
  ["wrong audience", { aud: "someone-elses-app" }, /audience/],
  ["wrong issuer", { iss: "https://evil.cloudflareaccess.com" }, /issuer/],
  ["expired", { exp: Math.floor(Date.now() / 1000) - 3600 }, /expired/],
  ["not yet valid", { nbf: Math.floor(Date.now() / 1000) + 3600 }, /not yet valid/],
  ["no email", { email: undefined }, /no email/],
])("rejects %s", async (_name, claims, want) => {
  await expect(verify(await assertion(claims))).rejects.toThrow(want);
});

test("rejects an algorithm swap", async () => {
  await expect(verify(await assertion({}, { alg: "HS256" }))).rejects.toThrow(/algorithm/);
});

test("rejects an unknown kid", async () => {
  await expect(verify(await assertion({}, { kid: "rotated-out" }))).rejects.toThrow(/no Access signing key/);
});

test("rejects a signature from a foreign key", async () => {
  await expect(verify(await mint(foreignKeys.privateKey, validClaims()))).rejects.toThrow(/signature/);
});

test("rejects tampered claims", async () => {
  const [header, , signature] = (await assertion()).split(".") as [string, string, string];
  const forged = b64url(JSON.stringify(validClaims({ email: "attacker@example.com" })));
  await expect(verify(`${header}.${forged}.${signature}`)).rejects.toThrow(/signature/);
});

test("rejects a malformed assertion", async () => {
  await expect(verify("not.a.jwt.at.all")).rejects.toThrow(/malformed/);
});
