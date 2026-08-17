// API 类型与请求封装（对齐 server/internal/api 返回结构）

export interface HostInfo {
  hostname: string;
  platform: string;
  platform_version: string;
  os: string;
  arch: string;
  kernel_version: string;
  cpu_model: string;
  cpu_cores: number;
  agent_version: string;
  ip: string;
  ipv4: string;
  ipv6: string;
  country_code: string;
}

export interface Availability { available: boolean; reason?: string }
export interface GPUDevice { index: number; name: string; util: number; mem_used: number; mem_total: number }
export interface GPUReport extends Availability { devices?: GPUDevice[] }

// ---- Agent 能力开关与网卡/挂载过滤（配置下发） ----

/** Agent 能力开关：与 protocol.Capabilities 七字段一一对应。 */
export interface CapabilitiesConfig {
  metrics: boolean;
  probe: boolean;
  command: boolean;
  terminal: boolean;
  files: boolean;
  upgrade: boolean;
  nat: boolean;
}

export const DEFAULT_CAPABILITIES: CapabilitiesConfig = {
  metrics: true,
  probe: true,
  command: true,
  terminal: true,
  files: true,
  upgrade: true,
  nat: true,
};

/** 配置下发请求体（服务端 serverApplyConfig 支持的全部字段）。 */
export interface ApplyServerConfig {
  server_url?: string;
  interval?: number;
  secret?: string;
  capabilities?: CapabilitiesConfig;
  interface_include?: string[];
  interface_exclude?: string[];
  mount_include?: string[];
  mount_exclude?: string[];
}

/** 逗号分隔 → 去空白/去空数组（配置下发 include/exclude glob 用）。 */
export function parseCommaList(value: string): string[] {
  return value
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
}

// ---- 通用 ----

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
  gpu: GPUReport;
  process_count: number;
  tcp_established: number;
  tcp_listen: number;
  udp_count: number;
  disk_read_speed: number;
  disk_write_speed: number;
  disk_read_iops: number;
  disk_write_iops: number;
  disk_io_availability: Availability;
  socket_availability: Availability;
  process_availability: Availability;
  temperature_availability: Availability;
  uptime: number;
  latency_ms: number;
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
  slo_target?: number;
  traffic_quota_bytes: number;
  traffic_cycle_day: number;
  traffic_timezone: string;
  traffic_accounting: "sum" | "in" | "out" | "max";
  traffic_usage?: TrafficUsage;
}

