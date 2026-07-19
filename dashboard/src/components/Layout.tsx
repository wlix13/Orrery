import { useState, type ReactNode } from "react";
import { Link, useRouter, type Route } from "../lib/router";
import { useTheme } from "../lib/theme";
import { useAuth } from "../lib/auth";

type IconName = "overview" | "nodes" | "users" | "settings";

interface NavItem {
  href: string;
  label: string;
  icon: IconName;
  isActive: (route: Route) => boolean;
}

const NAV_ITEMS: NavItem[] = [
  { href: "/", label: "Overview", icon: "overview", isActive: (r) => r.name === "overview" },
  { href: "/nodes", label: "Nodes", icon: "nodes", isActive: (r) => r.name === "nodes" || r.name === "nodeDetail" },
  { href: "/users", label: "Users", icon: "users", isActive: (r) => r.name === "users" || r.name === "userDetail" },
  { href: "/settings", label: "Settings", icon: "settings", isActive: (r) => r.name === "settings" },
];

const COLLAPSE_KEY = "orrery.sidebar";

function readCollapsed(): boolean {
  try {
    return localStorage.getItem(COLLAPSE_KEY) === "collapsed";
  } catch {
    return false;
  }
}

function writeCollapsed(collapsed: boolean): void {
  try {
    localStorage.setItem(COLLAPSE_KEY, collapsed ? "collapsed" : "expanded");
  } catch {
    // storage unavailable; the toggle still works for this session
  }
}

/** Nav glyphs; inline because the app carries no icon dependency. */
function NavIcon({ name }: { name: IconName }) {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.7"
      strokeLinecap="round"
      strokeLinejoin="round"
      className="shrink-0"
      aria-hidden
    >
      {name === "overview" && (
        <>
          <rect x="3" y="3" width="7.5" height="7.5" rx="1.5" />
          <rect x="13.5" y="3" width="7.5" height="7.5" rx="1.5" />
          <rect x="3" y="13.5" width="7.5" height="7.5" rx="1.5" />
          <rect x="13.5" y="13.5" width="7.5" height="7.5" rx="1.5" />
        </>
      )}
      {name === "nodes" && (
        <>
          <rect x="3" y="4" width="18" height="6.5" rx="2" />
          <rect x="3" y="13.5" width="18" height="6.5" rx="2" />
          <path d="M7 7.25h.01M7 16.75h.01" />
        </>
      )}
      {name === "users" && (
        <>
          <circle cx="9.5" cy="8" r="3.2" />
          <path d="M3.8 19c.5-3 2.8-4.8 5.7-4.8s5.2 1.8 5.7 4.8" />
          <path d="M16.4 5.4a3 3 0 0 1 0 5.4M18 14.6c2 .7 3.2 2.2 3.4 4.4" />
        </>
      )}
      {name === "settings" && (
        <>
          <path d="M4 7h8M17 7h3M4 17h3M12 17h8" />
          <circle cx="14.5" cy="7" r="2.2" />
          <circle cx="9.5" cy="17" r="2.2" />
        </>
      )}
    </svg>
  );
}

function ChevronIcon({ pointsRight }: { pointsRight: boolean }) {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.7"
      strokeLinecap="round"
      strokeLinejoin="round"
      className="shrink-0"
      aria-hidden
    >
      <path d={pointsRight ? "M9 6l6 6-6 6" : "M15 6l-6 6 6 6"} />
    </svg>
  );
}

function BrandMark({ size = 18 }: { size?: number }) {
  // Minimal orrery: fixed sun + orbiting planet.
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" className="shrink-0 text-accent" aria-hidden>
      <circle cx="12" cy="12" r="2.2" fill="currentColor" />
      <g className="brand-orbit origin-center" style={{ transformOrigin: "12px 12px" }}>
        <circle cx="12" cy="12" r="8" fill="none" stroke="currentColor" strokeOpacity="0.35" strokeWidth="1" />
        <circle cx="12" cy="4" r="1.6" fill="currentColor" fillOpacity="0.85" />
      </g>
    </svg>
  );
}

function Brand() {
  return (
    <span className="flex items-center gap-2.5">
      <BrandMark />
      <span className="flex flex-col leading-none">
        <span className="text-base font-semibold tracking-tight text-text">Orrery</span>
        <span className="mt-0.5 text-[0.65rem] font-medium tracking-wider text-text-faint uppercase">Fleet metrics</span>
      </span>
    </span>
  );
}

function ThemeToggle() {
  const { theme, toggleTheme } = useTheme();
  return (
    <button
      type="button"
      onClick={toggleTheme}
      className="rounded-md px-2 py-1 text-xs text-text-muted transition-colors hover:bg-surface-raised hover:text-text"
      aria-label="Toggle theme"
    >
      {theme === "dark" ? "Light" : "Dark"}
    </button>
  );
}

