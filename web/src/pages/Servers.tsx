import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckSquare, Copy, Download, KeyRound, Layers, Pencil, Plus, Search, Send, Square, TerminalSquare, Trash2 } from "lucide-react";
import { api, type Server } from "../lib/api";
import { fmtBytes } from "../lib/format";
import { useI18n } from "../lib/i18n";

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
  const { t, fmtDate, fmtDateTime } = useI18n();
  const qc = useQueryClient();
  // 管理页使用 REST 列表（含离线服务器；WS 快照只含在线上报）
  const { data: serverData } = useQuery({ queryKey: ["servers-list"], queryFn: api.servers, refetchInterval: 15000 });
  const servers = serverData?.servers ?? [];
  const [form, setForm] = useState<FormState | null>(null);
  const [secret, setSecret] = useState<string | null>(null);
  const [error, setError] = useState("");
  const [execResult, setExecResult] = useState<string>("");
  const [execTarget, setExecTarget] = useState<Server | null>(null);
  const [execCmd, setExecCmd] = useState("");
  const [installTarget, setInstallTarget] = useState<Server | null>(null);
  const [installCmd, setInstallCmd] = useState("");
  const [installCopied, setInstallCopied] = useState(false);
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [showGroups, setShowGroups] = useState(false);
  const [newGroup, setNewGroup] = useState("");
  const [cfgTarget, setCfgTarget] = useState<Server | null>(null);
  const [cfg, setCfg] = useState({ server_url: "", interval: 2, secret: "" });

  const { data: groupsData } = useQuery({ queryKey: ["groups"], queryFn: api.groups, enabled: showGroups });
  const groups = groupsData?.groups ?? [];

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ["servers"] });
    qc.invalidateQueries({ queryKey: ["groups"] });
  };

  const save = useMutation({
    mutationFn: async (f: FormState): Promise<{ secret?: string }> => {
      if (f.id) {
        await api.updateServer(f.id, f);
        return {};
      }
      return api.createServer(f);
    },
    onSuccess: (res) => {
      if (!form?.id) setSecret(res.secret ?? t("servers.secretFallback"));
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
    mutationFn: () => api.applyServerConfig(cfgTarget!.id, { server_url: cfg.server_url || undefined, interval: cfg.interval, secret: cfg.secret || undefined }),
    onSuccess: () => {
      setCfgTarget(null);
      setError(t("servers.cfgApplied"));
    },
    onError: (e) => setError(t("servers.cfgFailed", { error: (e as Error).message })),
  });
  const runExec = useMutation({
    mutationFn: () => api.exec(execTarget!.id, execCmd),
    onSuccess: (r) => setExecResult(`exit=${r.code}\n${r.output || r.error || ""}`),
    onError: (e) => setExecResult((e as Error).message),
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
              setSecret(null);
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
            <input placeholder={t("servers.name")} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input placeholder={t("servers.group")} value={form.group} onChange={(e) => setForm({ ...form, group: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input placeholder={t("servers.note")} value={form.note} onChange={(e) => setForm({ ...form, note: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input type="number" placeholder={t("servers.price")} value={form.price} onChange={(e) => setForm({ ...form, price: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input type="number" placeholder={t("servers.cycleDays")} value={form.cycle_days} onChange={(e) => setForm({ ...form, cycle_days: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input type="number" placeholder={t("servers.trafficQuota")} min={0} value={form.traffic_quota_bytes} onChange={(e) => setForm({ ...form, traffic_quota_bytes: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input type="number" placeholder={t("servers.trafficCycleDay")} min={1} max={28} value={form.traffic_cycle_day} onChange={(e) => setForm({ ...form, traffic_cycle_day: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input placeholder={t("servers.trafficTimezone")} value={form.traffic_timezone} onChange={(e) => setForm({ ...form, traffic_timezone: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <select value={form.traffic_accounting} onChange={(e) => setForm({ ...form, traffic_accounting: e.target.value as FormState["traffic_accounting"] })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm">
              <option value="sum">{t("servers.accountingSum")}</option><option value="in">{t("servers.accountingIn")}</option><option value="out">{t("servers.accountingOut")}</option><option value="max">{t("servers.accountingMax")}</option>
            </select>
            <input type="datetime-local" value={form.expire_at} onChange={(e) => setForm({ ...form, expire_at: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input type="number" placeholder={t("servers.sortOrder")} value={form.sort_order} onChange={(e) => setForm({ ...form, sort_order: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input type="number" step="0.1" min={0} placeholder={t("servers.sloTarget")} value={form.slo_target} onChange={(e) => setForm({ ...form, slo_target: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input placeholder={t("servers.tags")} value={form.tags} onChange={(e) => setForm({ ...form, tags: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={form.auto_renew} onChange={(e) => setForm({ ...form, auto_renew: e.target.checked })} /> {t("servers.autoRenew")}
            </label>
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={form.hidden} onChange={(e) => setForm({ ...form, hidden: e.target.checked })} /> {t("servers.hiddenFromGuests")}
            </label>
          </div>
          {secret && (
            <div className="mt-3 flex items-center gap-2 rounded-lg bg-ok/10 p-3 text-sm">
              <KeyRound className="h-4 w-4 text-ok" />
              <span className="flex-1 break-all">{secret}</span>
              <button onClick={() => navigator.clipboard?.writeText(secret)} className="flex items-center gap-1 text-muted hover:text-fg">
                <Copy className="h-3.5 w-3.5" /> {t("common.copy")}
              </button>
            </div>
          )}
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
                        setCfg({ server_url: "", interval: 2, secret: "" });
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
          <div className="w-full max-w-lg rounded-xl border border-border bg-panel p-4" onClick={(e) => e.stopPropagation()}>
            <h3 className="mb-3 text-sm font-medium">{t("servers.cfgHeading", { name: cfgTarget.name })}</h3>
            <div className="grid grid-cols-1 gap-3">
              <input placeholder={t("servers.cfgServerUrl")} value={cfg.server_url} onChange={(e) => setCfg({ ...cfg, server_url: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
              <input type="number" placeholder={t("servers.cfgInterval")} value={cfg.interval} onChange={(e) => setCfg({ ...cfg, interval: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
              <input placeholder={t("servers.cfgSecret")} value={cfg.secret} onChange={(e) => setCfg({ ...cfg, secret: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            </div>
            <button onClick={() => applyCfg.mutate()} disabled={!cfgTarget.online} className="mt-3 rounded-lg bg-accent px-4 py-1.5 text-sm text-white disabled:opacity-40">
              {t("servers.cfgSubmit")}
            </button>
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