export interface TrafficUsage {
  cycle_start: string;
  cycle_end: string;
  timezone: string;
  accounting: "sum" | "in" | "out" | "max";
  in_bytes: number;
  out_bytes: number;
  accounted_bytes: number;
  quota_bytes: number;
  remaining_bytes: number;
  percentage?: number;
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

export interface ClipboardItem {
  id: number;
  user_id: number;
  title: string;
  content: string;
  created_at: string;
}

/** 批量操作中单台服务器的独立结果（逐机回执，对齐 server batchServerResult）。 */
export interface BatchServerResult {
  server_id: number;
  server_name: string;
  status: "ok" | "offline" | "not_found" | "no_ip" | "error";
  error?: string;
  profile_id?: number;
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
  id: number;
  url: string;
  sha256: string;
  version: string;
  status: string;
  concurrency: number;
  target_count: number;
  started_at: string | null;
  finished_at: string | null;
  created_at: string;
  results: Array<{ id: number; server_id: number; name: string; status: string; error?: string; started_at: string | null; finished_at: string | null }>;
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
  // 自定义通知模板（可空）：首行为标题、其余为正文；支持 {{event}}/{{server.*}}/{{rule}}/{{metric}}/{{value}}/{{threshold}}/{{time}} 等变量
  template: string;
  enabled: boolean;
  acked_at: string | null;
  acked_by: string;
  silence_from: string | null;
  silence_to: string | null;
  // 重复提醒（分钟，0=关闭）；升级渠道与升级延迟（分钟）
  repeat_minutes: number;
  escalate_to_channel_id: number;
  escalate_after_minutes: number;
  // 周期流量规则的周期（锚点 + 单位 + 间隔；空单位 = 服务器月度周期）
  cycle_start: string | null;
  cycle_unit: string;
  cycle_interval: number;
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
  extra: string;
  // 渠道限流（0 = 不限）
  rate_limit_per_min: number;
  burst_limit: number;
}

export interface NotificationDelivery {
  id: number;
  webhook_id: number;
  owner_id: number;
  title: string;
  content: string;
  status: "pending" | "sent" | "failed";
  attempts: number;
  max_attempts: number;
  next_retry: string | null;
  last_error: string;
  sent_at: string | null;
  created_at: string;
}

export type NotificationUpdate = Partial<Notification> & {
  clear_url?: boolean;
  clear_headers?: boolean;
  clear_body?: boolean;
  clear_extra?: boolean;
};

export function notificationUpdatePayload(n: NotificationUpdate): NotificationUpdate {
  const payload: NotificationUpdate = { id: n.id };
  if (n.name !== undefined) payload.name = n.name;
  if (n.type !== undefined) payload.type = n.type;
  if (n.method !== undefined) payload.method = n.method;
  if (n.chat_id !== undefined) payload.chat_id = n.chat_id;
  if (n.rate_limit_per_min !== undefined) payload.rate_limit_per_min = n.rate_limit_per_min;
  if (n.burst_limit !== undefined) payload.burst_limit = n.burst_limit;
  if (n.clear_url) payload.clear_url = true;
  else if (n.url && !n.url.endsWith("/***")) payload.url = n.url;
  if (n.clear_headers) payload.clear_headers = true;
  else if (n.headers) payload.headers = n.headers;
  if (n.clear_body) payload.clear_body = true;
  else if (n.body) payload.body = n.body;
  if (n.clear_extra) payload.clear_extra = true;
  else if (n.extra) payload.extra = n.extra;
  return payload;
}

export interface Cron {
  id: number;
  name: string;
  expression: string;
  command: string;
  server_ids: string;
  enabled: boolean;
  skip_if_running: boolean;
  last_result: string;
  last_run_at: string;
}

export interface TaskRunResult {
  id: number;
  run_id: number;
  server_id: number;
  server_name: string;
  status: string;
  exit_code: number;
  duration_ms: number;
  stdout: string;
  stderr: string;
  error: string;
  truncated: boolean;
}

export interface TaskRun {
  id: number;
  cron_id: number;
  trigger: string;
  status: string;
  target_count: number;
  started_at: string | null;
  finished_at: string | null;
  duration_ms: number;
  error: string;
  created_at: string;
  results?: TaskRunResult[];
}

export type HTTPMethod = "GET" | "HEAD" | "POST" | "PUT" | "PATCH" | "DELETE";

export interface ServiceItem {
  id: number;
  owner_id: number;
  server_id: number;
  name: string;
  type: "http" | "tcp" | "ping" | "command";
  target: string;
  interval: number;
  enabled: boolean;
  hidden: boolean;
  notify: boolean;
  notify_webhook_id: number;
  notification_group_id: number;
  http_method: HTTPMethod;
  verify_tls: boolean | null;
  timeout: number;
  expected_status_min: number;
  expected_status_max: number;
  expected_statuses: string;
  ping_count: number;
  cert_warn: boolean;
  request_headers: string;
  request_body: string;
  assert_contains: string;
  failure_trigger_cron_id: number;
  recovery_trigger_cron_id: number;
  last_up: boolean | null;
  last_delay: number | null;
  last_check_at: number;
  today_up_rate: number | null;
  availability: number | null;
  min_delay: number | null;
  avg_delay: number | null;
  max_delay: number | null;
  delay_p50: number | null;
  delay_p95: number | null;
  delay_p99: number | null;
  delay_stddev_ms: number | null;
  delay_jitter_ms: number | null;
  loss_rate: number | null;
  status_code: number | null;
  cert_days: number | null;
  dns_ms: number | null;
  connect_ms: number | null;
  tls_ms: number | null;
  ttfb_ms: number | null;
}

export interface ServiceHistoryPoint {
  ts: number;
  up_rate: number;
  delay: number;
}

// 网络测试结果类型
export interface TraceHop {
  hop: number;
  ip: string;
  rtt_ms: number;
  loss: number;
  reached: boolean;
}

export interface TraceResult {
  ok: boolean;
  error?: string;
  hops: TraceHop[];
  raw_text?: string;
  exit_code?: number;
  truncated?: boolean;
}

export interface TraceMeshItem {
  source_id: number;
  source_name: string;
  target: string;
  trace: TraceResult;
}

export interface BandwidthResult {
  ok: boolean;
  error?: string;
  port?: number;
  bits_per_sec?: number;
  bytes_sent?: number;
  duration_ms?: number;
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

export interface DDNSRecordState {
  id: number;
  domain: string;
  record_type: "A" | "AAAA";
  status: "pending" | "success" | "retrying" | "stopped";
  last_ip: string;
  last_attempt: string | null;
  last_success: string | null;
  last_error: string;
  retry_count: number;
  next_retry: string | null;
}

export interface DDNSProfile {
  id: number;
  owner_id: number;
  server_id: number;
  name: string;
  provider: "cloudflare" | "webhook";
  record_type: "A" | "AAAA" | "dual";
  domains: string;
  webhook_url: string;
  webhook_method: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  webhook_headers: string;
  webhook_body: string;
  last_ip: string;
  last_updated: string;
  enabled: boolean;
  created_at: string;
  records: DDNSRecordState[];
}

export interface NATProfile {
  id: number;
  owner_id: number;
  server_id: number;
  domain: string;
  target_addr: string;
  enabled: boolean;
  created_at: string;
  /** 运行时状态（HTTP 隧道代理返回）：online / offline */
  status?: string;
  /** 该服务器当前活跃隧道数 */
  active_connections?: number;
  /** 每服务器并发隧道上限 */
  server_connection_limit?: number;
  /** 该 owner 当前活跃隧道数 */
  owner_active_connections?: number;
  /** 每用户并发隧道上限 */
  owner_connection_limit?: number;
}

export interface NATListResponse {
  nats: NATProfile[];
  limits?: { server: number; user: number };
  reserved_hosts?: string[];
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
  process_count: number;
  tcp_established: number;
  tcp_listen: number;
  udp_count: number;
  disk_read_speed: number;
  disk_write_speed: number;
  disk_read_iops: number;
  disk_write_iops: number;
}

/** 指标对比：单台服务器的历史点序列（GET /api/v1/metrics/compare）。 */
export interface CompareSeries {
  server_id: number;
  server_name: string;
  points: MetricPoint[];
}

// ---- 管理端资源排行（GET /api/v1/admin/top，实时快照取数、无历史聚合）----

/** 资源排行指标：cpu/mem/disk 为百分比，net_in/net_out 为 B/s，latency 为毫秒。 */
export type TopMetric = "cpu" | "mem" | "disk" | "net_in" | "net_out" | "latency";

/** 资源排行单行：value 为排序值；used/total 仅 mem/disk 返回（用量占比展示用）。 */
export interface TopServerEntry {
  server_id: number;
  server_name: string;
  value: number;
  used?: number;
  total?: number;
}

// ---- 状态页：事故 / 维护窗口 / SLA ----

export interface Incident {
  id: number;
  owner_id: number;
  title: string;
  severity: "minor" | "major" | "critical";
  status: "ongoing" | "resolved";
  server_ids: string;
  notes: string;
  start_at: string;
  end_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface MaintenanceWindow {
  id: number;
  owner_id: number;
  title: string;
  server_ids: string;
  start_at: string;
  end_at: string;
  recurring: boolean;
  created_at: string;
  updated_at: string;
}

export interface SlaMonth {
  month: string;
  uptime_minutes: number;
  eligible_minutes: number;
  maintenance_minutes: number;
  availability: number | null;
  slo_target: number;
  slo_met: boolean | null;
}

// ---- 定时加密备份（里程碑9）----

export interface BackupSchedule {
  id: number;
  name: string;
  enabled: boolean;
  cron: string;
  target: string; // http(s) PUT URL 或本地绝对目录
  keep_count: number;
  key_source: string; // 密钥来源标签（env:/file:/jwt:），不含密钥
  key_id: string; // 派生密钥指纹
  last_run_at: string | null;
  last_status: "" | "running" | "success" | "failed";
  last_error: string;
  last_size: number;
  created_at: string;
  updated_at: string;
}

export interface BackupRun {
  id: number;
  schedule_id: number;
  trigger: "cron" | "manual";
  status: "success" | "failed";
  target: string;
  size: number;
  sha256: string;
  error: string;
  duration_ms: number;
  created_at: string;
}

export interface DrillResult {
  ok: boolean;
  key_id: string;
  source: string;
  db_size: number;
  integrity: string;
  restore_note: string;
}

export interface Me {
  username: string;
  role: string;
  two_fa_enabled: boolean;
}

/** 在线访客/用户（GET /admin/online）。 */
export interface OnlineUser {
  ip: string;
  username: string; // 游客为空
  auth_method: "jwt" | "pat" | "guest";
  last_active_at: string;
  connections: number; // WS/终端等长连接数
}

/** IP 封禁记录（GET /admin/waf/bans）。 */
export interface WafBan {
  id: number;
  ip: string;
  reason: string;
  count: number;
  source: "rate" | "login" | "manual";
  banned_at: string;
  expire_at: string | null; // null = 永久
  created_at: string;
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
  permissions: {
    allow_fetch: boolean;
    fetch_domains: string[];
    allow_notify: boolean;
  };
  events: string[];
  logs: string[];
  last_run: string;
  last_status: string;
  last_error: string;
  run_count: number;
  running: boolean;
}

export interface ThemeInfo {
  name: string;
  display_name: string;
  version: string;
  argus: string;
  author: string;
  entry: string;
  preview: string;
  active: boolean;
  rollback: boolean;
}

export interface MarketThemeEntry {
  name: string;
  display_name: string;
  version: string;
  author: string;
  description: string;
  download_url: string;
  sha256: string;
  size: number;
  installed: boolean;
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
  /** 稳定错误码（后端可选输出，如 "server.offline"），前端按 code 翻译、未知回退 error 原文。 */
  code?: string;
  pagination?: { offset: number; limit: number; total: number };
}

/** 带稳定错误码的 API 错误：code 缺失或未知时按 message（后端原文）展示。 */
export class ApiError extends Error {
  readonly code?: string;
  readonly status?: number;
  constructor(message: string, code?: string, status?: number) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
  }
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
    throw new ApiError(body.error || `HTTP ${res.status}`, body.code, res.status);
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