function SignOutButton() {
  const { signOut } = useAuth();
  const { navigate } = useRouter();
  return (
    <button
      type="button"
      onClick={() => {
        signOut();
        navigate("/");
      }}
      className="rounded-md px-2 py-1 text-xs whitespace-nowrap text-text-muted transition-colors hover:bg-surface-raised hover:text-text"
    >
      Sign out
    </button>
  );
}

function PrincipalBadge({ collapsed }: { collapsed?: boolean }) {
  const { me } = useAuth();
  if (!me || me.method === "anonymous") return null;

  const scope = me.fleets === null ? "all fleets" : me.fleets.join(", ");

  return (
    <span
      className="truncate text-[0.65rem] text-text-faint"
      title={`${me.name} - ${scope} (${me.method.replace("_", " ")})`}
    >
      {collapsed ? me.name.slice(0, 2) : me.name}
    </span>
  );
}

function MockBadge() {
  const { isMock } = useAuth();
  if (!isMock) return null;
  return (
    <span className="rounded-md border border-accent/30 bg-accent-soft px-2 py-0.5 font-mono text-[0.65rem] font-medium tracking-wide text-accent">
      MOCK
    </span>
  );
}

function NavLink({
  item,
  route,
  collapsed = false,
  showIcon = false,
}: {
  item: NavItem;
  route: Route;
  collapsed?: boolean;
  showIcon?: boolean;
}) {
  const active = item.isActive(route);
  return (
    <Link
      href={item.href}
      title={collapsed ? item.label : undefined}
      aria-label={collapsed ? item.label : undefined}
      className={`relative flex items-center gap-2.5 rounded-md py-2 text-sm font-medium transition-colors ${
        collapsed ? "justify-center px-0" : "px-3"
      } ${active ? "nav-item-active bg-accent-soft text-accent" : "text-text-muted hover:bg-surface-raised hover:text-text"}`}
    >
      {active && !collapsed && (
        <span className="absolute top-1/2 left-0 h-4 w-0.5 -translate-y-1/2 rounded-full bg-accent" aria-hidden />
      )}
      {showIcon && <NavIcon name={item.icon} />}
      {!collapsed && <span>{item.label}</span>}
    </Link>
  );
}

export function Layout({ children }: { children: ReactNode }) {
  const { route } = useRouter();
  const [collapsed, setCollapsed] = useState(readCollapsed);

  const toggleCollapsed = () => {
    setCollapsed((prev) => {
      writeCollapsed(!prev);
      return !prev;
    });
  };

  return (
    <div className="flex min-h-screen flex-col lg:flex-row">
      <aside
        className={`hidden shrink-0 border-border/80 bg-surface/80 backdrop-blur-sm transition-[width] duration-200 lg:flex lg:flex-col lg:border-r ${
          collapsed ? "lg:w-[4.5rem]" : "lg:w-60"
        }`}
      >
        <div className={`flex border-b border-border/80 py-5 ${collapsed ? "justify-center px-2" : "px-5"}`}>
          {collapsed ? <BrandMark size={22} /> : <Brand />}
        </div>

        <nav className="flex flex-1 flex-col gap-0.5 p-3">
          {NAV_ITEMS.map((item) => (
            <NavLink key={item.href} item={item} route={route} collapsed={collapsed} showIcon />
          ))}

          <button
            type="button"
            onClick={toggleCollapsed}
            title={collapsed ? "Expand sidebar" : "Collapse sidebar"}
            aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
            aria-expanded={!collapsed}
            className={`mt-auto flex items-center gap-2.5 rounded-md py-2 text-sm font-medium text-text-faint transition-colors hover:bg-surface-raised hover:text-text ${
              collapsed ? "justify-center px-0" : "px-3"
            }`}
          >
            <ChevronIcon pointsRight={collapsed} />
            {!collapsed && <span>Collapse</span>}
          </button>
        </nav>

        <div
          className={`flex gap-1 border-t border-border/80 p-3 ${
            collapsed ? "flex-col items-center" : "items-center justify-between"
          }`}
        >
          {!collapsed && (
            <div className="flex min-w-0 flex-col">
              <MockBadge />
              <PrincipalBadge />
            </div>
          )}
          <div className={`flex gap-1 ${collapsed ? "flex-col items-center" : "items-center"}`}>
            <ThemeToggle />
            <SignOutButton />
          </div>
        </div>
      </aside>

      <header className="flex flex-col gap-3 border-b border-border/80 bg-surface/80 px-4 py-3 backdrop-blur-sm lg:hidden">
        <div className="flex items-center justify-between">
          <Brand />
          <div className="flex items-center gap-1">
            <MockBadge />
            <ThemeToggle />
            <SignOutButton />
          </div>
        </div>
        <nav className="flex gap-1 overflow-x-auto pb-0.5">
          {NAV_ITEMS.map((item) => (
            <NavLink key={item.href} item={item} route={route} />
          ))}
        </nav>
      </header>

      <main className="min-w-0 flex-1 overflow-x-hidden px-4 py-5 lg:px-8 lg:py-7">{children}</main>
    </div>
  );
}
