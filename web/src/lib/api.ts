// API 类型与请求封装（对齐 server/internal/api 返回结构）

export interface HostInfo {
  hostname: string;
  platform: string;
  platform_version: string;
  cpu_model: string;
  cpu_cores: number;
  agent_version: string;
}

export interface Server {
  id: number;
  name: string;
  group: string;
  note: string;
  host?: HostInfo;
  cpu: number;
  mem_used: number;
  mem_total: number;
  disk_used: number;
  disk_total: number;
  net_in_speed: number;
  net_out_speed: number;
  load1: number;
  uptime: number;
  online: boolean;
  last_seen: string;
}

export interface Alert {
  id: number;
  name: string;
  metric: string;
  min: number | null;
  max: number | null;
  duration: number;
  notify: boolean;
  webhook_id: number;
  enabled: boolean;
}

export interface Notification {
  id: number;
  name: string;
  url: string;
  method: string;
  headers: string;
  body: string;
}

export interface Cron {
  id: number;
  name: string;
  expression: string;
  command: string;
  server_ids: string;
  enabled: boolean;
  last_result: string;
  last_run_at: string;
}

export interface MetricPoint {
  ts: number;
  cpu: number;
  net_in: number;
  net_out: number;
  load1: number;
  mem_used: number;
  mem_total: number;
  disk_used: number;
  disk_total: number;
}

let token: string | null = localStorage.getItem("argus-token");

export function setToken(t: string | null) {
  token = t;
  if (t) localStorage.setItem("argus-token", t);
  else localStorage.removeItem("argus-token");
}
export function getToken() {
  return token;
}

// 统一响应壳（对齐 nezha 风格）：{"success":true,"data":...,"pagination":...}
interface ApiResponse<T> {
  success: boolean;
  data: T;
  error?: string;
  pagination?: { offset: number; limit: number; total: number };
}

async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (token) headers.Authorization = `Bearer ${token}`;
  const res = await fetch(path, { ...options, headers });
  if (res.status === 401) {
    setToken(null);
    window.location.href = "/login";
    throw new Error("unauthorized");
  }
  const body = (await res.json().catch(() => ({}))) as ApiResponse<T>;
  if (!res.ok || body.success === false) {
    throw new Error(body.error || `HTTP ${res.status}`);
  }
  return body.data;
}

export const api = {
  login: (username: string, password: string) =>
    request<{ token: string; username: string }>("/api/v1/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    }).then((r) => r),

  servers: () => request<{ servers: Server[] }>("/api/v1/servers"),
  createServer: (s: { name: string; group: string; note: string }) =>
    request<{ server: Server; secret: string }>("/api/v1/servers", { method: "POST", body: JSON.stringify(s) }),
  updateServer: (id: number, s: { name: string; group: string; note: string }) =>
    request<Server>(`/api/v1/servers/${id}`, { method: "PUT", body: JSON.stringify(s) }),
  deleteServer: (id: number) => request(`/api/v1/servers/${id}`, { method: "DELETE" }),
  exec: (id: number, command: string, timeout = 30) =>
    request<{ output: string; code: number; error?: string }>(`/api/v1/servers/${id}/exec`, {
      method: "POST",
      body: JSON.stringify({ command, timeout }),
    }),
  metrics: (id: number, period: "1h" | "24h" | "7d") =>
    request<{ period: string; points: MetricPoint[] }>(`/api/v1/servers/${id}/metrics?period=${period}`),

  alerts: () => request<{ alerts: Alert[] }>("/api/v1/alerts"),
  saveAlert: (a: Partial<Alert> & { id?: number }) =>
    a.id
      ? request<Alert>(`/api/v1/alerts/${a.id}`, { method: "PUT", body: JSON.stringify(a) })
      : request<Alert>("/api/v1/alerts", { method: "POST", body: JSON.stringify(a) }),
  deleteAlert: (id: number) => request(`/api/v1/alerts/${id}`, { method: "DELETE" }),

  notifications: () => request<{ notifications: Notification[] }>("/api/v1/notifications"),
  saveNotification: (n: Partial<Notification> & { id?: number }) =>
    n.id
      ? request<Notification>(`/api/v1/notifications/${n.id}`, { method: "PUT", body: JSON.stringify(n) })
      : request<Notification>("/api/v1/notifications", { method: "POST", body: JSON.stringify(n) }),
  deleteNotification: (id: number) => request(`/api/v1/notifications/${id}`, { method: "DELETE" }),

  crons: () => request<{ crons: Cron[] }>("/api/v1/crons"),
  saveCron: (c: Partial<Cron> & { id?: number }) =>
    c.id
      ? request<Cron>(`/api/v1/crons/${c.id}`, { method: "PUT", body: JSON.stringify(c) })
      : request<Cron>("/api/v1/crons", { method: "POST", body: JSON.stringify(c) }),
  deleteCron: (id: number) => request(`/api/v1/crons/${id}`, { method: "DELETE" }),
  runCron: (id: number) => request<{ result: string }>(`/api/v1/crons/${id}/run`, { method: "POST" }),
};

export function wsUrl(path: string): string {
  const proto = location.protocol === "https:" ? "wss" : "ws";
  return `${proto}://${location.host}${path}?token=${encodeURIComponent(token || "")}`;
}