  backupSchedules: () => request<{ schedules: BackupSchedule[] }>("/api/v1/admin/backup-schedules"),
  createBackupSchedule: (s: { name: string; cron: string; target: string; enabled?: boolean; keep_count?: number }) =>
    request<BackupSchedule>("/api/v1/admin/backup-schedules", { method: "POST", body: JSON.stringify(s) }),
  updateBackupSchedule: (id: number, s: Partial<BackupSchedule>) =>
    request<BackupSchedule>(`/api/v1/admin/backup-schedules/${id}`, { method: "PUT", body: JSON.stringify(s) }),
  deleteBackupSchedule: (id: number) => request(`/api/v1/admin/backup-schedules/${id}`, { method: "DELETE" }),
  runBackupSchedule: (id: number) => request<{ ok: boolean; schedule_id: number }>(`/api/v1/admin/backup-schedules/${id}/run`, { method: "POST" }),
  backupRuns: (id: number) => request<{ runs: BackupRun[] }>(`/api/v1/admin/backup-schedules/${id}/runs`),
  backupDrill: (id: number, file?: File) => {
    const form = new FormData();
    if (file) form.append("file", file);
    return request<DrillResult>(`/api/v1/admin/backup-schedules/${id}/drill`, { method: "POST", body: form });
  },

