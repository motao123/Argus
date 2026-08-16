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
  temperature: number;
  gpu_util: number;
  uptime: number;
  online: boolean;
  last_seen: string;
  price: number;
  cycle_days: number;
  expire_at: string | null;
  auto_renew: boolean;
  tags: string;
  sort_order: number;
  hidden: boolean;
  owner_id: number;
}

export interface TrafficPoint {
  ts: number;
  in: number;
  out: number;
}

export interface ServerGroup {
  id: number;
  name: string;
}

export interface AuditLog {
  id: number;
  user_id: number;
  username: string;
  action: string;
  detail: string;
  ip: string;
  created_at: string;
}

export interface TransferRecord {
  id: number;
  server_id: number;
  server_name: string;
  to_username: string;
  status: string;
  created_at: string;
}

export interface UpgradeJob {
  id: string;
  url: string;
  version: string;
  created_at: string;
  results: Record<string, { server_id: number; name: string; status: string; error?: string }>;
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
  group_id: number;
  trigger_cron_id: number;
  trigger_ratio: number | null;
  enabled: boolean;
}

export interface Notification {
  id: number;
  name: string;
  type: string;
  url: string;
  method: string;
  headers: string;
  body: string;
  chat_id: string;
}

export type NotificationUpdate = Partial<Notification> & {
  clear_url?: boolean;
  clear_headers?: boolean;
  clear_body?: boolean;
};

