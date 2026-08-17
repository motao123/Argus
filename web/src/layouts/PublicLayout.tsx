// 前台布局（公开，无需登录）：顶栏式设计，借鉴 komari 前台 + dash-v2 游客模式
import { Activity, Globe, LogIn, Moon, Settings, Sun } from "lucide-react";
import { Outlet, Link, useNavigate } from "react-router-dom";
import { useEffect, useState } from "react";
import { useServers } from "../context/servers";
import { getToken, setToken } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { useTheme } from "../lib/theme";
import { useQuery } from "@tanstack/react-query";
import CommandPalette from "../components/CommandPalette";

// 实时时钟（借鉴 dash-v2 Header）
function Clock() {
  const { lang } = useI18n();
  const [now, setNow] = useState(new Date());
  useEffect(() => {
    const t = setInterval(() => setNow(new Date()), 1000);
    return () => clearInterval(t);
  }, []);
  return (
    <span className="tabular text-sm text-muted">
      {now.toLocaleTimeString(lang, { hour12: false })}
    </span>
  );
}

export default function PublicLayout() {
  const { mode: theme, toggleMode } = useTheme();
  const { online, total } = useServers();
  const { lang, setLang, t } = useI18n();
  const navigate = useNavigate();
  const loggedIn = !!getToken();
  const { data: pub } = useQuery({
    queryKey: ["public-settings"],
    queryFn: () => fetch("/api/v1/public/settings").then((r) => r.json()).then((d) => d.data),
    staleTime: 60000,
  });
  const siteName = pub?.site_name || "Argus";
  const siteDesc = pub?.site_desc || t("common.poweredBy");

  return (
    <div className="flex min-h-screen flex-col bg-bg">
      {/* 顶栏 */}
      <header className="sticky top-0 z-10 border-b border-border bg-panel/90 backdrop-blur">
        <div className="mx-auto flex h-14 max-w-6xl items-center justify-between px-4">
          <Link to="/" className="flex items-center gap-2 text-lg font-bold">
            <Activity className="h-5 w-5 text-accent" />
            {siteName}
          </Link>
          <div className="flex items-center gap-4">
            <Clock />
            <span className="flex items-center gap-1.5 text-sm text-muted">
              <span className="h-2 w-2 rounded-full bg-ok shadow-[0_0_5px] shadow-ok" />
              {t("common.onlineOf", { online, total })}
            </span>
            <button
              onClick={() => setLang(lang === "zh-CN" ? "en" : "zh-CN")}
              className="flex items-center gap-1 rounded-lg px-2 py-1.5 text-xs text-muted hover:bg-black/5 dark:hover:bg-white/5"
              title={t("common.switchLang")}
            >
              <Globe className="h-3.5 w-3.5" />
              {lang === "zh-CN" ? "EN" : "中文"}
            </button>
            <button
              onClick={toggleMode}
              className="rounded-lg p-2 hover:bg-black/5 dark:hover:bg-white/5"
              title={t("common.theme")}
            >
              {theme === "dark" ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
            </button>
            {loggedIn ? (
              <div className="flex items-center gap-2">
                <Link
                  to="/admin/overview"
                  className="flex items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-sm text-white hover:opacity-90"
                >
                  <Settings className="h-3.5 w-3.5" />
                  {t("common.admin")}
                </Link>
                <button
                  onClick={() => {
                    setToken(null);
                    navigate("/");
                  }}
                  className="text-sm text-muted hover:text-fg"
                >
                  {t("common.logout")}
                </button>
              </div>
            ) : (
              <Link
                to="/login"
                className="flex items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-sm hover:bg-black/5 dark:hover:bg-white/5"
              >
                <LogIn className="h-3.5 w-3.5" />
                {t("common.login")}
              </Link>
            )}
          </div>
        </div>
      </header>

      <CommandPalette />
      {/* 主体 */}
      <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-6">
        <Outlet />
      </main>

      {/* 页脚 */}
      <footer className="border-t border-border py-4 text-center text-xs text-muted">
        Powered by <span className="font-medium">{siteName}</span> — {siteDesc}
      </footer>
    </div>
  );
}