  servers: () => request<{ servers: Server[] }>("/api/v1/servers"),
  createServer: (s: { name: string; group: string; note: string; traffic_quota_bytes?: number; traffic_cycle_day?: number; traffic_timezone?: string; traffic_accounting?: string }) =>
    request<{ server: Server; secret: string }>("/api/v1/servers", { method: "POST", body: JSON.stringify(s) }),
  updateServer: (id: number, s: Partial<Server>) =>
    request<Server>(`/api/v1/servers/${id}`, { method: "PUT", body: JSON.stringify(s) }),
  deleteServer: (id: number) => request(`/api/v1/servers/${id}`, { method: "DELETE" }),
  serverTraffic: (id: number, period: "day" | "month" | "year") =>
    request<{ period: string; points: TrafficPoint[]; usage: TrafficUsage }>(`/api/v1/servers/${id}/traffic?period=${period}`),
  batchDeleteServers: (ids: number[]) =>
    request<{ ok: boolean; deleted: number }>("/api/v1/batch-delete/servers", { method: "POST", body: JSON.stringify({ ids }) }),
  batchMoveServers: (ids: number[], group: string) =>
    request<{ ok: boolean; moved: number }>("/api/v1/batch-move/servers", { method: "POST", body: JSON.stringify({ ids, group }) }),
  batchConfigServers: (ids: number[], cfg: Omit<ApplyServerConfig, "secret">) =>
    request<{ results: BatchServerResult[] }>("/api/v1/batch-config/servers", { method: "POST", body: JSON.stringify({ ids, ...cfg }) }),
  batchDDNSServers: (ids: number[], profile_id: number) =>
    request<{ results: BatchServerResult[] }>("/api/v1/batch-ddns/servers", { method: "POST", body: JSON.stringify({ ids, profile_id }) }),
  clipboard: () => request<{ items: ClipboardItem[] }>("/api/v1/clipboard"),
  createClipboard: (item: { title?: string; content: string }) =>
    request<ClipboardItem>("/api/v1/clipboard", { method: "POST", body: JSON.stringify(item) }),
  updateClipboard: (id: number, item: { title?: string; content?: string }) =>
    request<ClipboardItem>(`/api/v1/clipboard/${id}`, { method: "PUT", body: JSON.stringify(item) }),
  deleteClipboard: (id: number) => request(`/api/v1/clipboard/${id}`, { method: "DELETE" }),
  plugins: () => request<{ plugins: PluginInfo[] }>("/api/v1/plugins"),
  pluginToggle: (name: string, enabled: boolean) =>
    request<{ ok: boolean }>(`/api/v1/plugins/${encodeURIComponent(name)}/toggle`, { method: "POST", body: JSON.stringify({ enabled }) }),
  pluginApprove: (name: string, approved: boolean) =>
    request<{ ok: boolean }>(`/api/v1/plugins/${encodeURIComponent(name)}/approve`, { method: "POST", body: JSON.stringify({ approved }) }),
  pluginRun: (name: string) => request<{ ok: boolean }>(`/api/v1/plugins/${encodeURIComponent(name)}/run`, { method: "POST" }),
  pluginDelete: (name: string) => request(`/api/v1/plugins/${encodeURIComponent(name)}`, { method: "DELETE" }),
  pluginMarket: () => request<{ plugins: Array<{ name: string; description: string; version: string; installed: boolean }> }>("/api/v1/plugins/market"),
  pluginInstall: (name: string) => request<{ ok: boolean }>(`/api/v1/plugins/market/${encodeURIComponent(name)}/install`, { method: "POST" }),
  themeList: () => request<{ themes: ThemeInfo[] }>("/api/v1/themes"),
  themeUpload: (file: File, sha256 = "") => {
    const form = new FormData();
    form.append("file", file);
    if (sha256) form.append("sha256", sha256);
    return request<{ theme: ThemeInfo }>("/api/v1/themes/upload", { method: "POST", body: form });
  },
  themeActivate: (name: string) => request<{ ok: boolean; active: string }>(`/api/v1/themes/${encodeURIComponent(name)}/activate`, { method: "POST" }),
  themeRollback: (name: string) => request<{ ok: boolean }>(`/api/v1/themes/${encodeURIComponent(name)}/rollback`, { method: "POST" }),
  themeDelete: (name: string) => request<{ ok: boolean; active: string }>(`/api/v1/themes/${encodeURIComponent(name)}`, { method: "DELETE" }),
  themeMarket: () => request<{ themes: MarketThemeEntry[] }>("/api/v1/themes/market"),
  themeMarketInstall: (name: string) => request<{ ok: boolean }>(`/api/v1/themes/market/${encodeURIComponent(name)}/install`, { method: "POST" }),
  groups: () => request<{ groups: ServerGroup[] }>("/api/v1/groups"),
  createGroup: (name: string) => request<ServerGroup>("/api/v1/groups", { method: "POST", body: JSON.stringify({ name }) }),
  deleteGroup: (id: number) => request(`/api/v1/groups/${id}`, { method: "DELETE" }),
  applyServerConfig: (id: number, cfg: ApplyServerConfig) =>
    request<{ ok: boolean }>(`/api/v1/servers/${id}/config`, { method: "POST", body: JSON.stringify(cfg) }),
  exec: (id: number, command: string, timeout = 30, twoFACode = "") =>
    request<{ output: string; code: number; error?: string }>(`/api/v1/servers/${id}/exec`, {
      method: "POST",
      headers: twoFACode ? { "X-2FA-Code": twoFACode } : {},
      body: JSON.stringify({ command, timeout }),
    }),
  metrics: (id: number, period: "1h" | "24h" | "7d") =>
    request<{ period: string; points: MetricPoint[] }>(`/api/v1/servers/${id}/metrics?period=${period}`),
  metricsCompare: (ids: number[], period: "1h" | "24h" | "7d") =>
    request<{ period: string; series: CompareSeries[] }>(`/api/v1/metrics/compare?ids=${ids.join(",")}&period=${period}`),
  // 管理端资源排行（admin 全量 / owner 自有；仅在线服务器实时快照）
  top: (metric: TopMetric, limit = 10) =>
    request<{ metric: TopMetric; limit: number; servers: TopServerEntry[] }>(
      `/api/v1/admin/top?metric=${metric}&limit=${limit}`,
    ),

