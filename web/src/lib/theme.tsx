// 主题框架（里程碑8）：ThemeProvider + useTheme，统一 AdminLayout/PublicLayout/CommandPalette
// 各自维护的重复主题状态（参照 i18n 的 Provider + hook 模式）。
//
// - mode: light/dark/system（localStorage "argus-theme" 持久化，沿用旧键与 .dark class 语义；
//   system 跟随操作系统 prefers-color-scheme，OS 切换时实时生效）
// - activeTheme: 服务端启用的主题包（来自 /api/v1/public/settings 的 active_theme /
//   active_theme_entry），以 <link id="argus-theme-css"> 注入入口 CSS；
//   "default" 或请求失败（Mock 模式）时不注入，回退内置默认主题
// - refresh(): 管理端启用/回滚主题后重新拉取服务端状态
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";

export type ThemeMode = "light" | "dark" | "system";

const STORAGE_KEY = "argus-theme";
const DEFAULT_MODE: ThemeMode = "light";
const CSS_LINK_ID = "argus-theme-css";

/** 服务端公开设置中的主题字段。 */
export interface ActiveThemeInfo {
  /** 当前启用主题名（"default" = 内置）。 */
  name: string;
  /** 入口 CSS 相对路径（default 时为空）。 */
  entry: string;
}

export interface ThemeContextValue {
  mode: ThemeMode;
  setMode: (mode: ThemeMode) => void;
  toggleMode: () => void;
  /** 服务端启用主题（默认 default）。 */
  active: ActiveThemeInfo;
  /** 重新拉取服务端启用主题（激活/回滚后调用）。 */
  refresh: () => Promise<void>;
}

export const ThemeContext = createContext<ThemeContextValue | null>(null);

const MODE_CYCLE: ThemeMode[] = ["light", "dark", "system"];

/** 探测明暗模式：localStorage → <html> class → 默认 light。 */
export function detectMode(): ThemeMode {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored === "light" || stored === "dark" || stored === "system") return stored;
  } catch {
    // ignore
  }
  return document.documentElement.classList.contains("dark") ? "dark" : DEFAULT_MODE;
}

/** 操作系统是否偏好深色。 */
export function prefersDark(): boolean {
  return typeof window !== "undefined" && window.matchMedia?.("(prefers-color-scheme: dark)")?.matches === true;
}

/** 解析模式为实际明暗（system → 跟随 OS）。 */
export function resolveMode(mode: ThemeMode): "light" | "dark" {
  if (mode === "system") return prefersDark() ? "dark" : "light";
  return mode;
}

/** 主题入口 CSS 完整 URL（名称与路径均做 URL 编码）。 */
export function themeCssUrl(name: string, entry: string): string {
  return `/theme-assets/${encodeURIComponent(name)}/${entry.split("/").map(encodeURIComponent).join("/")}`;
}

/** 注入/替换主题入口 CSS 链接；加载失败自动移除并回退默认主题。 */
export function applyThemeCss(active: ActiveThemeInfo | null): void {
  const existing = document.getElementById(CSS_LINK_ID);
  if (existing) {
    existing.remove();
  }
  if (!active || active.name === "default" || !active.entry) return;
  const link = document.createElement("link");
  link.id = CSS_LINK_ID;
  link.rel = "stylesheet";
  link.href = themeCssUrl(active.name, active.entry);
  // 主题包缺失/损坏时静默回退默认主题（不阻断页面）
  link.onerror = () => link.remove();
  document.head.appendChild(link);
}

/** 拉取服务端公开设置中的启用主题；失败（离线/Mock）返回默认。 */
async function fetchActiveTheme(): Promise<ActiveThemeInfo> {
  try {
    const res = await fetch("/api/v1/public/settings");
    const body = (await res.json()) as { data?: { active_theme?: string; active_theme_entry?: string } };
    const name = body?.data?.active_theme || "default";
    const entry = body?.data?.active_theme_entry || "";
    return { name, entry };
  } catch {
    return { name: "default", entry: "" };
  }
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [mode, setModeState] = useState<ThemeMode>(detectMode);
  const [active, setActive] = useState<ActiveThemeInfo>({ name: "default", entry: "" });

  const setMode = useCallback((next: ThemeMode) => {
    setModeState((prev) => (prev === next ? prev : next));
  }, []);

  const refresh = useCallback(async () => {
    const info = await fetchActiveTheme();
    setActive(info);
  }, []);

  // 明暗模式：持久化 + 同步 <html> class（system 跟随 OS，变化时实时生效）
  useEffect(() => {
    try {
      localStorage.setItem(STORAGE_KEY, mode);
    } catch {
      // ignore
    }
    const apply = () => document.documentElement.classList.toggle("dark", resolveMode(mode) === "dark");
    apply();
    if (mode === "system") {
      const mq = window.matchMedia?.("(prefers-color-scheme: dark)");
      mq?.addEventListener?.("change", apply);
      return () => mq?.removeEventListener?.("change", apply);
    }
    return undefined;
  }, [mode]);

  // 首次挂载拉取服务端启用主题
  useEffect(() => {
    void refresh();
  }, [refresh]);

  // 服务端启用主题 → 注入入口 CSS
  useEffect(() => {
    applyThemeCss(active);
  }, [active]);

  const value = useMemo<ThemeContextValue>(
    () => ({
      mode,
      setMode,
      toggleMode: () => {
        const i = MODE_CYCLE.indexOf(mode);
        setMode(MODE_CYCLE[(i + 1) % MODE_CYCLE.length]);
      },
      active,
      refresh,
    }),
    [mode, setMode, active, refresh],
  );

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext);
  if (!ctx) throw new Error("useTheme 必须在 <ThemeProvider> 内使用");
  return ctx;
}
