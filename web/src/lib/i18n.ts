// 轻量 i18n（zh-CN / en），localStorage 持久化
export type Lang = "zh-CN" | "en";

const dict: Record<string, { "zh-CN": string; en: string }> = {
  "nav.overview": { "zh-CN": "服务器总览", en: "Server Overview" },
  "nav.servers": { "zh-CN": "服务器", en: "Servers" },
  "nav.services": { "zh-CN": "服务监控", en: "Services" },
  "nav.alerts": { "zh-CN": "报警", en: "Alerts" },
  "nav.crons": { "zh-CN": "任务", en: "Crons" },
  "nav.files": { "zh-CN": "文件管理", en: "Files" },
  "nav.access": { "zh-CN": "访问控制", en: "Access" },
  "common.login": { "zh-CN": "登录", en: "Login" },
  "common.logout": { "zh-CN": "退出", en: "Logout" },
  "common.admin": { "zh-CN": "管理后台", en: "Admin" },
  "common.viewFront": { "zh-CN": "查看前台", en: "View Front" },
  "common.search": { "zh-CN": "搜索服务器…", en: "Search servers…" },
  "common.online": { "zh-CN": "在线", en: "Online" },
  "common.offline": { "zh-CN": "离线", en: "Offline" },
  "common.all": { "zh-CN": "全部", en: "All" },
  "common.sort": { "zh-CN": "默认", en: "Default" },
  "common.theme": { "zh-CN": "切换主题", en: "Toggle theme" },
  "common.poweredBy": { "zh-CN": "轻量自托管服务器监控", en: "Lightweight self-hosted server monitoring" },
  "stat.total": { "zh-CN": "总服务器", en: "Total Servers" },
  "stat.traffic": { "zh-CN": "实时流量", en: "Network" },
  "svc.status": { "zh-CN": "服务监控状态", en: "Service Status" },
  "svc.today": { "zh-CN": "今日", en: "Today" },
  "detail.terminal": { "zh-CN": "网页终端", en: "Terminal" },
  "detail.loginForTerminal": { "zh-CN": "登录后使用终端", en: "Login to use terminal" },
};

export function getLang(): Lang {
  return (localStorage.getItem("argus-lang") as Lang) || "zh-CN";
}

export function setLang(l: Lang) {
  localStorage.setItem("argus-lang", l);
}

export function t(key: string): string {
  const entry = dict[key];
  if (!entry) return key;
  return entry[getLang()];
}