  // 网络测试：路由追踪 / 多源追踪 / 带宽测速
  trace: (id: number, target: string, opts: { protocol?: string; max_hops?: number; timeout_sec?: number } = {}) =>
    request<{ trace: TraceResult; server_id: number; server_name: string; target: string }>(`/api/v1/servers/${id}/trace`, {
      method: "POST",
      body: JSON.stringify({ target, ...opts }),
    }),
  traceMesh: (body: { source_ids: number[]; targets: string[]; mode?: string; protocol?: string; max_hops?: number }) =>
    request<{ results: TraceMeshItem[]; mode: string }>("/api/v1/network-test/trace-mesh", {
      method: "POST",
      body: JSON.stringify(body),
    }),
  bandwidthTest: (body: { source_id: number; target: string; duration?: number; parallel?: number }) =>
    request<{ result: BandwidthResult; source_id: number; source_name: string; target: string }>(
      "/api/v1/network-test/bandwidth",
      { method: "POST", body: JSON.stringify(body) },
    ),

  alerts: () => request<{ alerts: Alert[] }>("/api/v1/alerts"),
  saveAlert: (a: Partial<Alert> & { id?: number }) =>
    a.id
      ? request<Alert>(`/api/v1/alerts/${a.id}`, { method: "PUT", body: JSON.stringify(a) })
      : request<Alert>("/api/v1/alerts", { method: "POST", body: JSON.stringify(a) }),
  deleteAlert: (id: number) => request(`/api/v1/alerts/${id}`, { method: "DELETE" }),
  ackAlert: (id: number) => request<{ ok: boolean; acked_at: string; acked_by: string }>(`/api/v1/alerts/${id}/ack`, { method: "POST" }),
  unackAlert: (id: number) => request<{ ok: boolean }>(`/api/v1/alerts/${id}/ack`, { method: "DELETE" }),
  silenceAlert: (id: number, until: string) =>
    request<{ ok: boolean; silence_from: string; silence_to: string }>(`/api/v1/alerts/${id}/silence`, {
      method: "POST",
      body: JSON.stringify({ until }),
    }),
  unsilenceAlert: (id: number) => request<{ ok: boolean }>(`/api/v1/alerts/${id}/silence`, { method: "DELETE" }),

