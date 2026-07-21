// Xray "user emails" are identities, not mailboxes: `name@namespace`.
// Lists lead with the local part and tag only namespaces that break the pattern.

export interface UserId {
  local: string;
  /** null when the identity carries no `@namespace` at all. */
  namespace: string | null;
}

export function splitUserId(email: string): UserId {
  const at = email.lastIndexOf("@");
  if (at <= 0 || at === email.length - 1) return { local: email, namespace: null };
  return { local: email.slice(0, at), namespace: email.slice(at + 1) };
}

/** The namespace a strict majority of a list shares, or null if there is none. */
export function dominantNamespace(emails: string[]): string | null {
  const counts = new Map<string, number>();
  let total = 0;

  for (const email of emails) {
    const { namespace } = splitUserId(email);
    if (namespace === null) continue;
    counts.set(namespace, (counts.get(namespace) ?? 0) + 1);
    total += 1;
  }

  let best: string | null = null;
  let bestCount = 0;

  for (const [namespace, count] of counts) {
    if (count > bestCount) {
      best = namespace;
      bestCount = count;
    }
  }

  return bestCount * 2 > total ? best : null;
}

/** Orders by local part, then namespace. */
export function compareUserIds(a: string, b: string): number {
  const left = splitUserId(a);
  const right = splitUserId(b);
  return (
    left.local.localeCompare(right.local) || (left.namespace ?? "").localeCompare(right.namespace ?? "")
  );
}
