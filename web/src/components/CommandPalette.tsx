// 命令面板 Cmd+K（借鉴 dash-v2 DashCommand）：服务器搜索跳转 + 主题切换
import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Activity, Command, MonitorSmartphone, Moon, Sun } from "lucide-react";
import { useServers } from "../context/servers";
import { useI18n } from "../lib/i18n";

export default function CommandPalette() {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const { servers } = useServers();
  const navigate = useNavigate();
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setOpen((v) => !v);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  useEffect(() => {
    if (open) setTimeout(() => inputRef.current?.focus(), 50);
    else setQuery("");
  }, [open]);

  const results = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return servers.slice(0, 8);
    return servers.filter((s) => s.name.toLowerCase().includes(q)).slice(0, 8);
  }, [query, servers]);

  if (!open) return null;

  const toggleTheme = () => {
    const dark = document.documentElement.classList.contains("dark");
    document.documentElement.classList.toggle("dark", !dark);
    localStorage.setItem("argus-theme", dark ? "light" : "dark");
    setOpen(false);
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center bg-black/40 pt-[15vh]"
      onClick={() => setOpen(false)}
    >
      <div
        className="w-full max-w-lg rounded-xl border border-border bg-panel shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2 border-b border-border px-4 py-3">
          <Command className="h-4 w-4 text-muted" />
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t("common.search")}
            className="flex-1 bg-transparent text-sm outline-none"
          />
          <kbd className="rounded bg-black/5 px-1.5 py-0.5 text-xs text-muted dark:bg-white/10">Esc</kbd>
        </div>
        <div className="max-h-72 overflow-auto p-2">
          {results.map((s) => (
            <button
              key={s.id}
              onClick={() => {
                navigate(`/server/${s.id}`);
                setOpen(false);
              }}
              className="flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left text-sm hover:bg-black/5 dark:hover:bg-white/5"
            >
              <MonitorSmartphone className="h-4 w-4 text-muted" />
              <span className="flex-1 truncate">{s.name}</span>
              <span className={`h-2 w-2 rounded-full ${s.online ? "bg-ok" : "bg-err"}`} />
            </button>
          ))}
          {results.length === 0 && (
            <div className="px-3 py-6 text-center text-sm text-muted">{t("common.noMatch")}</div>
          )}
          <div className="mt-1 border-t border-border pt-1">
            <button
              onClick={toggleTheme}
              className="flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left text-sm hover:bg-black/5 dark:hover:bg-white/5"
            >
              {document.documentElement.classList.contains("dark") ? (
                <Sun className="h-4 w-4 text-muted" />
              ) : (
                <Moon className="h-4 w-4 text-muted" />
              )}
              {t("commandPalette.toggleTheme")}
            </button>
            <button
              onClick={() => {
                navigate("/");
                setOpen(false);
              }}
              className="flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left text-sm hover:bg-black/5 dark:hover:bg-white/5"
            >
              <Activity className="h-4 w-4 text-muted" />
              {t("commandPalette.backOverview")}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
