// Xray reports whatever address the inbound saw. A WireGuard inbound hands
// over the peer's tunnel address, so these classes mark an identity-ish
// address rather than the client's routable source.

export type IPScope = "private" | "cgnat" | "link-local" | "loopback";

function parseV4(ip: string): [number, number] | null {
  const parts = ip.split(".");
  if (parts.length !== 4) return null;

  const octets = parts.map((p) => (/^\d{1,3}$/.test(p) ? Number(p) : NaN));
  if (octets.some((o) => Number.isNaN(o) || o > 255)) return null;

  return [octets[0]!, octets[1]!];
}

/** Expands an IPv6 address (with optional "::" run) into its 8 hextet values, or null if malformed. */
function parseV6Groups(ip: string): number[] | null {
  const halves = ip.split("::");
  if (halves.length > 2) return null;

  const side = (s: string) => (s === "" ? [] : s.split(":"));
  const left = side(halves[0] ?? "");
  const right = halves.length === 2 ? side(halves[1] ?? "") : [];
  const fill = 8 - left.length - right.length;
  if (halves.length === 1 ? left.length !== 8 : fill < 0) return null;

  const groups = halves.length === 2 ? [...left, ...new Array(fill).fill("0"), ...right] : left;
  const nums = groups.map((g) => (/^[0-9a-f]{1,4}$/.test(g) ? parseInt(g, 16) : NaN));

  return nums.some((n) => Number.isNaN(n)) ? null : nums;
}

/** The non-routable class an address falls in, or null when it is public. */
export function ipScope(raw: string): IPScope | null {
  const ip = raw.trim().toLowerCase().replace(/^\[|\]$/g, "").split("%")[0] ?? "";
  const v4 = parseV4(ip.startsWith("::ffff:") ? ip.slice("::ffff:".length) : ip);

  if (v4) {
    const [a, b] = v4;
    if (a === 127) return "loopback";
    if (a === 10 || (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168)) return "private";
    if (a === 100 && b >= 64 && b <= 127) return "cgnat";
    if (a === 169 && b === 254) return "link-local";
    return null;
  }

  if (!ip.includes(":")) return null;

  const v6 = parseV6Groups(ip);
  if (v6 && v6.slice(0, 7).every((g) => g === 0) && v6[7] === 1) return "loopback";

  const head = ip.split(":")[0] ?? "";
  if (head.startsWith("fc") || head.startsWith("fd")) return "private";
  if (/^fe[89ab]/.test(head)) return "link-local";

  return null;
}