  notifications: () => request<{ notifications: Notification[] }>("/api/v1/notifications"),
  saveNotification: (n: NotificationUpdate) =>
    n.id
      ? request<Notification>(`/api/v1/notifications/${n.id}`, { method: "PUT", body: JSON.stringify(notificationUpdatePayload(n)) })
      : request<Notification>("/api/v1/notifications", { method: "POST", body: JSON.stringify(n) }),
  deleteNotification: (id: number) => request(`/api/v1/notifications/${id}`, { method: "DELETE" }),
  deliveries: (offset = 0, limit = 50) =>
    request<{ deliveries: NotificationDelivery[]; pagination?: { total: number } }>(
      `/api/v1/notifications/deliveries?offset=${offset}&limit=${limit}`
    ),
  retryDelivery: (id: number) => request<{ ok: boolean }>(`/api/v1/notifications/deliveries/${id}/retry`, { method: "POST" }),
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
  trafficReport: () =>
    request<{ webhook_id: number; period: string; hour: number; weekday: number; day: number; enabled: boolean }>("/api/v1/traffic-report"),
  saveTrafficReport: (cfg: { webhook_id: number; period: string; hour: number; weekday: number; day: number; enabled: boolean }) =>
    request<{ ok: boolean }>("/api/v1/traffic-report", { method: "POST", body: JSON.stringify(cfg) }),

