import { Activity, Bell, CalendarClock, FolderOpen, Globe, KeyRound, LayoutDashboard, LogOut, Menu, MonitorSmartphone, Moon, Network as NetworkIcon, Radar, Settings as SettingsIcon, ShieldCheck, Sun, BellRing, ScrollText, Users, Wrench, X, Zap } from "lucide-react";
import { NavLink, Outlet, useNavigate } from "react-router-dom";
import { useEffect, useState } from "react";
import { useServers } from "../context/servers";
import { setToken } from "../lib/api";
import { useI18n, type TKey } from "../lib/i18n";
import CommandPalette from "../components/CommandPalette";

function useTheme() {
  const [theme, setTheme] = useState<"light" | "dark">(() =>
    document.documentElement.classList.contains("dark") ? "dark" : "light",
  );
  useEffect(() => {
    document.documentElement.classList.toggle("dark", theme === "dark");
    localStorage.setItem("argus-theme", theme);
  }, [theme]);
  return [theme, setTheme] as const;
}

const nav: { to: string; key: TKey; icon: typeof LayoutDashboard }[] = [
  { to: "/admin/overview", key: "nav.overview", icon: LayoutDashboard },
  { to: "/admin/servers", key: "nav.servers", icon: MonitorSmartphone },
  { to: "/admin/services", key: "nav.services", icon: Radar },
  { to: "/admin/alerts", key: "nav.alerts", icon: Bell },
  { to: "/admin/crons", key: "nav.crons", icon: CalendarClock },
  { to: "/admin/files", key: "nav.files", icon: FolderOpen },
  { to: "/admin/access", key: "nav.access", icon: KeyRound },
  { to: "/admin/sessions", key: "nav.sessions", icon: Users },
  { to: "/admin/network", key: "nav.network", icon: NetworkIcon },
  { to: "/admin/security", key: "nav.security", icon: ShieldCheck },
  { to: "/admin/notifications", key: "nav.notifications", icon: BellRing },
  { to: "/admin/plugins", key: "nav.plugins", icon: Zap },
  { to: "/admin/lifecycle", key: "nav.lifecycle", icon: Wrench },
  { to: "/admin/audit", key: "nav.audit", icon: ScrollText },
  { to: "/admin/maintenance", key: "nav.maintenance", icon: Wrench },
  { to: "/admin/settings", key: "nav.settings", icon: SettingsIcon },
];

function SidebarContent({ theme, onToggleTheme, onToggleLang, onNavigate }: { theme: "light" | "dark"; onToggleTheme: () => void; onToggleLang: () => void; onNavigate?: () => void }) {
  const { online, total } = useServers();
  const { t, lang } = useI18n();
  const navigate = useNavigate();
  return (
    <>
      <div className="flex items-center gap-2 px-5 py-4 text-lg font-bold">
        <Activity className="h-5 w-5 text-accent" />
        Argus
      </div>
      <nav className="flex-1 space-y-1 overflow-y-auto px-3">
        {nav.map(({ to, key, icon: Icon }) => (
          <NavLink
            key={to}
            to={to}
            onClick={onNavigate}
            className={({ isActive }) =>
              `flex items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors ${
                isActive ? "bg-accent/10 text-accent font-medium" : "text-muted hover:bg-black/5 hover:text-fg dark:hover:bg-white/5"
              }`
            }
          >
            <Icon className="h-4 w-4 shrink-0" />
            {t(key)}
          </NavLink>
        ))}
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
  const [theme, setTheme] = useTheme();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const { lang, setLang, t } = useI18n();

  const toggleLang = () => setLang(lang === "zh-CN" ? "en" : "zh-CN");

  return (
    <div className="min-h-screen">
      {/* 桌面端固定侧栏 */}
      <aside className="fixed inset-y-0 left-0 z-10 hidden w-52 flex-col border-r border-border bg-panel lg:flex">
        <SidebarContent theme={theme} onToggleTheme={() => setTheme(theme === "dark" ? "light" : "dark")} onToggleLang={toggleLang} />
      </aside>

      {/* 移动端顶部栏 + 抽屉 */}
      <header className="sticky top-0 z-20 flex items-center justify-between border-b border-border bg-panel px-4 py-3 lg:hidden">
        <button onClick={() => setDrawerOpen(true)} className="rounded-lg p-2 hover:bg-black/5 dark:hover:bg-white/5" aria-label={t("common.openMenu")}>
          <Menu className="h-5 w-5" />
        </button>
        <div className="flex items-center gap-2 font-bold">
          <Activity className="h-5 w-5 text-accent" /> Argus
        </div>
        <button onClick={() => setTheme(theme === "dark" ? "light" : "dark")} className="rounded-lg p-2 hover:bg-black/5 dark:hover:bg-white/5" aria-label={t("common.theme")}>
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
            <SidebarContent theme={theme} onToggleTheme={() => setTheme(theme === "dark" ? "light" : "dark")} onToggleLang={toggleLang} onNavigate={() => setDrawerOpen(false)} />
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
