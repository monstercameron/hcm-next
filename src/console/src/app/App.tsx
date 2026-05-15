import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";
import { NavLink, Navigate, Route, Routes } from "react-router-dom";
import { BrowserRouter } from "react-router-dom";
import { BrandTokenProvider } from "../brand/BrandTokenProvider";
import { WorkflowPageRoute } from "../features/workflows/WorkflowPageRoute";
import {
  StyleLabRail,
  createDefaultStyleLabConfig,
  styleLabConfigToFieldStyleProps,
  styleLabConfigToCssVariables,
  styleLabConfigToWidgetStyleProps,
} from "../runtime/style-lab";
import { demoRuntimeContext } from "./demo-data";
import { appRoutes, navSections } from "./page-routes";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: 1,
    },
  },
});

export function App(): JSX.Element {
  const [styleLabConfig, setStyleLabConfig] = useState(() =>
    createDefaultStyleLabConfig(demoRuntimeContext.brand.tokens),
  );
  const styleOverrides = styleLabConfigToCssVariables(styleLabConfig);
  const fieldBrandingStyleProps = styleLabConfigToFieldStyleProps(styleLabConfig);
  const widgetBrandingStyleProps = styleLabConfigToWidgetStyleProps(styleLabConfig);

  return (
    <QueryClientProvider client={queryClient}>
      <BrandTokenProvider
        brand={demoRuntimeContext.brand}
        styleOverrides={styleOverrides}
      >
        <BrowserRouter>
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
              <nav className="app-nav">
                {navSections.map((section) => (
                  <section className="nav-section" key={section.label}>
                    <p className="nav-section-title">{section.label}</p>
                    {section.routes.map((route) => {
                      const Icon = route.icon;

                      return (
                        <NavLink
                          key={route.path}
                          to={route.path}
                          className={({ isActive }) =>
                            isActive ? "nav-link nav-link-active" : "nav-link"
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
            <main className="app-main">
              <Routes>
                {appRoutes.map((route) => (
                  <Route
                    key={route.path}
                    path={route.path}
                    element={
                      <WorkflowPageRoute
                        fieldBrandingStyleProps={fieldBrandingStyleProps}
                        pageId={route.pageId}
                        widgetBrandingStyleProps={widgetBrandingStyleProps}
                      />
                    }
                  />
                ))}
                <Route path="*" element={<Navigate to="/" replace />} />
              </Routes>
            </main>
            <StyleLabRail
              brand={demoRuntimeContext.brand}
              className="app-style-lab-rail"
              onChange={setStyleLabConfig}
              value={styleLabConfig}
            />
          </div>
        </BrowserRouter>
      </BrandTokenProvider>
    </QueryClientProvider>
  );
}
