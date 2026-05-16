import { fromThrowable } from "@hcm-next/foundation";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ArrowRight, Building2, LogOut, ShieldCheck } from "lucide-react";
import { useState, type FormEvent, type ReactNode } from "react";
import {
  BrowserRouter,
  Navigate,
  NavLink,
  Route,
  Routes,
  useLocation,
  useNavigate,
} from "react-router-dom";
import { BrandTokenProvider } from "../brand/BrandTokenProvider";
import { WorkflowPageRoute } from "../features/workflows/WorkflowPageRoute";
import {
  StyleLabRail,
  createDefaultStyleLabConfig,
  styleLabConfigToCssVariables,
  styleLabConfigToFieldStyleProps,
  styleLabConfigToWidgetStyleProps,
  type StyleLabControlStyle,
} from "../runtime/style-lab";
import type {
  FieldControlBaseStyleProps,
  WidgetStyleProps,
} from "../runtime/control-library";
import { demoRuntimeContext } from "./demo-data";
import { appRoutes, navSections } from "./page-routes";

type DemoSession = {
  email: string;
  name: string;
  signedInAt: string;
  workspace: string;
};

const demoSessionStorageKey = "hcm-next-demo-session";
const demoCredentials = {
  email: "admin@harborcare.example",
  password: "HarborCare2026!",
  workspace: "HarborCare Operations",
};

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: 1,
    },
  },
});

const createDemoSession = (): DemoSession => ({
  email: demoCredentials.email,
  name: "Avery Morgan",
  signedInAt: new Date().toISOString(),
  workspace: demoCredentials.workspace,
});

const readStoredDemoSession = (): DemoSession | undefined => {
  if (typeof window === "undefined") {
    return undefined;
  }

  const storedValue = window.localStorage.getItem(demoSessionStorageKey);

  if (storedValue === null) {
    return undefined;
  }

  const parseResult = fromThrowable(
    () => JSON.parse(storedValue) as Partial<DemoSession>,
  );

  if (!parseResult.ok) {
    window.localStorage.removeItem(demoSessionStorageKey);
    return undefined;
  }

  const parsedValue = parseResult.value;
  if (
    parsedValue.email === demoCredentials.email &&
    typeof parsedValue.name === "string" &&
    typeof parsedValue.workspace === "string"
  ) {
    return {
      email: parsedValue.email,
      name: parsedValue.name,
      signedInAt:
        typeof parsedValue.signedInAt === "string"
          ? parsedValue.signedInAt
          : new Date().toISOString(),
      workspace: parsedValue.workspace,
    };
  }

  return undefined;
};

