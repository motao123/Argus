import { useMemo, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckSquare, Copy, Download, Globe, KeyRound, Layers, Pencil, Plus, Search, Send, Settings2, Square, TerminalSquare, Trash2 } from "lucide-react";
import { api, ApiError, DEFAULT_CAPABILITIES, parseCommaList, type BatchServerResult, type CapabilitiesConfig, type Server } from "../lib/api";
import { fmtBytes } from "../lib/format";
import { useI18n, type TKey } from "../lib/i18n";

// 能力开关：键与协议字段一致，值为 i18n 标签 key（默认全部启用）。
const CAPABILITY_ITEMS: Array<[keyof CapabilitiesConfig, TKey]> = [
  ["metrics", "servers.cfgCapMetrics"],
  ["probe", "servers.cfgCapProbe"],
  ["command", "servers.cfgCapCommand"],
  ["terminal", "servers.cfgCapTerminal"],
  ["files", "servers.cfgCapFiles"],
  ["upgrade", "servers.cfgCapUpgrade"],
  ["nat", "servers.cfgCapNAT"],
];

const FILTER_ITEMS: Array<["interface_include" | "interface_exclude" | "mount_include" | "mount_exclude", TKey]> = [
  ["interface_include", "servers.cfgInterfaceInclude"],
  ["interface_exclude", "servers.cfgInterfaceExclude"],
  ["mount_include", "servers.cfgMountInclude"],
  ["mount_exclude", "servers.cfgMountExclude"],
];

const BATCH_STATUS_KEYS: Record<BatchServerResult["status"], TKey> = {
  ok: "servers.batchStatusOk",
  offline: "servers.batchStatusOffline",
  not_found: "servers.batchStatusNotFound",
  no_ip: "servers.batchStatusNoIp",
  error: "servers.batchStatusError",
};

