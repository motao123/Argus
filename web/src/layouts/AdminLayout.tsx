import { Activity, Bell, BellRing, CalendarClock, ChartLine, ClipboardList, DatabaseBackup, FolderOpen, Globe, KeyRound, LayoutDashboard, LogOut, Menu, MonitorSmartphone, Moon, Network as NetworkIcon, Palette, Radar, RadioTower, ScrollText, Settings as SettingsIcon, ShieldAlert, ShieldCheck, Sun, TriangleAlert, Users, Wrench, X, Zap } from "lucide-react";
import { NavLink, Outlet, useLocation, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { useServers } from "../context/servers";
import { api, setToken } from "../lib/api";
import { useI18n, type TKey } from "../lib/i18n";
import { useTheme } from "../lib/theme";
import CommandPalette from "../components/CommandPalette";

// roles 缺省 = 全部角色；与后端权限矩阵保持一致（前后端菜单收敛）。
// 菜单按一级分组折叠展示：监控 / 运维 / 通知 / 系统 / 扩展；总览单独置顶。
type NavItem = { to: string; key: TKey; icon: typeof LayoutDashboard; roles?: string[] };
type NavGroup = { groupKey: TKey; items: NavItem[] };

const navOverview: NavItem = { to: "/admin/overview", key: "nav.overview", icon: LayoutDashboard };

const navGroups: NavGroup[] = [
  {
    groupKey: "nav.groupMonitor",
    items: [
      { to: "/admin/servers", key: "nav.servers", icon: MonitorSmartphone },
      { to: "/admin/compare", key: "nav.compare", icon: ChartLine, roles: ["admin", "user"] },
      { to: "/admin/services", key: "nav.services", icon: Radar },
      { to: "/admin/alerts", key: "nav.alerts", icon: Bell, roles: ["admin", "user"] },
      { to: "/admin/incidents", key: "nav.statusPage", icon: TriangleAlert, roles: ["admin", "user"] },
    ],
  },
  {
    groupKey: "nav.groupOps",
    items: [
      { to: "/admin/crons", key: "nav.crons", icon: CalendarClock, roles: ["admin", "user"] },
      { to: "/admin/files", key: "nav.files", icon: FolderOpen, roles: ["admin", "user"] },
      { to: "/admin/network", key: "nav.network", icon: NetworkIcon, roles: ["admin", "user"] },
      { to: "/admin/network-test", key: "nav.networkTest", icon: RadioTower, roles: ["admin", "user"] },
      { to: "/admin/clipboard", key: "nav.clipboard", icon: ClipboardList, roles: ["admin", "user"] },
      { to: "/admin/lifecycle", key: "nav.lifecycle", icon: Wrench, roles: ["admin"] },
    ],
  },
  {
    groupKey: "nav.groupNotify",
    items: [{ to: "/admin/notifications", key: "nav.notifications", icon: BellRing, roles: ["admin"] }],
  },
  {
    groupKey: "nav.groupSystem",
    items: [
      { to: "/admin/access", key: "nav.access", icon: KeyRound, roles: ["admin"] },
      { to: "/admin/sessions", key: "nav.sessions", icon: Users },
      { to: "/admin/security", key: "nav.security", icon: ShieldCheck },
      { to: "/admin/waf", key: "nav.waf", icon: ShieldAlert, roles: ["admin"] },
      { to: "/admin/audit", key: "nav.audit", icon: ScrollText, roles: ["admin"] },
      { to: "/admin/maintenance", key: "nav.maintenance", icon: Wrench, roles: ["admin"] },
      { to: "/admin/backups", key: "nav.backups", icon: DatabaseBackup, roles: ["admin"] },
      { to: "/admin/settings", key: "nav.settings", icon: SettingsIcon, roles: ["admin"] },
    ],
  },
  {
    groupKey: "nav.groupExtend",
    items: [
      { to: "/admin/plugins", key: "nav.plugins", icon: Zap, roles: ["admin"] },
      { to: "/admin/themes", key: "nav.themes", icon: Palette, roles: ["admin"] },
    ],
  },
];

// visibleGroups 按角色过滤并折叠：包含当前激活项的分组默认展开，其余默认收起。
function visibleGroups(role: string, pathname: string): { group: NavGroup; items: NavItem[]; open: boolean }[] {
  const out: { group: NavGroup; items: NavItem[]; open: boolean }[] = [];
  for (const g of navGroups) {
    const items = g.items.filter((n) => !n.roles || n.roles.includes(role));
    if (items.length === 0) continue; // 角色不可见的分组整体隐藏
    out.push({ group: g, items, open: items.some((n) => pathname.startsWith(n.to)) });
  }
  return out;
}

function SidebarContent({ theme, onToggleTheme, onToggleLang, onNavigate }: { theme: "light" | "dark"; onToggleTheme: () => void; onToggleLang: () => void; onNavigate?: () => void }) {
  const { online, total } = useServers();
  const { t, lang } = useI18n();
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const { data: me } = useQuery({ queryKey: ["me"], queryFn: api.me });
  const role = me?.role ?? "user";
  const groups = useMemo(() => visibleGroups(role, pathname), [role, pathname]);
  // 展开组集合：初始包含当前激活项所在组；路由变化时自动展开新激活组；点击可手动切换。
  const initialOpen = useMemo(
    () => new Set(groups.filter((g) => g.open).map((g) => g.group.groupKey)),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  );
  const [openGroups, setOpenGroups] = useState<Set<string>>(initialOpen);
  useEffect(() => {
    setOpenGroups((prev) => {
      const active = groups.filter((g) => g.open).map((g) => g.group.groupKey);
      if (active.every((k) => prev.has(k))) return prev;
      const next = new Set(prev);
      active.forEach((k) => next.add(k));
      return next;
    });
  }, [groups]);
  const toggleGroup = (key: string) =>
    setOpenGroups((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  return (
    <>
      <div className="flex items-center gap-2 px-5 py-4 text-lg font-bold">
        <Activity className="h-5 w-5 text-accent" />
        Argus
      </div>
      {role === "readonly" && <div className="mx-3 mb-2 rounded-lg bg-warn/15 px-3 py-1.5 text-xs text-warn">{t("common.readonlyMode")}</div>}
      <nav className="flex-1 space-y-1 overflow-y-auto px-3">
        <NavLink
          to={navOverview.to}
          onClick={onNavigate}
          className={({ isActive }) =>
            `flex items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors ${
              isActive ? "bg-accent/10 text-accent font-medium" : "text-muted hover:bg-black/5 hover:text-fg dark:hover:bg-white/5"
            }`
          }
        >
          <navOverview.icon className="h-4 w-4 shrink-0" />
          {t(navOverview.key)}
        </NavLink>
        {groups.map(({ group, items }) => {
          const isOpen = openGroups.has(group.groupKey);
          const Icon = group.items[0]?.icon ?? LayoutDashboard;
          return (
            <div key={group.groupKey}>
              <button
                onClick={() => toggleGroup(group.groupKey)}
                className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-xs font-semibold uppercase tracking-wide text-muted hover:bg-black/5 hover:text-fg dark:hover:bg-white/5"
              >
                <Icon className="h-4 w-4 shrink-0" />
                <span className="flex-1 text-left">{t(group.groupKey)}</span>
                <span className="text-[10px]">{isOpen ? "▲" : "▼"}</span>
              </button>
              {isOpen && (
                <div className="mt-1 space-y-1">
                  {items.map(({ to, key, icon: ItemIcon }) => (
                    <NavLink
                      key={to}
                      to={to}
                      onClick={onNavigate}
                      className={({ isActive }) =>
                        `flex items-center gap-3 rounded-lg py-2 pl-8 pr-3 text-sm transition-colors ${
                          isActive ? "bg-accent/10 text-accent font-medium" : "text-muted hover:bg-black/5 hover:text-fg dark:hover:bg-white/5"
                        }`
                      }
                    >
                      <ItemIcon className="h-4 w-4 shrink-0" />
                      {t(key)}
                    </NavLink>
                  ))}
                </div>
              )}
            </div>
          );
        })}
      </nav>
      <div className="border-t border-border p-3 text-xs text-muted">
        <div className="mb-2 flex items-center justify-between px-1">
          <span>{t("common.onlineOf", { online, total })}</span>
          <span className="flex items-center gap-1">
            <button onClick={onToggleLang} className="flex items-center gap-1 rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5" title={t("common.switchLang")}>
              <Globe className="h-3.5 w-3.5" />
              {lang === "zh-CN" ? "EN" : "中文"}
            </button>
            <button onClick={onToggleTheme} className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5" title={t("common.theme")}>
              {theme === "dark" ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
            </button>
          </span>
        </div>
        <button
          onClick={() => {
            setToken(null);
            navigate("/login");
          }}
          className="flex w-full items-center gap-2 rounded px-2 py-1.5 hover:bg-black/5 dark:hover:bg-white/5"
        >
          <LogOut className="h-3.5 w-3.5" /> {t("common.logout")}
        </button>
      </div>
    </>
  );
}

export default function Layout() {
  const { mode: theme, toggleMode } = useTheme();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const { lang, setLang, t } = useI18n();

  const toggleLang = () => setLang(lang === "zh-CN" ? "en" : "zh-CN");

  return (
    <div className="min-h-screen">
      {/* 桌面端固定侧栏 */}
      <aside className="fixed inset-y-0 left-0 z-10 hidden w-52 flex-col border-r border-border bg-panel lg:flex">
        <SidebarContent theme={theme} onToggleTheme={toggleMode} onToggleLang={toggleLang} />
      </aside>

      {/* 移动端顶部栏 + 抽屉 */}
      <header className="sticky top-0 z-20 flex items-center justify-between border-b border-border bg-panel px-4 py-3 lg:hidden">
        <button onClick={() => setDrawerOpen(true)} className="rounded-lg p-2 hover:bg-black/5 dark:hover:bg-white/5" aria-label={t("common.openMenu")}>
          <Menu className="h-5 w-5" />
        </button>
        <div className="flex items-center gap-2 font-bold">
          <Activity className="h-5 w-5 text-accent" /> Argus
        </div>
        <button onClick={toggleMode} className="rounded-lg p-2 hover:bg-black/5 dark:hover:bg-white/5" aria-label={t("common.theme")}>
          {theme === "dark" ? <Sun className="h-5 w-5" /> : <Moon className="h-5 w-5" />}
        </button>
      </header>
      {drawerOpen && (
        <div className="fixed inset-0 z-30 lg:hidden">
          <div className="absolute inset-0 bg-black/40" onClick={() => setDrawerOpen(false)} />
          <aside className="absolute inset-y-0 left-0 flex w-64 flex-col border-r border-border bg-panel">
            <button onClick={() => setDrawerOpen(false)} className="absolute right-2 top-3 rounded-lg p-1.5 hover:bg-black/5 dark:hover:bg-white/5" aria-label={t("common.closeMenu")}>
              <X className="h-5 w-5" />
            </button>
            <SidebarContent theme={theme} onToggleTheme={toggleMode} onToggleLang={toggleLang} onNavigate={() => setDrawerOpen(false)} />
          </aside>
        </div>
      )}

      <CommandPalette />
      <main className="p-4 sm:p-6 lg:ml-52">
        <Outlet />
      </main>
    </div>
  );
}