function LoginScreen({
  isAuthenticated,
  onLogin,
}: {
  isAuthenticated: boolean;
  onLogin: (session: DemoSession) => void;
}): JSX.Element {
  const navigate = useNavigate();
  const [email, setEmail] = useState(demoCredentials.email);
  const [password, setPassword] = useState(demoCredentials.password);
  const [error, setError] = useState("");

  if (isAuthenticated) {
    return <Navigate to="/workspace" replace />;
  }

  const submitLogin = (event: FormEvent<HTMLFormElement>): void => {
    event.preventDefault();

    if (email === demoCredentials.email && password === demoCredentials.password) {
      const session = createDemoSession();

      onLogin(session);
      navigate("/workspace", { replace: true });
      return;
    }

    setError("The demo credentials do not match.");
  };

  return (
    <main className="login-shell">
      <section className="login-panel" aria-label="Workspace sign in">
        <div className="login-brand">
          <div className="app-brand-mark" aria-hidden="true">
            HN
          </div>
          <div>
            <p className="eyebrow">HCM Next</p>
            <h1>Sign in</h1>
          </div>
        </div>
        <form className="login-form" onSubmit={submitLogin}>
          <label>
            <span>Email</span>
            <input
              autoComplete="username"
              autoFocus
              onChange={(event) => setEmail(event.currentTarget.value)}
              type="email"
              value={email}
            />
          </label>
          <label>
            <span>Password</span>
            <input
              autoComplete="current-password"
              onChange={(event) => setPassword(event.currentTarget.value)}
              type="password"
              value={password}
            />
          </label>
          {error.length > 0 ? (
            <p className="login-error" role="alert">
              {error}
            </p>
          ) : null}
          <button className="login-submit" type="submit">
            <span>Enter workspace</span>
            <ArrowRight size={18} aria-hidden />
          </button>
        </form>
      </section>
      <aside className="login-workspace-card" aria-label="Selected workspace">
        <div className="workspace-status">
          <Building2 size={20} aria-hidden />
          <span>{demoCredentials.workspace}</span>
        </div>
        <div className="workspace-signal-grid">
          <div>
            <span>Active queues</span>
            <strong>4</strong>
          </div>
          <div>
            <span>Open approvals</span>
            <strong>18</strong>
          </div>
          <div>
            <span>Payroll cutoffs</span>
            <strong>3</strong>
          </div>
          <div>
            <span>Policy ready</span>
            <strong>96%</strong>
          </div>
        </div>
        <div className="workspace-activity-list">
          <div>
            <strong>Headcount approval</strong>
            <span>Cambridge Nursing</span>
          </div>
          <div>
            <strong>Retro change review</strong>
            <span>Payroll cutoff Jun 15</span>
          </div>
          <div>
            <strong>Policy evidence</strong>
            <span>3 files ready</span>
          </div>
        </div>
        <div className="workspace-trust-row">
          <ShieldCheck size={18} aria-hidden />
          <span>Demo access verified</span>
        </div>
      </aside>
    </main>
  );
}

function ConsoleShell({
  children,
  onLogout,
  onStyleLabChange,
  session,
  styleLabConfig,
}: {
  children: ReactNode;
  onLogout: () => void;
  onStyleLabChange: (nextValue: StyleLabControlStyle) => void;
  session: DemoSession;
  styleLabConfig: StyleLabControlStyle;
}): JSX.Element {
  const location = useLocation();

  return (
    <div className="app-shell app-shell-with-style-rail">
      <aside className="app-sidebar" aria-label="Primary navigation">
        <div className="app-brand">
          <div className="app-brand-mark" aria-hidden="true">
            HN
          </div>
          <div>
            <p className="eyebrow">HCM Next</p>
            <h1>Workflow Console</h1>
          </div>
        </div>
        <div className="workspace-session-card">
          <span>{session.workspace}</span>
          <strong>{session.name}</strong>
          <small>{session.email}</small>
          <button onClick={onLogout} type="button">
            <LogOut size={15} aria-hidden />
            Sign out
          </button>
        </div>
        <nav className="app-nav">
          {navSections.map((section) => (
            <section className="nav-section" key={section.label}>
              <p className="nav-section-title">{section.label}</p>
              {section.routes.map((route) => {
                const Icon = route.icon;
                const navPath = route.path === "/" ? "/workspace" : route.path;
                const isWorkspaceActive =
                  route.path === "/" &&
                  (location.pathname === "/" || location.pathname === "/workspace");

                return (
                  <NavLink
                    key={route.path}
                    to={navPath}
                    className={({ isActive }) =>
                      isActive || isWorkspaceActive
                        ? "nav-link nav-link-active"
                        : "nav-link"
                    }
                  >
                    <Icon size={18} aria-hidden />
                    <span>
                      <strong>{route.label}</strong>
                      <small>{route.description}</small>
                    </span>
                  </NavLink>
                );
              })}
            </section>
          ))}
        </nav>
      </aside>
      <main className="app-main">{children}</main>
      <StyleLabRail
        brand={demoRuntimeContext.brand}
        className="app-style-lab-rail"
        onChange={onStyleLabChange}
        value={styleLabConfig}
      />
    </div>
  );
}

