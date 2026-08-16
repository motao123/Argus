import { Activity, Bell, CalendarClock, FolderOpen, KeyRound, LayoutDashboard, LogOut, MonitorSmartphone, Moon, Network as NetworkIcon, Radar, Settings as SettingsIcon, Sun, Users } from "lucide-react";
import { Link, NavLink, Outlet, useNavigate } from "react-router-dom";
import { useEffect, useState } from "react";
import { useServers } from "../context/servers";
import { setToken } from "../lib/api";
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

const nav = [
  { to: "/admin/overview", label: "总览", icon: LayoutDashboard },
  { to: "/admin/servers", label: "服务器", icon: MonitorSmartphone },
  { to: "/admin/services", label: "服务监控", icon: Radar },
  { to: "/admin/alerts", label: "报警", icon: Bell },
  { to: "/admin/crons", label: "任务", icon: CalendarClock },
  { to: "/admin/files", label: "文件管理", icon: FolderOpen },
  { to: "/admin/access", label: "访问控制", icon: KeyRound },
  { to: "/admin/sessions", label: "在线会话", icon: Users },
  { to: "/admin/network", label: "网络服务", icon: NetworkIcon },
  { to: "/admin/settings", label: "设置", icon: SettingsIcon },
];

export default function Layout() {
  const [theme, setTheme] = useTheme();
  const { online, total } = useServers();
  const navigate = useNavigate();

  return (
    <div className="flex min-h-screen">
      <aside className="fixed inset-y-0 left-0 z-10 flex w-52 flex-col border-r border-border bg-panel">
        <div className="flex items-center gap-2 px-5 py-4 text-lg font-bold">
          <Activity className="h-5 w-5 text-accent" />
          Argus
        </div>
        <nav className="flex-1 space-y-1 px-3">
          {nav.map(({ to, label, icon: Icon }) => (
            <NavLink
              key={to}
              to={to}
              end={to === "/"}
              className={({ isActive }) =>
                `flex items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors ${
                  isActive ? "bg-accent/10 text-accent font-medium" : "text-muted hover:bg-black/5 hover:text-fg dark:hover:bg-white/5"
                }`
              }
            >
              <Icon className="h-4 w-4" />
              {label}
            </NavLink>
          ))}
        </nav>
        <div className="border-t border-border p-3 text-xs text-muted">
          <div className="mb-2 flex items-center justify-between px-1">
            <span>在线 {online}/{total}</span>
            <button
              onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
              className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5"
              title="切换主题"
            >
              {theme === "dark" ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
            </button>
          </div>
          <button
            onClick={() => {
              setToken(null);
              navigate("/login");
            }}
            className="flex w-full items-center gap-2 rounded px-2 py-1.5 hover:bg-black/5 dark:hover:bg-white/5"
          >
            <LogOut className="h-3.5 w-3.5" /> 退出登录
          </button>
        </div>
      </aside>
      <CommandPalette />
      <main className="ml-52 flex-1 p-6">
        <Outlet />
      </main>
    </div>
  );
}
