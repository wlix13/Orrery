import { splitUserId } from "../lib/identity";

interface UserNameProps {
  email: string;
  /** Namespace shared by most of the surrounding list; left implicit. */
  mainNamespace?: string | null;
  className?: string;
}

/** Identity as `local` plus a namespace tag when it differs from mainNamespace. */
export function UserName({ email, mainNamespace = null, className }: UserNameProps) {
  const { local, namespace } = splitUserId(email);
  const foreign = namespace !== null && namespace !== mainNamespace;

  return (
    <span className={`inline-flex items-baseline gap-1.5 ${className ?? ""}`} title={email}>
      <span className="truncate">{local}</span>
      {foreign && (
        <span className="shrink-0 rounded bg-surface-raised px-1 py-0.5 font-mono text-[0.65rem] text-text-faint">
          @{namespace}
        </span>
      )}
    </span>
  );
}