// 批量操作逐机结果列表（批量配置 / 批量 DDNS 共用）。
function BatchResultList({ results }: { results: BatchServerResult[] }) {
  const { t } = useI18n();
  const ok = results.filter((r) => r.status === "ok").length;
  const failed = results.length - ok;
  return (
    <div className="mt-3 rounded-lg border border-border bg-bg p-3">
      <p className="mb-2 text-sm font-medium">{t("servers.batchResultsSummary", { ok, failed })}</p>
      <ul className="max-h-52 space-y-1 overflow-auto text-xs">
        {results.map((r) => (
          <li key={r.server_id} className="flex items-start justify-between gap-2">
            <span className="font-medium">{r.server_name || `#${r.server_id}`}</span>
            <span className={`text-right ${r.status === "ok" ? "text-ok" : r.status === "error" || r.status === "not_found" ? "text-err" : "text-muted"}`}>
              {t(BATCH_STATUS_KEYS[r.status])}
              {r.error ? ` · ${r.error}` : ""}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

// 批量配置表单（不含密钥字段：批量下发不接受密钥）。
interface BatchConfigForm {
  server_url: string;
  interval: number;
  capabilities: CapabilitiesConfig;
  interface_include: string;
  interface_exclude: string;
  mount_include: string;
  mount_exclude: string;
}

const emptyBatchConfig: BatchConfigForm = {
  server_url: "",
  interval: 2,
  capabilities: { ...DEFAULT_CAPABILITIES },
  interface_include: "",
  interface_exclude: "",
  mount_include: "",
  mount_exclude: "",
};

// 表单字段：带标签与单位/示例说明，避免只依赖 placeholder。
function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <label className="flex flex-col gap-1">
      <span className="text-xs font-medium text-muted">{label}</span>
      {children}
      {hint && <span className="text-[11px] text-muted/70">{hint}</span>}
    </label>
  );
}

const inputCls = "rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent";

interface FormState {
  id?: number;
  name: string;
  group: string;
  note: string;
  price: number;
  cycle_days: number;
  expire_at: string;
  auto_renew: boolean;
  tags: string;
  sort_order: number;
  hidden: boolean;
  slo_target: number;
  traffic_quota_bytes: number;
  traffic_cycle_day: number;
  traffic_timezone: string;
  traffic_accounting: "sum" | "in" | "out" | "max";
}

const emptyForm: FormState = {
  name: "", group: "", note: "", price: 0, cycle_days: 0, expire_at: "", auto_renew: false, tags: "", sort_order: 0, hidden: false,
  slo_target: 99.9,
  traffic_quota_bytes: 0, traffic_cycle_day: 1, traffic_timezone: "UTC", traffic_accounting: "sum",
};

export default function Servers() {
  const { t, tErr, fmtDate, fmtDateTime } = useI18n();
  const qc = useQueryClient();
  // 管理页使用 REST 列表（含离线服务器；WS 快照只含在线上报）
  const { data: serverData } = useQuery({ queryKey: ["servers-list"], queryFn: api.servers, refetchInterval: 15000 });
  const servers = serverData?.servers ?? [];
  const [form, setForm] = useState<FormState | null>(null);
  const [error, setError] = useState("");
  const [execResult, setExecResult] = useState<string>("");
  const [execTarget, setExecTarget] = useState<Server | null>(null);
  const [execCmd, setExecCmd] = useState("");
  const [exec2FA, setExec2FA] = useState("");
  const [exec2FAPrompt, setExec2FAPrompt] = useState(false);
  const [installTarget, setInstallTarget] = useState<Server | null>(null);
  const [installCmd, setInstallCmd] = useState("");
  const [installCopied, setInstallCopied] = useState(false);
  const [cfg, setCfg] = useState({
    server_url: "",
    interval: 2,
    secret: "",
    capabilities: { ...DEFAULT_CAPABILITIES },
    interface_include: "",
    interface_exclude: "",
    mount_include: "",
    mount_exclude: "",
  });
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [showGroups, setShowGroups] = useState(false);
  const [newGroup, setNewGroup] = useState("");
  const [cfgTarget, setCfgTarget] = useState<Server | null>(null);
  const [batchCfgOpen, setBatchCfgOpen] = useState(false);
  const [batchCfg, setBatchCfg] = useState<BatchConfigForm>({ ...emptyBatchConfig, capabilities: { ...emptyBatchConfig.capabilities } });
  const [batchDDNSOpen, setBatchDDNSOpen] = useState(false);
  const [batchDDNSProfile, setBatchDDNSProfile] = useState(0);
  const [batchResults, setBatchResults] = useState<BatchServerResult[] | null>(null);

  const { data: groupsData } = useQuery({ queryKey: ["groups"], queryFn: api.groups, enabled: showGroups });
  const groups = groupsData?.groups ?? [];
  const { data: ddnsData } = useQuery({ queryKey: ["ddns"], queryFn: api.ddns, enabled: batchDDNSOpen });
  const ddnsProfiles = ddnsData?.profiles ?? [];

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ["servers"] });
    qc.invalidateQueries({ queryKey: ["groups"] });
  };

  const save = useMutation({
    mutationFn: async (f: FormState): Promise<{ secret?: string; server?: Server }> => {
      if (f.id) {
        await api.updateServer(f.id, f);
        return {};
      }
      return api.createServer(f);
    },
    onSuccess: (res) => {
      // 哪吒风格：创建服务器后立即弹出该服务器的一键安装命令
      if (!form?.id && res.server) {
        const srv = res.server;
        setInstallTarget(srv);
        setInstallCmd("");
        setInstallCopied(false);
        api.serverInstallCommand(srv.id).then((r) => setInstallCmd(r.command)).catch(() => {});
      }
      setForm(null);
      invalidate();
    },
    onError: (e) => setError((e as Error).message),
  });

  const remove = useMutation({ mutationFn: api.deleteServer, onSuccess: invalidate });
  const batchDelete = useMutation({
    mutationFn: api.batchDeleteServers,
    onSuccess: (r) => {
      setSelected(new Set());
      setError(t("servers.deletedOf", { count: r.deleted }));
      invalidate();
    },
    onError: (e) => setError((e as Error).message),
  });
  const batchMove = useMutation({
    mutationFn: (group: string) => api.batchMoveServers(Array.from(selected), group),
    onSuccess: (r) => {
      setSelected(new Set());
      setError(t("servers.movedOf", { count: r.moved }));
      invalidate();
    },
    onError: (e) => setError((e as Error).message),
  });
  const createGroup = useMutation({
    mutationFn: (name: string) => api.createGroup(name),
    onSuccess: () => {
      setNewGroup("");
      invalidate();
    },
  });
  const deleteGroup = useMutation({ mutationFn: api.deleteGroup, onSuccess: invalidate });
  const applyCfg = useMutation({
    mutationFn: () => {
      const c = cfg;
      return api.applyServerConfig(cfgTarget!.id, {
        server_url: c.server_url || undefined,
        interval: c.interval,
        secret: c.secret || undefined,
        capabilities: c.capabilities,
        interface_include: parseCommaList(c.interface_include),
        interface_exclude: parseCommaList(c.interface_exclude),
        mount_include: parseCommaList(c.mount_include),
        mount_exclude: parseCommaList(c.mount_exclude),
      });
    },
    onSuccess: () => {
      setCfgTarget(null);
      setError(t("servers.cfgApplied"));
    },
    onError: (e) => setError(t("servers.cfgFailed", { error: tErr(e) })),
  });
  const runExec = useMutation({
    mutationFn: () => api.exec(execTarget!.id, execCmd, 30, exec2FA),
    onSuccess: (r) => setExecResult(`exit=${r.code}\n${r.output || r.error || ""}`),
    onError: (e) => {
      // 2FA 敏感操作未提供验证码：提示输入 TOTP 码后重试
      if (e instanceof ApiError && (e.status === 428 || e.code === "auth.2fa_required")) {
        setExec2FAPrompt(true);
        setExecResult(t("servers.exec2FARequired"));
        return;
      }
      setExecResult((e as Error).message);
    },
  });
  const runBatchCfg = useMutation({
    mutationFn: (f: BatchConfigForm) =>
      api.batchConfigServers(Array.from(selected), {
        server_url: f.server_url || undefined,
        interval: f.interval,
        capabilities: f.capabilities,
        interface_include: parseCommaList(f.interface_include),
        interface_exclude: parseCommaList(f.interface_exclude),
        mount_include: parseCommaList(f.mount_include),
        mount_exclude: parseCommaList(f.mount_exclude),
      }),
    onSuccess: (r) => setBatchResults(r.results),
    onError: (e) => setError(t("servers.batchCfgFailed", { error: tErr(e) })),
  });
  const runBatchDDNS = useMutation({
    mutationFn: (profileId: number) => api.batchDDNSServers(Array.from(selected), profileId),
    onSuccess: (r) => setBatchResults(r.results),
    onError: (e) => setError(t("servers.batchDDNSFailed", { error: tErr(e) })),
  });

  const filtered = useMemo(
    () => servers.filter((s) => !query || s.name.toLowerCase().includes(query.toLowerCase()) || (s.tags || "").toLowerCase().includes(query.toLowerCase())),
    [servers, query],
  );

  const toggle = (id: number) => {
    const next = new Set(selected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    setSelected(next);
  };

  return (
    <div>
      <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">{t("servers.title")}</h1>
          <p className="text-sm text-muted">{t("servers.subtitle", { total: servers.length, selected: selected.size })}</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <div className="relative">
            <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t("servers.search")}
              className="w-44 rounded-lg border border-border bg-panel py-2 pl-9 pr-3 text-sm outline-none"
            />
          </div>
          <button onClick={() => setShowGroups(true)} className="flex items-center gap-2 rounded-lg border border-border px-3 py-2 text-sm">
            <Layers className="h-4 w-4" /> {t("servers.groupManage")}
          </button>
          <button
            onClick={() => {
              setForm(emptyForm);
            }}
            className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-white"
          >
            <Plus className="h-4 w-4" /> {t("servers.addServer")}
          </button>
        </div>
      </div>

      {selected.size > 0 && (
        <div className="mb-4 flex flex-wrap items-center gap-2 rounded-xl border border-accent/30 bg-accent/5 p-3">
          <span className="text-sm">{t("servers.selectedOf", { count: selected.size })}</span>
          <select
            value=""
            onChange={(e) => e.target.value && batchMove.mutate(e.target.value)}
            className="rounded-lg border border-border bg-bg px-3 py-1.5 text-sm"
          >
            <option value="">{t("servers.moveToGroup")}</option>
            {groups.map((g) => (
              <option key={g.id} value={g.name}>{g.name}</option>
            ))}
          </select>
          <button
            onClick={() => {
              setBatchResults(null);
              setBatchCfg({ ...emptyBatchConfig, capabilities: { ...emptyBatchConfig.capabilities } });
              setBatchCfgOpen(true);
            }}
            className="flex items-center gap-1 rounded-lg border border-border px-3 py-1.5 text-sm hover:bg-black/5 dark:hover:bg-white/5"
          >
            <Settings2 className="h-4 w-4" /> {t("servers.batchConfig")}
          </button>
          <button
            onClick={() => {
              setBatchResults(null);
              setBatchDDNSProfile(0);
              setBatchDDNSOpen(true);
            }}
            className="flex items-center gap-1 rounded-lg border border-border px-3 py-1.5 text-sm hover:bg-black/5 dark:hover:bg-white/5"
          >
            <Globe className="h-4 w-4" /> {t("servers.batchDDNS")}
          </button>
          <button
            onClick={() => confirm(t("servers.confirmBatchDelete", { count: selected.size })) && batchDelete.mutate(Array.from(selected))}
            className="rounded-lg border border-err/40 px-3 py-1.5 text-sm text-err hover:bg-err/10"
          >
            {t("servers.batchDelete")}
          </button>
          <button onClick={() => setSelected(new Set())} className="rounded-lg px-3 py-1.5 text-sm text-muted">{t("servers.clearSelection")}</button>
        </div>
      )}

      {error && <p className="mb-3 text-sm text-ok">{error}</p>}

      {form && (
        <div className="mb-5 rounded-xl border border-border bg-panel p-4">
          <h2 className="mb-3 text-sm font-medium">{form.id ? t("servers.editServer") : t("servers.addServer")}</h2>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <Field label={t("servers.name")}>
              <input className={inputCls} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
            </Field>
            <Field label={t("servers.group")}>
              <input className={inputCls} value={form.group} onChange={(e) => setForm({ ...form, group: e.target.value })} />
            </Field>
            <Field label={t("servers.note")}>
              <input className={inputCls} value={form.note} onChange={(e) => setForm({ ...form, note: e.target.value })} />
            </Field>

            {/* 创建时只填基础信息，资产字段在部署上线后通过「编辑」补录 */}
            {form.id && (
              <>
                <Field label={t("servers.price")}>
                  <input type="number" min={0} className={inputCls} value={form.price} onChange={(e) => setForm({ ...form, price: Number(e.target.value) })} />
                </Field>
                <Field label={t("servers.cycleDays")}>
                  <input type="number" min={0} className={inputCls} value={form.cycle_days} onChange={(e) => setForm({ ...form, cycle_days: Number(e.target.value) })} />
                </Field>
                <Field label={t("servers.expireLabel")}>
                  <input type="datetime-local" className={inputCls} value={form.expire_at} onChange={(e) => setForm({ ...form, expire_at: e.target.value })} />
                </Field>

                <Field label={t("servers.trafficQuota")} hint={t("servers.trafficQuotaHint")}>
                  <input type="number" min={0} className={inputCls} value={form.traffic_quota_bytes} onChange={(e) => setForm({ ...form, traffic_quota_bytes: Number(e.target.value) })} />
                </Field>
                <Field label={t("servers.trafficCycleDay")}>
                  <input type="number" min={1} max={28} className={inputCls} value={form.traffic_cycle_day} onChange={(e) => setForm({ ...form, traffic_cycle_day: Number(e.target.value) })} />
                </Field>
                <Field label={t("servers.trafficTimezone")}>
                  <input className={inputCls} value={form.traffic_timezone} onChange={(e) => setForm({ ...form, traffic_timezone: e.target.value })} />
                </Field>

                <Field label={t("servers.trafficAccounting")}>
                  <select value={form.traffic_accounting} onChange={(e) => setForm({ ...form, traffic_accounting: e.target.value as FormState["traffic_accounting"] })} className={inputCls}>
                    <option value="sum">{t("servers.accountingSum")}</option><option value="in">{t("servers.accountingIn")}</option><option value="out">{t("servers.accountingOut")}</option><option value="max">{t("servers.accountingMax")}</option>
                  </select>
                </Field>
                <Field label={t("servers.sortOrder")}>
                  <input type="number" className={inputCls} value={form.sort_order} onChange={(e) => setForm({ ...form, sort_order: Number(e.target.value) })} />
                </Field>
                <Field label={t("servers.sloTarget")} hint={t("servers.sloTargetHint")}>
                  <input type="number" step="0.1" min={0} className={inputCls} value={form.slo_target} onChange={(e) => setForm({ ...form, slo_target: Number(e.target.value) })} />
                </Field>

                <Field label={t("servers.tags")}>
                  <input className={inputCls} value={form.tags} onChange={(e) => setForm({ ...form, tags: e.target.value })} />
                </Field>
                <label className="flex items-center gap-2 text-sm">
                  <input type="checkbox" checked={form.auto_renew} onChange={(e) => setForm({ ...form, auto_renew: e.target.checked })} /> {t("servers.autoRenew")}
                </label>
                <label className="flex items-center gap-2 text-sm">
                  <input type="checkbox" checked={form.hidden} onChange={(e) => setForm({ ...form, hidden: e.target.checked })} /> {t("servers.hiddenFromGuests")}
                </label>
              </>
            )}
          </div>
          {!form.id && <p className="mt-3 text-xs text-muted">{t("servers.createHint")}</p>}
          <div className="mt-3 flex gap-2">
            <button onClick={() => save.mutate(form)} className="rounded-lg bg-accent px-4 py-1.5 text-sm text-white">{t("common.save")}</button>
            <button onClick={() => setForm(null)} className="rounded-lg border border-border px-4 py-1.5 text-sm text-muted">{t("common.cancel")}</button>
          </div>
        </div>
      )}

      <div className="overflow-x-auto rounded-xl border border-border bg-panel">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted">
              <th className="px-3 py-3"><button onClick={() => setSelected(selected.size === filtered.length ? new Set() : new Set(filtered.map((s) => s.id)))}>{selected.size === filtered.length && filtered.length > 0 ? <CheckSquare className="h-4 w-4" /> : <Square className="h-4 w-4" />}</button></th>
              <th className="px-4 py-3 font-normal">{t("servers.id")}</th>
              <th className="px-4 py-3 font-normal">{t("servers.name")}</th>
              <th className="px-4 py-3 font-normal">{t("servers.group")}</th>
              <th className="px-4 py-3 font-normal">{t("servers.system")}</th>
              <th className="px-4 py-3 font-normal">{t("servers.status")}</th>
              <th className="px-4 py-3 font-normal">{t("servers.trafficCycle")}</th>
              <th className="px-4 py-3 font-normal">{t("servers.expires")}</th>
              <th className="px-4 py-3 text-right font-normal">{t("servers.actions")}</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((s) => (
              <tr key={s.id} className="border-b border-border last:border-0 hover:bg-black/2 dark:hover:bg-white/2">
                <td className="px-3 py-3">
                  <button onClick={() => toggle(s.id)}>{selected.has(s.id) ? <CheckSquare className="h-4 w-4 text-accent" /> : <Square className="h-4 w-4 text-muted" />}</button>
                </td>
                <td className="px-4 py-3 tabular text-muted">#{s.id}</td>
                <td className="px-4 py-3 font-medium">
                  {s.name}
                  {s.hidden && <span className="ml-2 rounded-full bg-muted/20 px-2 py-0.5 text-xs text-muted">{t("common.hidden")}</span>}
                  {s.tags && (
                    <span className="ml-2 flex flex-wrap gap-1">
                      {s.tags.split(",").filter(Boolean).map((tag) => (
                        <span key={tag} className="rounded-full bg-accent/10 px-2 py-0.5 text-xs text-accent">{tag.trim()}</span>
                      ))}
                    </span>
                  )}
                </td>
                <td className="px-4 py-3">{s.group || "—"}</td>
                <td className="px-4 py-3 text-muted">{s.host?.platform || "—"}</td>
                <td className="px-4 py-3">
                  <span className={`rounded-full px-2 py-0.5 text-xs ${s.online ? "bg-ok/15 text-ok" : "bg-err/15 text-err"}`}>
                    {s.online ? t("common.online") : t("common.offline")}
                  </span>
                </td>
                <td className="px-4 py-3 text-xs text-muted">
                  {s.traffic_usage ? <div className="space-y-1">
                    <div>{fmtDate(s.traffic_usage.cycle_start)} — {fmtDate(s.traffic_usage.cycle_end)}</div>
                    <div>↓ {fmtBytes(s.traffic_usage.in_bytes)} · ↑ {fmtBytes(s.traffic_usage.out_bytes)} · {t("serverDetail.accounted", { bytes: fmtBytes(s.traffic_usage.accounted_bytes) })}</div>
                    {s.traffic_usage.quota_bytes > 0 && <div>{t("serverDetail.remaining", { bytes: fmtBytes(s.traffic_usage.remaining_bytes) })} · {s.traffic_usage.percentage?.toFixed(1)}%</div>}
                  </div> : "—"}
                </td>
                <td className="px-4 py-3 text-xs text-muted">{s.expire_at ? fmtDateTime(s.expire_at) : "—"}</td>
                <td className="px-4 py-3">
                  <div className="flex justify-end gap-1">
                    <Link
                      to={`/admin/terminal/${s.id}`}
                      title={s.online ? t("servers.terminalTitle") : t("serverDetail.terminalOffline")}
                      className={`rounded p-1.5 ${s.online ? "hover:bg-black/5 dark:hover:bg-white/5" : "pointer-events-none opacity-40"}`}
                    >
                      <TerminalSquare className="h-4 w-4" />
                    </Link>
                    <button
                      onClick={() => {
                        setCfgTarget(s);
                        setCfg({
                          server_url: "",
                          interval: 2,
                          secret: "",
                          capabilities: { ...DEFAULT_CAPABILITIES },
                          interface_include: "",
                          interface_exclude: "",
                          mount_include: "",
                          mount_exclude: "",
                        });
                      }}
                      title={t("servers.configTitle")}
                      className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5"
                    >
                      <Send className="h-4 w-4" />
                    </button>
                    <button
                      onClick={() => {
                        setInstallTarget(s);
                        setInstallCmd("");
                        setInstallCopied(false);
                        api.serverInstallCommand(s.id).then((r) => setInstallCmd(r.command)).catch(() => setInstallCmd(""));
                      }}
                      title={t("servers.installTitle")}
                      className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5"
                    >
                      <Download className="h-4 w-4" />
                    </button>
                    <button
                      onClick={() => {
                        setExecTarget(s);
                        setExecCmd("uptime");
                        setExecResult("");
                      }}
                      title={t("servers.execTitle")}
                      className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5"
                    >
                      <KeyRound className="h-4 w-4" />
                    </button>
                    <button
                      onClick={() =>
                        setForm({
                          id: s.id, name: s.name, group: s.group, note: s.note, price: s.price, cycle_days: s.cycle_days,
                          expire_at: s.expire_at ? s.expire_at.slice(0, 16) : "", auto_renew: s.auto_renew, tags: s.tags,
                          sort_order: s.sort_order, hidden: s.hidden, slo_target: s.slo_target || 99.9,
                          traffic_quota_bytes: s.traffic_quota_bytes, traffic_cycle_day: s.traffic_cycle_day || 1,
                          traffic_timezone: s.traffic_timezone || "UTC", traffic_accounting: s.traffic_accounting || "sum",
                        })
                      }
                      title={t("common.edit")}
                      className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5"
                    >
                      <Pencil className="h-4 w-4" />
                    </button>
                    <button
                      onClick={() => {
                        if (confirm(t("servers.confirmDelete", { name: s.name }))) remove.mutate(s.id);
                      }}
                      title={t("common.delete")}
                      className="rounded p-1.5 text-err hover:bg-err/10"
                    >
                      <Trash2 className="h-4 w-4" />
                    </button>
                  </div>
                </td>
              </tr>
            ))}
            {filtered.length === 0 && (
              <tr><td colSpan={9} className="px-4 py-8 text-center text-muted">{t("servers.noServers")}</td></tr>
            )}
          </tbody>
        </table>
      </div>

      {/* 远程执行 */}
      {execTarget && (
        <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/40 p-4" onClick={() => setExecTarget(null)}>
          <div className="w-full max-w-lg rounded-xl border border-border bg-panel p-4" onClick={(e) => e.stopPropagation()}>
            <h3 className="mb-3 text-sm font-medium">{t("servers.execHeading", { name: execTarget.name })}</h3>
            <input value={execCmd} onChange={(e) => setExecCmd(e.target.value)} className="w-full rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            {exec2FAPrompt && (
              <div className="mt-2 flex items-center gap-2">
                <input
                  value={exec2FA}
                  onChange={(e) => setExec2FA(e.target.value)}
                  placeholder={t("servers.twoFACodePlaceholder")}
                  className="w-40 rounded-lg border border-border bg-bg px-3 py-2 text-sm"
                />
                <span className="text-xs text-muted">{t("servers.twoFACodeHint")}</span>
              </div>
            )}
            <button onClick={() => runExec.mutate()} className="mt-2 rounded-lg bg-accent px-4 py-1.5 text-sm text-white">{t("servers.exec")}</button>
            {execResult && <pre className="mt-3 max-h-60 overflow-auto whitespace-pre-wrap rounded-lg bg-bg p-3 text-xs">{execResult}</pre>}
          </div>
        </div>
      )}

      {/* 一键安装命令 */}
      {installTarget && (
        <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/40 p-4" onClick={() => setInstallTarget(null)}>
          <div className="w-full max-w-2xl rounded-xl border border-border bg-panel p-4" onClick={(e) => e.stopPropagation()}>
            <h3 className="mb-3 text-sm font-medium">{t("servers.installHeading", { name: installTarget.name })}</h3>
            <p className="mb-2 text-xs text-muted">{t("servers.installHelp")}</p>
            <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-all rounded-lg bg-bg p-3 text-xs">{installCmd || t("common.loading")}</pre>
            <div className="mt-3 flex gap-2">
              <button
                onClick={() => {
                  navigator.clipboard.writeText(installCmd).then(() => {
                    setInstallCopied(true);
                    setTimeout(() => setInstallCopied(false), 1500);
                  });
                }}
                disabled={!installCmd}
                className="flex items-center gap-1 rounded-lg bg-accent px-4 py-1.5 text-sm text-white disabled:opacity-40"
              >
                {installCopied ? <CheckSquare className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
                {installCopied ? t("install.copied") : t("install.copy")}
              </button>
              <button onClick={() => setInstallTarget(null)} className="rounded-lg border border-border px-4 py-1.5 text-sm">{t("common.close")}</button>
            </div>
          </div>
        </div>
      )}

      {/* 配置下发 */}
      {cfgTarget && (
        <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/40 p-4" onClick={() => setCfgTarget(null)}>
          <div className="max-h-[90vh] w-full max-w-xl overflow-y-auto rounded-xl border border-border bg-panel p-4" onClick={(e) => e.stopPropagation()}>
            <h3 className="mb-3 text-sm font-medium">{t("servers.cfgHeading", { name: cfgTarget.name })}</h3>
            <div className="grid grid-cols-1 gap-3">
              <input placeholder={t("servers.cfgServerUrl")} value={cfg.server_url} onChange={(e) => setCfg({ ...cfg, server_url: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
              <input type="number" placeholder={t("servers.cfgInterval")} value={cfg.interval} onChange={(e) => setCfg({ ...cfg, interval: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
              <input placeholder={t("servers.cfgSecret")} value={cfg.secret} onChange={(e) => setCfg({ ...cfg, secret: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />

              {/* 能力开关 */}
              <div className="rounded-lg border border-border bg-bg p-3">
                <p className="mb-2 text-xs font-medium text-muted">{t("servers.cfgCapabilities")}</p>
                <div className="grid grid-cols-2 gap-x-4 gap-y-1.5">
                  {CAPABILITY_ITEMS.map(([key, labelKey]) => (
                    <label key={key} className="flex items-center gap-2 text-sm">
                      <input
                        type="checkbox"
                        checked={cfg.capabilities[key]}
                        onChange={(e) => setCfg({ ...cfg, capabilities: { ...cfg.capabilities, [key]: e.target.checked } })}
                      />
                      {t(labelKey)}
                    </label>
                  ))}
                </div>
              </div>

              {/* 网卡 / 挂载过滤 */}
              <div className="rounded-lg border border-border bg-bg p-3">
                <p className="mb-1 text-xs font-medium text-muted">{t("servers.cfgFilters")}</p>
                <p className="mb-2 text-[11px] text-muted/70">{t("servers.cfgFiltersHint")}</p>
                <div className="grid grid-cols-1 gap-2">
                  {FILTER_ITEMS.map(([key, labelKey]) => (
                    <Field key={key} label={t(labelKey)}>
                      <input
                        value={cfg[key]}
                        placeholder="eth*, /data/*"
                        onChange={(e) => setCfg({ ...cfg, [key]: e.target.value })}
                        className={inputCls}
                      />
                    </Field>
                  ))}
                </div>
              </div>
            </div>
            <button onClick={() => applyCfg.mutate()} disabled={!cfgTarget.online} className="mt-3 rounded-lg bg-accent px-4 py-1.5 text-sm text-white disabled:opacity-40">
              {t("servers.cfgSubmit")}
            </button>
          </div>
        </div>
      )}

      {/* 批量配置 */}
      {batchCfgOpen && (
        <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/40 p-4" onClick={() => setBatchCfgOpen(false)}>
          <div className="max-h-[90vh] w-full max-w-xl overflow-y-auto rounded-xl border border-border bg-panel p-4" onClick={(e) => e.stopPropagation()}>
            <h3 className="mb-1 text-sm font-medium">{t("servers.batchCfgHeading", { count: selected.size })}</h3>
            <p className="mb-3 text-xs text-muted">{t("servers.batchCfgHint")}</p>
            <div className="grid grid-cols-1 gap-3">
              <input placeholder={t("servers.cfgServerUrl")} value={batchCfg.server_url} onChange={(e) => setBatchCfg({ ...batchCfg, server_url: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
              <input type="number" placeholder={t("servers.cfgInterval")} value={batchCfg.interval} onChange={(e) => setBatchCfg({ ...batchCfg, interval: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />

              <div className="rounded-lg border border-border bg-bg p-3">
                <p className="mb-2 text-xs font-medium text-muted">{t("servers.cfgCapabilities")}</p>
                <div className="grid grid-cols-2 gap-x-4 gap-y-1.5">
                  {CAPABILITY_ITEMS.map(([key, labelKey]) => (
                    <label key={key} className="flex items-center gap-2 text-sm">
                      <input
                        type="checkbox"
                        checked={batchCfg.capabilities[key]}
                        onChange={(e) => setBatchCfg({ ...batchCfg, capabilities: { ...batchCfg.capabilities, [key]: e.target.checked } })}
                      />
                      {t(labelKey)}
                    </label>
                  ))}
                </div>
              </div>

              <div className="rounded-lg border border-border bg-bg p-3">
                <p className="mb-1 text-xs font-medium text-muted">{t("servers.cfgFilters")}</p>
                <p className="mb-2 text-[11px] text-muted/70">{t("servers.cfgFiltersHint")}</p>
                <div className="grid grid-cols-1 gap-2">
                  {FILTER_ITEMS.map(([key, labelKey]) => (
                    <Field key={key} label={t(labelKey)}>
                      <input
                        value={batchCfg[key]}
                        placeholder="eth*, /data/*"
                        onChange={(e) => setBatchCfg({ ...batchCfg, [key]: e.target.value })}
                        className={inputCls}
                      />
                    </Field>
                  ))}
                </div>
              </div>
            </div>
            <button
              onClick={() => runBatchCfg.mutate(batchCfg)}
              disabled={runBatchCfg.isPending}
              className="mt-3 rounded-lg bg-accent px-4 py-1.5 text-sm text-white disabled:opacity-40"
            >
              {runBatchCfg.isPending ? t("common.loading") : t("servers.batchCfgSubmit")}
            </button>
            {batchResults && <BatchResultList results={batchResults} />}
          </div>
        </div>
      )}

      {/* 批量 DDNS */}
      {batchDDNSOpen && (
        <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/40 p-4" onClick={() => setBatchDDNSOpen(false)}>
          <div className="max-h-[90vh] w-full max-w-xl overflow-y-auto rounded-xl border border-border bg-panel p-4" onClick={(e) => e.stopPropagation()}>
            <h3 className="mb-1 text-sm font-medium">{t("servers.batchDDNSHeading", { count: selected.size })}</h3>
            <p className="mb-3 text-xs text-muted">{t("servers.batchDDNSHint")}</p>
            <select
              value={batchDDNSProfile}
              onChange={(e) => setBatchDDNSProfile(Number(e.target.value))}
              className="w-full rounded-lg border border-border bg-bg px-3 py-2 text-sm"
            >
              <option value={0}>{t("servers.batchSelectProfile")}</option>
              {ddnsProfiles.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name} · {servers.find((s) => s.id === p.server_id)?.name || `#${p.server_id}`} · {p.domains}
                </option>
              ))}
            </select>
            {ddnsProfiles.length === 0 && <p className="mt-2 text-xs text-muted">{t("servers.batchNoProfile")}</p>}
            <div className="mt-3 flex gap-2">
              <button
                onClick={() => runBatchDDNS.mutate(batchDDNSProfile)}
                disabled={!batchDDNSProfile || runBatchDDNS.isPending}
                className="rounded-lg bg-accent px-4 py-1.5 text-sm text-white disabled:opacity-40"
              >
                {runBatchDDNS.isPending ? t("common.loading") : t("servers.batchDDNSSubmit")}
              </button>
              <button onClick={() => setBatchDDNSOpen(false)} className="rounded-lg border border-border px-4 py-1.5 text-sm">{t("common.close")}</button>
            </div>
            {batchResults && <BatchResultList results={batchResults} />}
          </div>
        </div>
      )}

      {/* 分组管理 */}
      {showGroups && (
        <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/40 p-4" onClick={() => setShowGroups(false)}>
          <div className="w-full max-w-md rounded-xl border border-border bg-panel p-4" onClick={(e) => e.stopPropagation()}>
            <h3 className="mb-3 text-sm font-medium">{t("servers.groupsTitle")}</h3>
            <div className="mb-3 flex gap-2">
              <input value={newGroup} onChange={(e) => setNewGroup(e.target.value)} placeholder={t("servers.newGroup")} className="flex-1 rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
              <button onClick={() => newGroup && createGroup.mutate(newGroup)} className="rounded-lg bg-accent px-4 py-2 text-sm text-white">{t("common.create")}</button>
            </div>
            <ul className="space-y-1">
              {groups.map((g) => (
                <li key={g.id} className="flex items-center justify-between rounded-lg border border-border px-3 py-2 text-sm">
                  <span>{g.name}</span>
                  <button onClick={() => confirm(t("servers.confirmDeleteGroup", { name: g.name })) && deleteGroup.mutate(g.id)} className="text-err hover:opacity-70">
                    <Trash2 className="h-4 w-4" />
                  </button>
                </li>
              ))}
              {groups.length === 0 && <li className="py-4 text-center text-sm text-muted">{t("servers.noGroups")}</li>}
            </ul>
          </div>
        </div>
      )}
    </div>
  );
}