  transfers: () => request<{ transfers: TransferRecord[] }>("/api/v1/server-transfers"),
  createTransfer: (server_id: number, to_user_id: number) =>
    request<{ transfer: TransferRecord; new_secret: string; note: string }>("/api/v1/server-transfers", { method: "POST", body: JSON.stringify({ server_id, to_user_id }) }),
  cancelTransfer: (id: number) => request<{ ok: boolean }>(`/api/v1/server-transfers/${id}/cancel`, { method: "POST" }),
  upgradeJobs: () => request<{ jobs: UpgradeJob[] }>("/api/v1/upgrade-jobs"),
  createUpgradeJob: (j: { server_ids: number[]; url: string; sha256: string; version: string; concurrency: number }) =>
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

  // 一键安装命令（哪吒风格）
  installCommand: () => request<{ command: string; script_url: string; ws_url: string }>("/api/v1/install/command"),
  serverInstallCommand: (id: number) =>
    request<{ command: string; script_url: string; ws_url: string; server_id: number }>(`/api/v1/servers/${id}/install-command`),

  settings: () => request<{ settings: Record<string, string> }>("/api/v1/settings"),
  saveSettings: (settings: Record<string, string>) =>
    request(`/api/v1/settings`, { method: "POST", body: JSON.stringify({ settings }) }),
  sessions: () => request<{ sessions: Session[] }>("/api/v1/sessions"),
  kickSession: (id: number) => request(`/api/v1/sessions/${id}`, { method: "DELETE" }),
  kickAllSessions: (userId?: number) => request(`/api/v1/sessions${userId ? `?user_id=${userId}` : ""}`, { method: "DELETE" }),

