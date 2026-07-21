import { RouterProvider, useRouter, Link } from "./lib/router";
import { ThemeProvider } from "./lib/theme";
import { AuthProvider, useAuth } from "./lib/auth";
import { FleetsProvider } from "./lib/fleets";
import { Layout } from "./components/Layout";
import { TokenGate } from "./components/TokenGate";
import { ErrorBoundary } from "./components/ErrorBoundary";
import Overview from "./pages/Overview";
import Nodes from "./pages/Nodes";
import NodeDetail from "./pages/NodeDetail";
import Users from "./pages/Users";
import UserDetail from "./pages/UserDetail";
import Settings from "./pages/Settings";

function NotFound({ path }: { path: string }) {
  return (
    <div className="flex h-[60vh] flex-col items-center justify-center gap-2 text-center">
      <h1 className="page-title">Page not found</h1>
      <p className="text-sm text-text-muted">No route matches {path}.</p>
      <Link href="/" className="text-sm text-accent hover:underline">
        Back to Overview
      </Link>
    </div>
  );
}

function Routes() {
  const { route } = useRouter();
  switch (route.name) {
    case "overview":
      return <Overview />;
    case "nodes":
      return <Nodes />;
    case "nodeDetail":
      return <NodeDetail nodeKey={route.node} />;
    case "users":
      return <Users />;
    case "userDetail":
      return <UserDetail email={route.email} />;
    case "settings":
      return <Settings />;
    case "notFound":
      return <NotFound path={route.path} />;
  }
}

function Gate() {
  const { needsToken, checking } = useAuth();
  if (checking) return null;

  if (needsToken) return <TokenGate />;
  return (
    <FleetsProvider>
      <Layout>
        <ErrorBoundary>
          <Routes />
        </ErrorBoundary>
      </Layout>
    </FleetsProvider>
  );
}

export default function App() {
  return (
    <ThemeProvider>
      <RouterProvider>
        <AuthProvider>
          <Gate />
        </AuthProvider>
      </RouterProvider>
    </ThemeProvider>
  );
}