function AuthenticatedWorkflowPage({
  fieldBrandingStyleProps,
  onLogout,
  onStyleLabChange,
  pageId,
  session,
  styleLabConfig,
  widgetBrandingStyleProps,
}: {
  fieldBrandingStyleProps?: FieldControlBaseStyleProps;
  onLogout: () => void;
  onStyleLabChange: (nextValue: StyleLabControlStyle) => void;
  pageId: string;
  session: DemoSession | undefined;
  styleLabConfig: StyleLabControlStyle;
  widgetBrandingStyleProps?: WidgetStyleProps;
}): JSX.Element {
  const location = useLocation();

  if (session === undefined) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />;
  }

  return (
    <ConsoleShell
      onLogout={onLogout}
      onStyleLabChange={onStyleLabChange}
      session={session}
      styleLabConfig={styleLabConfig}
    >
      <WorkflowPageRoute
        {...(fieldBrandingStyleProps === undefined ? {} : { fieldBrandingStyleProps })}
        pageId={pageId}
        {...(widgetBrandingStyleProps === undefined
          ? {}
          : { widgetBrandingStyleProps })}
      />
    </ConsoleShell>
  );
}

function BlankWorkspacePage({
  onLogout,
  session,
}: {
  onLogout: () => void;
  session: DemoSession | undefined;
}): JSX.Element {
  const location = useLocation();

  if (session === undefined) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />;
  }

  return (
    <main className="workspace-shell" aria-label="Workspace">
      <header className="workspace-topbar">
        <div className="app-brand">
          <div className="app-brand-mark" aria-hidden="true">
            HN
          </div>
          <div>
            <p className="eyebrow">HCM Next</p>
            <h1>{session.workspace}</h1>
          </div>
        </div>
        <button onClick={onLogout} type="button">
          <LogOut size={16} aria-hidden />
          Sign out
        </button>
      </header>
      <section className="workspace-blank-canvas" aria-label="Blank workspace" />
    </main>
  );
}

export function App(): JSX.Element {
  const [styleLabConfig, setStyleLabConfig] = useState(() =>
    createDefaultStyleLabConfig(demoRuntimeContext.brand.tokens),
  );
  const [session, setSession] = useState<DemoSession | undefined>(
    readStoredDemoSession,
  );
  const styleOverrides = styleLabConfigToCssVariables(styleLabConfig);
  const fieldBrandingStyleProps = styleLabConfigToFieldStyleProps(styleLabConfig);
  const widgetBrandingStyleProps = styleLabConfigToWidgetStyleProps(styleLabConfig);

  const handleLogin = (nextSession: DemoSession): void => {
    window.localStorage.setItem(demoSessionStorageKey, JSON.stringify(nextSession));
    setSession(nextSession);
  };

  const handleLogout = (): void => {
    window.localStorage.removeItem(demoSessionStorageKey);
    setSession(undefined);
  };

  const workflowRoute = (pageId: string): JSX.Element => (
    <AuthenticatedWorkflowPage
      fieldBrandingStyleProps={fieldBrandingStyleProps}
      onLogout={handleLogout}
      onStyleLabChange={setStyleLabConfig}
      pageId={pageId}
      session={session}
      styleLabConfig={styleLabConfig}
      widgetBrandingStyleProps={widgetBrandingStyleProps}
    />
  );

  return (
    <QueryClientProvider client={queryClient}>
      <BrandTokenProvider
        brand={demoRuntimeContext.brand}
        styleOverrides={styleOverrides}
      >
        <BrowserRouter>
          <Routes>
            <Route
              path="/"
              element={
                <Navigate
                  to={session === undefined ? "/login" : "/workspace"}
                  replace
                />
              }
            />
            <Route
              path="/login"
              element={
                <LoginScreen
                  isAuthenticated={session !== undefined}
                  onLogin={handleLogin}
                />
              }
            />
            <Route
              path="/workspace"
              element={<BlankWorkspacePage onLogout={handleLogout} session={session} />}
            />
            {appRoutes
              .filter((route) => route.path !== "/")
              .map((route) => (
                <Route
                  key={route.path}
                  path={route.path}
                  element={workflowRoute(route.pageId)}
                />
              ))}
            <Route
              path="*"
              element={
                <Navigate
                  to={session === undefined ? "/login" : "/workspace"}
                  replace
                />
              }
            />
          </Routes>
        </BrowserRouter>
      </BrandTokenProvider>
    </QueryClientProvider>
  );
}
