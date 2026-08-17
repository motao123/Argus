import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import AdminLayout from "./layouts/AdminLayout";
import PublicLayout from "./layouts/PublicLayout";
import { ServersProvider } from "./context/servers";
import { api, getToken } from "./lib/api";
import { I18nProvider } from "./lib/i18n";
import { ThemeProvider } from "./lib/theme";
import Login from "./pages/Login";
import Overview from "./pages/Overview";
import ServerDetail from "./pages/ServerDetail";
import Servers from "./pages/Servers";
import Clipboard from "./pages/Clipboard";
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
import Backups from "./pages/Backups";
import Incidents from "./pages/Incidents";
import Plugins from "./pages/Plugins";
import Themes from "./pages/Themes";
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

// RequireRole 页面级角色守卫：与侧栏菜单、后端权限矩阵保持一致（前后端收敛）。
function RequireRole({ roles, children }: { roles: string[]; children: React.ReactNode }) {
  const { data: me, isLoading } = useQuery({ queryKey: ["me"], queryFn: api.me });
  if (isLoading) return null;
  if (!me || !roles.includes(me.role)) return <Navigate to="/admin/403" replace />;
  return <>{children}</>;
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <I18nProvider>
      <ThemeProvider>
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
            <Route path="/admin/clipboard" element={<RequireRole roles={["admin", "user"]}><Clipboard /></RequireRole>} />
            <Route path="/admin/alerts" element={<RequireRole roles={["admin", "user"]}><Alerts /></RequireRole>} />
            <Route path="/admin/crons" element={<RequireRole roles={["admin", "user"]}><Crons /></RequireRole>} />
            <Route path="/admin/services" element={<Services />} />
            <Route path="/admin/files" element={<RequireRole roles={["admin", "user"]}><Files /></RequireRole>} />
            <Route path="/admin/access" element={<RequireRole roles={["admin"]}><Access /></RequireRole>} />
            <Route path="/admin/sessions" element={<Sessions />} />
            <Route path="/admin/network" element={<RequireRole roles={["admin", "user"]}><Network /></RequireRole>} />
            <Route path="/admin/security" element={<Security />} />
            <Route path="/admin/maintenance" element={<RequireRole roles={["admin"]}><Maintenance /></RequireRole>} />
            <Route path="/admin/backups" element={<RequireRole roles={["admin"]}><Backups /></RequireRole>} />
            <Route path="/admin/incidents" element={<RequireRole roles={["admin", "user"]}><Incidents /></RequireRole>} />
            <Route path="/admin/plugins" element={<RequireRole roles={["admin"]}><Plugins /></RequireRole>} />
            <Route path="/admin/themes" element={<RequireRole roles={["admin"]}><Themes /></RequireRole>} />
            <Route path="/admin/audit" element={<RequireRole roles={["admin"]}><Audit /></RequireRole>} />
            <Route path="/admin/notifications" element={<RequireRole roles={["admin"]}><Notifications /></RequireRole>} />
            <Route path="/admin/lifecycle" element={<RequireRole roles={["admin"]}><Lifecycle /></RequireRole>} />
            <Route path="/admin/settings" element={<RequireRole roles={["admin"]}><Settings /></RequireRole>} />
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
      </ThemeProvider>
    </I18nProvider>
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
