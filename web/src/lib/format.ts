// 单位与时间格式化工具

export function fmtBytes(n: number): string {
  if (!n || n <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), units.length - 1);
  const v = n / 1024 ** i;
  return `${v >= 100 ? v.toFixed(0) : v.toFixed(1)} ${units[i]}`;
}

export function fmtSpeed(n: number): string {
  return `${fmtBytes(n)}/s`;
}

export function fmtPercent(used: number, total: number): string {
  if (!total) return "0%";
  return `${Math.min(100, (used / total) * 100).toFixed(1)}%`;
}

export function fmtUptime(seconds: number): string {
  if (!seconds) return "—";
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (d > 0) return `${d}天 ${h}时`;
  if (h > 0) return `${h}时 ${m}分`;
  return `${m}分`;
}

export function fmtTime(ts: number): string {
  const d = new Date(ts * 1000);
  return d.toLocaleTimeString("zh-CN", { hour12: false });
}

export function fmtDateTime(ts: string): string {
  if (!ts || ts.startsWith("0001")) return "—";
  return new Date(ts).toLocaleString("zh-CN", { hour12: false });
}