  // WAF 封禁与在线用户（admin）
  onlineUsers: () => request<{ online: OnlineUser[] }>("/api/v1/admin/online"),
  wafBans: (offset = 0, limit = 50) =>
    request<{ bans: WafBan[]; pagination?: { total: number } }>(`/api/v1/admin/waf/bans?offset=${offset}&limit=${limit}`),
  banIP: (ip: string, reason = "", hours = 24) =>
    request<{ ban: WafBan }>("/api/v1/admin/waf/ban", { method: "POST", body: JSON.stringify({ ip, reason, hours }) }),
  unbanIP: (ip: string) => request(`/api/v1/admin/waf/ban/${encodeURIComponent(ip)}`, { method: "DELETE" }),

  ddns: () => request<{ profiles: DDNSProfile[] }>("/api/v1/ddns"),
  saveDDNS: async (p: Partial<DDNSProfile> & { id?: number; access_key?: string }): Promise<DDNSProfile | { ok: boolean }> =>
    p.id
      ? request<{ ok: boolean }>(`/api/v1/ddns/${p.id}`, { method: "PUT", body: JSON.stringify(p) })
      : request<DDNSProfile>("/api/v1/ddns", { method: "POST", body: JSON.stringify(p) }),
  deleteDDNS: (id: number) => request(`/api/v1/ddns/${id}`, { method: "DELETE" }),
  testDDNS: (id: number) => request<{ ipv4: string; ipv6: string; records: DDNSRecordState[] }>(`/api/v1/ddns/${id}/test`, { method: "POST" }),

  nats: () => request<NATListResponse>("/api/v1/nats"),
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
  runCron: (id: number) => request<{ run_id: number }>(`/api/v1/crons/${id}/run`, { method: "POST" }),
  cronRuns: (id: number) => request<{ runs: TaskRun[] }>(`/api/v1/crons/${id}/runs?limit=20`),
  cronRun: (cronId: number, runId: number) => request<TaskRun>(`/api/v1/crons/${cronId}/runs/${runId}`),

  incidents: () => request<{ incidents: Incident[] }>("/api/v1/incidents"),
  saveIncident: (i: Partial<Incident> & { id?: number; start_at?: string; end_at?: string }) =>
    i.id
      ? request<Incident>(`/api/v1/incidents/${i.id}`, { method: "PUT", body: JSON.stringify(i) })
      : request<Incident>("/api/v1/incidents", { method: "POST", body: JSON.stringify(i) }),
  resolveIncident: (id: number) => request<Incident>(`/api/v1/incidents/${id}/resolve`, { method: "POST" }),
  deleteIncident: (id: number) => request(`/api/v1/incidents/${id}`, { method: "DELETE" }),

  maintenanceWindows: () => request<{ windows: MaintenanceWindow[] }>("/api/v1/maintenance-windows"),
  saveMaintenanceWindow: (w: Partial<MaintenanceWindow> & { id?: number }) =>
    w.id
      ? request<MaintenanceWindow>(`/api/v1/maintenance-windows/${w.id}`, { method: "PUT", body: JSON.stringify(w) })
      : request<MaintenanceWindow>("/api/v1/maintenance-windows", { method: "POST", body: JSON.stringify(w) }),
  deleteMaintenanceWindow: (id: number) => request(`/api/v1/maintenance-windows/${id}`, { method: "DELETE" }),

  serverSla: (id: number, months = 6) =>
    request<{ server_id: number; slo_target: number; months: SlaMonth[] }>(`/api/v1/servers/${id}/sla?months=${months}`),
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