export function notificationUpdatePayload(n: NotificationUpdate): NotificationUpdate {
  const payload: NotificationUpdate = { id: n.id };
  if (n.name !== undefined) payload.name = n.name;
  if (n.type !== undefined) payload.type = n.type;
  if (n.method !== undefined) payload.method = n.method;
  if (n.chat_id !== undefined) payload.chat_id = n.chat_id;
  if (n.clear_url) payload.clear_url = true;
  else if (n.url && !n.url.endsWith("/***")) payload.url = n.url;
  if (n.clear_headers) payload.clear_headers = true;
  else if (n.headers) payload.headers = n.headers;
  if (n.clear_body) payload.clear_body = true;
  else if (n.body) payload.body = n.body;
  return payload;
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

export interface PluginInfo {
  name: string;
  version: string;
  description: string;
  cron: string;
  enabled: boolean;
  approved: boolean;
  permissions_allow_fetch: boolean;
  logs: string[];
  last_run: string;
}

export interface NotificationGroup {
  id: number;
  name: string;
  member_ids: string;
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
  client_secret_configured?: boolean;
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
  saveOAuthConfig: (cfg: Partial<OAuthConfig> & { name: string; client_secret?: string; clear_client_secret?: boolean }) =>
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
  updateServer: (id: number, s: Partial<Server>) =>
    request<Server>(`/api/v1/servers/${id}`, { method: "PUT", body: JSON.stringify(s) }),
  deleteServer: (id: number) => request(`/api/v1/servers/${id}`, { method: "DELETE" }),
  serverTraffic: (id: number, period: "day" | "month" | "year") =>
    request<{ period: string; points: TrafficPoint[] }>(`/api/v1/servers/${id}/traffic?period=${period}`),
  batchDeleteServers: (ids: number[]) =>
    request<{ ok: boolean; deleted: number }>("/api/v1/batch-delete/servers", { method: "POST", body: JSON.stringify({ ids }) }),
  batchMoveServers: (ids: number[], group: string) =>
    request<{ ok: boolean; moved: number }>("/api/v1/batch-move/servers", { method: "POST", body: JSON.stringify({ ids, group }) }),
  plugins: () => request<{ plugins: PluginInfo[] }>("/api/v1/plugins"),
  pluginToggle: (name: string, enabled: boolean) =>
    request<{ ok: boolean }>(`/api/v1/plugins/${encodeURIComponent(name)}/toggle`, { method: "POST", body: JSON.stringify({ enabled }) }),
  pluginApprove: (name: string, approved: boolean) =>
    request<{ ok: boolean }>(`/api/v1/plugins/${encodeURIComponent(name)}/approve`, { method: "POST", body: JSON.stringify({ approved }) }),
  pluginRun: (name: string) => request<{ ok: boolean }>(`/api/v1/plugins/${encodeURIComponent(name)}/run`, { method: "POST" }),
  pluginDelete: (name: string) => request(`/api/v1/plugins/${encodeURIComponent(name)}`, { method: "DELETE" }),
  pluginMarket: () => request<{ plugins: Array<{ name: string; description: string; version: string; installed: boolean }> }>("/api/v1/plugins/market"),
  pluginInstall: (name: string) => request<{ ok: boolean }>(`/api/v1/plugins/market/${encodeURIComponent(name)}/install`, { method: "POST" }),
  groups: () => request<{ groups: ServerGroup[] }>("/api/v1/groups"),
  createGroup: (name: string) => request<ServerGroup>("/api/v1/groups", { method: "POST", body: JSON.stringify({ name }) }),
  deleteGroup: (id: number) => request(`/api/v1/groups/${id}`, { method: "DELETE" }),
  applyServerConfig: (id: number, cfg: { server_url?: string; interval?: number; secret?: string }) =>
    request<{ ok: boolean }>(`/api/v1/servers/${id}/config`, { method: "POST", body: JSON.stringify(cfg) }),
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
  saveNotification: (n: NotificationUpdate) =>
    n.id
      ? request<Notification>(`/api/v1/notifications/${n.id}`, { method: "PUT", body: JSON.stringify(notificationUpdatePayload(n)) })
      : request<Notification>("/api/v1/notifications", { method: "POST", body: JSON.stringify(n) }),
  deleteNotification: (id: number) => request(`/api/v1/notifications/${id}`, { method: "DELETE" }),
  testMessage: (webhook_id: number, title?: string, content?: string) =>
    request<{ ok: boolean; sent_to: string }>("/api/v1/test-message", { method: "POST", body: JSON.stringify({ webhook_id, title, content }) }),
  notificationGroups: () => request<{ groups: NotificationGroup[] }>("/api/v1/notification-groups"),
  saveNotificationGroup: (g: Partial<NotificationGroup> & { id?: number }) =>
    g.id
      ? request<{ ok: boolean }>(`/api/v1/notification-groups/${g.id}`, { method: "PUT", body: JSON.stringify(g) })
      : request<NotificationGroup>("/api/v1/notification-groups", { method: "POST", body: JSON.stringify(g) }),
  deleteNotificationGroup: (id: number) => request(`/api/v1/notification-groups/${id}`, { method: "DELETE" }),

  auditLogs: (offset = 0, limit = 50) =>
    request<{ logs: AuditLog[]; pagination?: { total: number } }>(`/api/v1/admin/logs?offset=${offset}&limit=${limit}`),
  offlineNotify: () => request<{ webhook_id: number; offline_after: number; enabled: boolean }>("/api/v1/offline-notify"),
  saveOfflineNotify: (cfg: { webhook_id: number; offline_after: number; enabled: boolean }) =>
    request<{ ok: boolean }>("/api/v1/offline-notify", { method: "POST", body: JSON.stringify(cfg) }),
  trafficReport: () => request<{ webhook_id: number; hour: number; enabled: boolean }>("/api/v1/traffic-report"),
  saveTrafficReport: (cfg: { webhook_id: number; hour: number; enabled: boolean }) =>
    request<{ ok: boolean }>("/api/v1/traffic-report", { method: "POST", body: JSON.stringify(cfg) }),

  transfers: () => request<{ transfers: TransferRecord[] }>("/api/v1/server-transfers"),
  createTransfer: (server_id: number, to_user_id: number) =>
    request<{ transfer: TransferRecord; new_secret: string; note: string }>("/api/v1/server-transfers", { method: "POST", body: JSON.stringify({ server_id, to_user_id }) }),
  cancelTransfer: (id: number) => request<{ ok: boolean }>(`/api/v1/server-transfers/${id}/cancel`, { method: "POST" }),
  upgradeJobs: () => request<{ jobs: UpgradeJob[] }>("/api/v1/upgrade-jobs"),
  createUpgradeJob: (j: { server_ids: number[]; url: string; sha256: string; version: string }) =>
    request<UpgradeJob>("/api/v1/upgrade-jobs", { method: "POST", body: JSON.stringify(j) }),

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
  kickAllSessions: (userId?: number) => request(`/api/v1/sessions${userId ? `?user_id=${userId}` : ""}`, { method: "DELETE" }),

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
