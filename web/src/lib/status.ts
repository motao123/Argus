// 状态页辅助纯函数（事故/维护窗口/SLA 展示，便于单测）。
import type { Incident, MaintenanceWindow } from "./api";

/** 严重级别 → 色阶（供 dot/徽标使用）。 */
export function severityTone(sev: Incident["severity"]): "err" | "warn" | "ok" {
  switch (sev) {
    case "critical":
      return "err";
    case "major":
      return "warn";
    default:
      return "ok";
  }
}

export type WindowState = "active" | "upcoming" | "ended";

/**
 * 维护窗口当前状态。
 * - 一次性窗口：now < start → upcoming；now >= end → ended；否则 active。
 * - 每周重复窗口：按 StartAt 的星期/时刻每周重复，只返回 active / upcoming。
 */
export function windowState(w: Pick<MaintenanceWindow, "start_at" | "end_at" | "recurring">, now = Date.now()): WindowState {
  const start = new Date(w.start_at).getTime();
  const end = new Date(w.end_at).getTime();
  if (w.recurring) {
    const dur = end - start;
    const weekMs = 7 * 24 * 3600 * 1000;
    let occ = start;
    while (occ + weekMs <= now) occ += weekMs;
    return now >= occ && now < occ + dur ? "active" : "upcoming";
  }
  if (now < start) return "upcoming";
  if (now >= end) return "ended";
  return "active";
}

/** 当前是否处于维护（供离线告警提示与横幅展示）。 */
export function isWindowActive(w: Pick<MaintenanceWindow, "start_at" | "end_at" | "recurring">, now = Date.now()): boolean {
  return windowState(w, now) === "active";
}

/** 分钟数 → 紧凑时长（1440 → "1d"，90 → "1h 30m"，语言中立）。 */
export function fmtMinutes(mins: number): string {
  if (!Number.isFinite(mins) || mins <= 0) return "0m";
  const d = Math.floor(mins / 1440);
  const h = Math.floor((mins % 1440) / 60);
  const m = Math.round(mins % 60);
  const parts: string[] = [];
  if (d > 0) parts.push(`${d}d`);
  if (h > 0) parts.push(`${h}h`);
  if (m > 0 && d === 0) parts.push(`${m}m`);
  return parts.join(" ") || "0m";
}

/** 可用率 → 文案（百分比，保留 2 位小数）。 */
export function fmtAvailability(v: number | null | undefined): string {
  if (v === null || v === undefined || !Number.isFinite(v)) return "—";
  return `${v.toFixed(2)}%`;
}

/** "2026-08" → 月份展示（YYYY年MM月，语言无关的 ISO 亦可）。 */
export function monthLabel(month: string): string {
  const [y, m] = month.split("-");
  return `${y}-${m}`;
}

/** 严重级别排序权重（时间线展示用）。 */
export function severityRank(sev: Incident["severity"]): number {
  switch (sev) {
    case "critical":
      return 0;
    case "major":
      return 1;
    default:
      return 2;
  }
}
