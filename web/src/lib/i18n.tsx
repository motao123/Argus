// i18n 框架（里程碑7）：I18nProvider + useI18n + 插值 + Intl 日期/数字/相对时间。
// - 语言选择：localStorage("argus-lang") 优先 → 浏览器语言回退 → 默认 zh-CN
// - 切换语言：无刷新，同步 <html lang>，持久化 localStorage
// - 插值：翻译方法接受模板占位符 {name}；缺失 key 原样返回便于排查
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { LANGS, messages, type Lang, type TKey } from "../locales";

const STORAGE_KEY = "argus-lang";
const DEFAULT_LANG: Lang = "zh-CN";
const DASH = "—";

export type { Lang, TKey };
export { LANGS };

export function isLang(value: unknown): value is Lang {
  return value === "zh-CN" || value === "en";
}

/** 语言探测：localStorage → navigator.language → 默认 zh-CN。 */
export function detectLang(): Lang {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (isLang(stored)) return stored;
  } catch {
    // localStorage 不可用（隐私模式等）时走浏览器回退
  }
  try {
    if (typeof navigator !== "undefined" && navigator.language?.toLowerCase().startsWith("en")) {
      return "en";
    }
  } catch {
    // ignore
  }
  return DEFAULT_LANG;
}

/** 纯函数翻译（无 React 依赖，便于单测）：返回扁平 key 对应的当前语言文本。 */
export function translate(lang: Lang, key: string, vars?: Record<string, string | number>): string {
  const template = messages[lang][key] ?? key;
  if (!vars) return template;
  return template.replace(/\{(\w+)\}/g, (match, name: string) =>
    Object.prototype.hasOwnProperty.call(vars, name) ? String(vars[name]) : match,
  );
}

/**
 * 按后端稳定错误码翻译（code "server.offline" → key "errors.server_offline"）。
 * 未提供 code 或该 code 无对应 key 时回退原文，保证任何错误都有可见文案。
 */
export function translateError(lang: Lang, code: string | undefined, fallback: string): string {
  if (!code) return fallback;
  const key = `errors.${code.replace(/\./g, "_")}`;
  const template = messages[lang][key];
  return template !== undefined ? template : fallback;
}

// ---------- Intl 格式化器（按语言缓存实例） ----------

const dateTimeCache = new Map<string, Intl.DateTimeFormat>();
const numberCache = new Map<string, Intl.NumberFormat>();
const relativeCache = new Map<string, Intl.RelativeTimeFormat>();

function dateTimeFormat(lang: Lang, options?: Intl.DateTimeFormatOptions): Intl.DateTimeFormat {
  const key = `${lang}|${options ? JSON.stringify(options) : ""}`;
  let fmt = dateTimeCache.get(key);
  if (!fmt) {
    fmt = new Intl.DateTimeFormat(lang, options);
    dateTimeCache.set(key, fmt);
  }
  return fmt;
}

function numberFormat(lang: Lang, options?: Intl.NumberFormatOptions): Intl.NumberFormat {
  const key = `${lang}|${options ? JSON.stringify(options) : ""}`;
  let fmt = numberCache.get(key);
  if (!fmt) {
    fmt = new Intl.NumberFormat(lang, options);
    numberCache.set(key, fmt);
  }
  return fmt;
}

function relativeFormat(lang: Lang): Intl.RelativeTimeFormat {
  let fmt = relativeCache.get(lang);
  if (!fmt) {
    fmt = new Intl.RelativeTimeFormat(lang, { numeric: "auto" });
    relativeCache.set(lang, fmt);
  }
  return fmt;
}

/** 把 Date/秒时间戳/ISO 字符串统一为 Date。 */
function toDate(value: number | string | Date): Date {
  if (value instanceof Date) return value;
  if (typeof value === "number") return new Date(value * 1000); // 秒时间戳（对齐既有 fmtTime 语义）
  return new Date(value);
}

/** 判断时间戳是否为空（0001 年等零值，或数值 0）。 */
function isZeroTimestamp(value: number | string | Date): boolean {
  if (typeof value === "number" && value === 0) return true;
  if (typeof value === "string" && value.startsWith("0001")) return true;
  const d = toDate(value);
  return Number.isNaN(d.getTime()) || d.getFullYear() <= 1;
}

// ---------- Context ----------

