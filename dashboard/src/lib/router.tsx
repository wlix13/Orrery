// Minimal history-based router for the 6 dashboard routes. No react-router:
// this app has a fixed, tiny route table, so a ~50-line matcher + the
// History API is all that's needed.
//
// Edge case: node keys are `fleet/id` (contain a slash), so /nodes/:key
// can't be a single path segment. We encode fleet and id separately when
// building links and rejoin the trailing segments when parsing.

import {
  createContext,
  createElement,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type AnchorHTMLAttributes,
  type MouseEvent,
  type ReactNode,
} from "react";

export type Route =
  | { name: "overview" }
  | { name: "nodes" }
  | { name: "nodeDetail"; node: string }
  | { name: "users" }
  | { name: "userDetail"; email: string }
  | { name: "settings" }
  | { name: "notFound"; path: string };

function parsePath(pathname: string): Route {
  const segments = pathname.split("/").filter(Boolean).map(decodeURIComponent);

  if (segments.length === 0) return { name: "overview" };

  const [head, ...rest] = segments;
  if (head === "nodes") {
    if (rest.length === 0) return { name: "nodes" };
    return { name: "nodeDetail", node: rest.join("/") };
  }
  if (head === "users") {
    if (rest.length === 0) return { name: "users" };
    return { name: "userDetail", email: rest.join("/") };
  }
  if (head === "settings" && rest.length === 0) return { name: "settings" };

  return { name: "notFound", path: pathname };
}

export function nodeHref(node: string): string {
  // node is `fleet/id`; encode each part, keep the separating slash literal.
  if (!node) return "/nodes";
  return `/nodes/${node.split("/").map(encodeURIComponent).join("/")}`;
}

export function userHref(email: string): string {
  return `/users/${encodeURIComponent(email)}`;
}

/** Nodes list with optional fleet/status query filters. */
export function nodesHref(opts?: { fleet?: string; status?: string }): string {
  const params = new URLSearchParams();
  if (opts?.fleet) params.set("fleet", opts.fleet);
  if (opts?.status) params.set("status", opts.status);
  const qs = params.toString();
  return qs ? `/nodes?${qs}` : "/nodes";
}

interface RouterState {
  route: Route;
  pathname: string;
  search: string;
  navigate: (path: string) => void;
}

const RouterContext = createContext<RouterState | null>(null);

function readLocation(): { pathname: string; search: string } {
  return { pathname: window.location.pathname, search: window.location.search };
}

export function RouterProvider({ children }: { children: ReactNode }) {
  const [loc, setLoc] = useState(readLocation);

  useEffect(() => {
    const onPopState = () => setLoc(readLocation());
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);

  const navigate = useCallback((path: string) => {
    const current = window.location.pathname + window.location.search;
    if (path !== current) {
      window.history.pushState(null, "", path);
    }
    setLoc(readLocation());
    window.scrollTo(0, 0);
  }, []);

  const route = useMemo(() => parsePath(loc.pathname), [loc.pathname]);
  const value = useMemo(
    () => ({ route, pathname: loc.pathname, search: loc.search, navigate }),
    [route, loc.pathname, loc.search, navigate],
  );

  return createElement(RouterContext.Provider, { value }, children);
}

export function useRouter(): RouterState {
  const ctx = useContext(RouterContext);
  if (!ctx) throw new Error("useRouter must be used within a RouterProvider");
  return ctx;
}

/** In-app link: intercepts plain left-clicks and routes via history.pushState. */
export function Link({
  href,
  children,
  className,
  onClick,
  ...rest
}: { href: string } & AnchorHTMLAttributes<HTMLAnchorElement>) {
  const { navigate } = useRouter();

  const handleClick = (e: MouseEvent<HTMLAnchorElement>) => {
    onClick?.(e);
    if (e.defaultPrevented) return;
    if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return; // let modified clicks behave natively
    e.preventDefault();
    navigate(href);
  };

  return createElement("a", { href, className, onClick: handleClick, ...rest }, children);
}
