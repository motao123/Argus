import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import AdminLayout from "./layouts/AdminLayout";
import PublicLayout from "./layouts/PublicLayout";
import { ServersProvider } from "./context/servers";
import { getToken } from "./lib/api";
import Login from "./pages/Login";
import Overview from "./pages/Overview";
import ServerDetail from "./pages/ServerDetail";
import Servers from "./pages/Servers";
import Alerts from "./pages/Alerts";
import Crons from "./pages/Crons";
import Services from "./pages/Services";
import Files from "./pages/Files";
import Access from "./pages/Access";
import Sessions from "./pages/Sessions";
import Settings from "./pages/Settings";
import Network from "./pages/Network";
import Security from "./pages/Security";
import Maintenance from "./pages/Maintenance";
import Plugins from "./pages/Plugins";
import Audit from "./pages/Audit";
import Notifications from "./pages/Notifications";
import Lifecycle from "./pages/Lifecycle";
import { NotFound, Forbidden } from "./pages/Status";
import ErrorBoundary from "./components/ErrorBoundary";
import TerminalPage from "./pages/Terminal";
import PublicOverview from "./pages/PublicOverview";
import "./index.css";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } },
});

function RequireAuth({ children }: { children: React.ReactNode }) {
  if (!getToken()) return <Navigate to="/login" replace />;
  return <>{children}</>;
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ErrorBoundary>
      <QueryClientProvider client={queryClient}>
        <BrowserRouter>
        <Routes>
          {/* 前台（公开，无需登录）—— 借鉴 komari 前台 + nezha 游客模式 */}
          <Route
            element={
              <ServersProvider>
                <PublicLayout />
              </ServersProvider>
            }
          >
            <Route path="/" element={<PublicOverview />} />
            <Route path="/server/:id" element={<ServerDetail />} />
          </Route>

          <Route path="/login" element={<Login />} />

          {/* 管理后台（登录后） */}
          <Route
            element={
              <RequireAuth>
                <ServersProvider>
                  <AdminLayout />
                </ServersProvider>
              </RequireAuth>
            }
          >
            <Route path="/admin" element={<Navigate to="/admin/overview" replace />} />
            <Route path="/admin/overview" element={<Overview />} />
            <Route path="/admin/servers" element={<Servers />} />
            <Route path="/admin/alerts" element={<Alerts />} />
            <Route path="/admin/crons" element={<Crons />} />
            <Route path="/admin/services" element={<Services />} />
            <Route path="/admin/files" element={<Files />} />
            <Route path="/admin/access" element={<Access />} />
            <Route path="/admin/sessions" element={<Sessions />} />
            <Route path="/admin/network" element={<Network />} />
            <Route path="/admin/security" element={<Security />} />
            <Route path="/admin/maintenance" element={<Maintenance />} />
            <Route path="/admin/plugins" element={<Plugins />} />
            <Route path="/admin/audit" element={<Audit />} />
            <Route path="/admin/notifications" element={<Notifications />} />
            <Route path="/admin/lifecycle" element={<Lifecycle />} />
            <Route path="/admin/settings" element={<Settings />} />
            <Route path="/admin/terminal/:id" element={<TerminalPage />} />
            <Route path="/admin/403" element={<Forbidden />} />
          </Route>

          <Route path="/403" element={<Forbidden />} />
          <Route path="/404" element={<NotFound />} />
          <Route path="*" element={<NotFound />} />
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
    </ErrorBoundary>
  </StrictMode>,
);

// 应用站点 favicon（来自公开设置）
fetch("/api/v1/public/settings")
  .then((r) => r.json())
  .then((d) => {
    const fav = d?.data?.favicon;
    if (fav) {
      const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
      if (link) link.href = fav;
    }
  })
  .catch(() => {});