export interface I18nContextValue {
  lang: Lang;
  setLang: (lang: Lang) => void;
  /** 翻译 + 插值。key 缺失时原样返回 key，便于发现问题。 */
  t: (key: TKey, vars?: Record<string, string | number>) => string;
  /** 按后端错误码翻译错误（未知 code 或非 API 错误时回退 message/原文）。 */
  tErr: (err: unknown) => string;
  /** 日期（默认 yyyy/MM/dd 风格，可按 locale 自动调整）。 */
  fmtDate: (value: number | string | Date, options?: Intl.DateTimeFormatOptions) => string;
  /** 时间 HH:mm。 */
  fmtTime: (value: number | string | Date) => string;
  /** 日期 + 时间。 */
  fmtDateTime: (value: number | string | Date) => string;
  /** 数字格式化（千分位等）。 */
  fmtNumber: (value: number, options?: Intl.NumberFormatOptions) => string;
  /** Intl 相对时间：距现在 {seconds} 秒前。 */
  fmtRelativeTime: (seconds: number) => string;
  /** 运行时长（天/时/分）。 */
  fmtDuration: (seconds: number) => string;
}

export const I18nContext = createContext<I18nContextValue | null>(null);

export function I18nProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<Lang>(detectLang);

  const setLang = useCallback((next: Lang) => {
    setLangState((prev) => {
      if (prev === next) return prev;
      return next;
    });
  }, []);

  // 持久化 + 同步 <html lang>（切换不刷新页面）
  useEffect(() => {
    try {
      localStorage.setItem(STORAGE_KEY, lang);
    } catch {
      // ignore
    }
    document.documentElement.lang = lang;
  }, [lang]);

  const value = useMemo<I18nContextValue>(() => {
    const t: I18nContextValue["t"] = (key, vars) => translate(lang, key, vars);
    const tErr: I18nContextValue["tErr"] = (err) => {
      const e = err as { code?: string; message?: string } | null;
      const fallback = e?.message || (typeof err === "string" ? err : String(err));
      return translateError(lang, e?.code, fallback);
    };
    return {
      lang,
      setLang,
      t,
      tErr,
      fmtDate: (value, options) =>
        isZeroTimestamp(value)
          ? DASH
          : dateTimeFormat(lang, options ?? { year: "numeric", month: "2-digit", day: "2-digit" }).format(toDate(value)),
      fmtTime: (value) =>
        isZeroTimestamp(value)
          ? DASH
          : dateTimeFormat(lang, { hour: "2-digit", minute: "2-digit", hour12: false }).format(toDate(value)),
      fmtDateTime: (value) =>
        isZeroTimestamp(value)
          ? DASH
          : dateTimeFormat(lang, {
              year: "numeric",
              month: "2-digit",
              day: "2-digit",
              hour: "2-digit",
              minute: "2-digit",
              hour12: false,
            }).format(toDate(value)),
      fmtNumber: (value, options) => numberFormat(lang, options).format(value),
      fmtRelativeTime: (seconds) => {
        if (!seconds) return DASH;
        const abs = Math.abs(seconds);
        let unit: Intl.RelativeTimeFormatUnit = "second";
        let value = Math.round(seconds);
        if (abs >= 2592000) {
          unit = "month";
          value = Math.round(seconds / 2592000);
        } else if (abs >= 86400) {
          unit = "day";
          value = Math.round(seconds / 86400);
        } else if (abs >= 3600) {
          unit = "hour";
          value = Math.round(seconds / 3600);
        } else if (abs >= 60) {
          unit = "minute";
          value = Math.round(seconds / 60);
        }
        return relativeFormat(lang).format(-value, unit);
      },
      fmtDuration: (seconds) => {
        if (!seconds) return DASH;
        const d = Math.floor(seconds / 86400);
        const h = Math.floor((seconds % 86400) / 3600);
        const m = Math.floor((seconds % 3600) / 60);
        if (lang === "en") {
          if (d > 0) return `${d}d ${h}h`;
          if (h > 0) return `${h}h ${m}m`;
          return `${m}m`;
        }
        if (d > 0) return `${d}天 ${h}时`;
        if (h > 0) return `${h}时 ${m}分`;
        return `${m}分`;
      },
    };
  }, [lang, setLang]);

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n(): I18nContextValue {
  const ctx = useContext(I18nContext);
  if (!ctx) throw new Error("useI18n 必须在 <I18nProvider> 内使用");
  return ctx;
}
