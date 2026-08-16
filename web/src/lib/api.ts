// API 类型与请求封装（对齐 server/internal/api 返回结构）

export interface HostInfo {
  hostname: string;
  platform: string;
  platform_version: string;
  cpu_model: string;
  cpu_cores: number;
  agent_version: string;
  ip: string;
  country_code: string;
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

export interface ServiceItem {
  id: number;
  server_id: number;
  name: string;
  type: string;
  target: string;
  interval: number;
  enabled: boolean;
  last_up: boolean;
  last_delay: number;
  today_up_rate: number;
}

export interface ServiceHistoryPoint {
  ts: number;
  up_rate: number;
  delay: number;
}

export interface FsEntry {
  name: string;
  path: string;
  size: number;
  mode: string;
  is_dir: boolean;
  modified: number;
}

export interface User {
  id: number;
  username: string;
  role: string;
  created_at: string;
}

export interface ApiToken {
  id: number;
  name: string;
  scopes: string;
  server_ids: string;
  expires_at: string | null;
  revoked: boolean;
  created_at: string;
}

export interface Session {
  id: number;
  user_id: number;
  user_agent: string;
  ip: string;
  created_at: string;
  expires_at: string;
}

export interface DDNSProfile {
  id: number;
  owner_id: number;
  server_id: number;
  name: string;
  provider: "cloudflare" | "webhook";
  domains: string;
  webhook_url: string;
  last_ip: string;
  last_updated: string;
  enabled: boolean;
  created_at: string;
}

export interface NATProfile {
  id: number;
  owner_id: number;
  server_id: number;
  domain: string;
  target_addr: string;
  enabled: boolean;
  created_at: string;
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

export interface Me {
  username: string;
  role: string;
  two_fa_enabled: boolean;
}

export interface TwoFASetup {
  secret: string;
  otpauth_url: string;
}

export interface OAuthConfig {
  id: number;
  name: string;
  client_id: string;
  auth_url: string;
  token_url: string;
  user_info_url: string;
  username_field: string;
  admin_logins: string;
  enabled: boolean;
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
  const headers: Record<string, string> = {};
  if (token) headers.Authorization = `Bearer ${token}`;
  // FormData 让浏览器自动设置 multipart boundary
  if (!(options.body instanceof FormData)) headers["Content-Type"] = "application/json";
  const res = await fetch(path, { ...options, headers });
  if (res.status === 401 && !path.includes("/auth/login") && !path.includes("/auth/oauth")) {
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

// requestBlob 用于二维码 PNG、备份下载等非 JSON 响应。
async function requestBlob(path: string, options: RequestInit = {}): Promise<Blob> {
  const headers: Record<string, string> = {};
  if (token) headers.Authorization = `Bearer ${token}`;
  const res = await fetch(path, { ...options, headers });
  if (!res.ok) {
    throw new Error(`HTTP ${res.status}`);
  }
  return res.blob();
}

export const api = {
  login: (username: string, password: string, twoFACode = "") =>
    request<{ token: string; username: string }>("/api/v1/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password, two_fa_code: twoFACode }),
    }).then((r) => r),

  me: () => request<Me>("/api/v1/auth/me"),
  oauthProviders: () => request<{ providers: string[] }>("/api/v1/auth/oauth/providers"),
  consumeOAuthCode: (code: string) =>
    request<{ token: string }>("/api/v1/auth/oauth/consume", { method: "POST", body: JSON.stringify({ code }) }),

  twoFASetup: () => request<TwoFASetup>("/api/v1/auth/2fa/setup"),
  twoFAQRCode: () => requestBlob("/api/v1/auth/2fa/qrcode"),
  twoFAEnable: (code: string) =>
    request<{ ok: boolean }>("/api/v1/auth/2fa/enable", { method: "POST", body: JSON.stringify({ code }) }),
  twoFADisable: (code: string) =>
    request<{ ok: boolean }>("/api/v1/auth/2fa/disable", { method: "POST", body: JSON.stringify({ code }) }),

  oauthConfigs: () => request<{ providers: OAuthConfig[] }>("/api/v1/oauth/providers"),
  saveOAuthConfig: (cfg: Partial<OAuthConfig> & { name: string; client_secret?: string }) =>
    request<{ ok: boolean }>("/api/v1/oauth/providers", { method: "POST", body: JSON.stringify(cfg) }),
  deleteOAuthConfig: (name: string) => request(`/api/v1/oauth/providers/${encodeURIComponent(name)}`, { method: "DELETE" }),

  dbSize: () => request<{ db_size: number; wal_size: number; total: number }>("/api/v1/admin/db/size"),
  dbVacuum: () => request<{ ok: boolean }>("/api/v1/admin/db/vacuum", { method: "POST" }),
  backupDownload: () => requestBlob("/api/v1/admin/backup"),
  backupRestore: (file: File, uploadId: string, offset: number, final: boolean, totalHash: string) => {
    const form = new FormData();
    form.append("file", file);
    form.append("upload_id", uploadId);
    form.append("offset", String(offset));
    form.append("final", final ? "1" : "0");
    form.append("total_hash", totalHash);
    return request<{ ok: boolean; written: number; final: boolean; note?: string }>("/api/v1/admin/backup/restore", {
      method: "POST",
      body: form,
    });
  },

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

  services: () => request<{ services: ServiceItem[] }>("/api/v1/services"),
  saveService: (svc: Partial<ServiceItem> & { id?: number }) =>
    svc.id
      ? request<{ ok: boolean }>(`/api/v1/services/${svc.id}`, { method: "PUT", body: JSON.stringify(svc) })
      : request<ServiceItem>("/api/v1/services", { method: "POST", body: JSON.stringify(svc) }),
  deleteService: (id: number) => request(`/api/v1/services/${id}`, { method: "DELETE" }),
  serviceHistory: (id: number, period: "1d" | "7d" | "30d") =>
    request<{ period: string; points: ServiceHistoryPoint[] }>(`/api/v1/services/${id}/history?period=${period}`),

  files: (serverId: number, path: string) =>
    request<{ path: string; entries: FsEntry[] }>(`/api/v1/files/${serverId}?path=${encodeURIComponent(path)}`),
  fileRead: (serverId: number, path: string, offset = 0, limit = 262144) =>
    request<{ data: string; eof: boolean; size: number }>(`/api/v1/files/${serverId}/read`, {
      method: "POST",
      body: JSON.stringify({ path, offset, limit }),
    }),
  fileWrite: (serverId: number, path: string, dataBase64: string, append = false) =>
    request<{ bytes: number }>(`/api/v1/files/${serverId}/write`, {
      method: "POST",
      body: JSON.stringify({ path, data: dataBase64, append }),
    }),
  fileDelete: (serverId: number, path: string, recursive = false) =>
    request(`/api/v1/files/${serverId}/delete`, {
      method: "POST",
      body: JSON.stringify({ path, recursive }),
    }),

  users: () => request<{ users: User[] }>("/api/v1/users"),
  userSecret: (id: number) => request<{ agent_secret: string }>(`/api/v1/users/${id}/secret`),
  createUser: (u: { username: string; password: string; role: string }) =>
    request<{ user: User; agent_secret: string }>("/api/v1/users", { method: "POST", body: JSON.stringify(u) }),
  deleteUser: (id: number) => request(`/api/v1/users/${id}`, { method: "DELETE" }),

  settings: () => request<{ settings: Record<string, string> }>("/api/v1/settings"),
  saveSettings: (settings: Record<string, string>) =>
    request(`/api/v1/settings`, { method: "POST", body: JSON.stringify({ settings }) }),
  sessions: () => request<{ sessions: Session[] }>("/api/v1/sessions"),
  kickSession: (id: number) => request(`/api/v1/sessions/${id}`, { method: "DELETE" }),

  ddns: () => request<{ profiles: DDNSProfile[] }>("/api/v1/ddns"),
  saveDDNS: async (p: Partial<DDNSProfile> & { id?: number; access_key?: string }): Promise<DDNSProfile | { ok: boolean }> =>
    p.id
      ? request<{ ok: boolean }>(`/api/v1/ddns/${p.id}`, { method: "PUT", body: JSON.stringify(p) })
      : request<DDNSProfile>("/api/v1/ddns", { method: "POST", body: JSON.stringify(p) }),
  deleteDDNS: (id: number) => request(`/api/v1/ddns/${id}`, { method: "DELETE" }),
  testDDNS: (id: number) => request<{ ip: string; results: Record<string, string> }>(`/api/v1/ddns/${id}/test`, { method: "POST" }),

  nats: () => request<{ nats: NATProfile[] }>("/api/v1/nats"),
  saveNAT: async (n: Partial<NATProfile> & { id?: number }): Promise<NATProfile | { ok: boolean }> =>
    n.id
      ? request<{ ok: boolean }>(`/api/v1/nats/${n.id}`, { method: "PUT", body: JSON.stringify(n) })
      : request<NATProfile>("/api/v1/nats", { method: "POST", body: JSON.stringify(n) }),
  deleteNAT: (id: number) => request(`/api/v1/nats/${id}`, { method: "DELETE" }),

  tokens: () => request<{ tokens: ApiToken[] }>("/api/v1/tokens"),
  createToken: (t: { name: string; scopes: string[]; server_ids?: string; expires_in?: number }) =>
    request<{ token: string; id: number }>("/api/v1/tokens", { method: "POST", body: JSON.stringify(t) }),
  revokeToken: (id: number) => request(`/api/v1/tokens/${id}`, { method: "DELETE" }),

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

// countryCodeToFlag 国家码 → 国旗 emoji（US → 🇺🇸）
export function countryFlag(code: string): string {
  if (!code || code.length !== 2) return "";
  return String.fromCodePoint(...[...code.toUpperCase()].map((c) => 0x1f1e6 + c.charCodeAt(0) - 65));
}
