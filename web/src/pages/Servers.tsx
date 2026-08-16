import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckSquare, Copy, KeyRound, Layers, Pencil, Plus, Search, Send, Square, TerminalSquare, Trash2 } from "lucide-react";
import { api, type Server } from "../lib/api";
import { fmtBytes, fmtDateTime } from "../lib/format";

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
  traffic_quota_bytes: number;
  traffic_cycle_day: number;
  traffic_timezone: string;
  traffic_accounting: "sum" | "in" | "out" | "max";
}

const emptyForm: FormState = {
  name: "", group: "", note: "", price: 0, cycle_days: 0, expire_at: "", auto_renew: false, tags: "", sort_order: 0, hidden: false,
  traffic_quota_bytes: 0, traffic_cycle_day: 1, traffic_timezone: "UTC", traffic_accounting: "sum",
};

export default function Servers() {
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
      if (!form?.id) setSecret(res.secret ?? "(未返回密钥)");
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
      setError(`已删除 ${r.deleted} 台`);
      invalidate();
    },
    onError: (e) => setError((e as Error).message),
  });
  const batchMove = useMutation({
    mutationFn: (group: string) => api.batchMoveServers(Array.from(selected), group),
    onSuccess: (r) => {
      setSelected(new Set());
      setError(`已移动 ${r.moved} 台`);
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
      setError("配置已下发");
    },
    onError: (e) => setError("配置下发失败: " + (e as Error).message),
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
          <h1 className="text-xl font-semibold">服务器管理</h1>
          <p className="text-sm text-muted">共 {servers.length} 台 · 已选 {selected.size} 台</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <div className="relative">
            <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="搜索名称/标签…"
              className="w-44 rounded-lg border border-border bg-panel py-2 pl-9 pr-3 text-sm outline-none"
            />
          </div>
          <button onClick={() => setShowGroups(true)} className="flex items-center gap-2 rounded-lg border border-border px-3 py-2 text-sm">
            <Layers className="h-4 w-4" /> 分组管理
          </button>
          <button
            onClick={() => {
              setForm(emptyForm);
              setSecret(null);
            }}
            className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-white"
          >
            <Plus className="h-4 w-4" /> 添加服务器
          </button>
        </div>
      </div>

      {selected.size > 0 && (
        <div className="mb-4 flex flex-wrap items-center gap-2 rounded-xl border border-accent/30 bg-accent/5 p-3">
          <span className="text-sm">已选 {selected.size} 台：</span>
          <select
            value=""
            onChange={(e) => e.target.value && batchMove.mutate(e.target.value)}
            className="rounded-lg border border-border bg-bg px-3 py-1.5 text-sm"
          >
            <option value="">移动到分组…</option>
            {groups.map((g) => (
              <option key={g.id} value={g.name}>{g.name}</option>
            ))}
          </select>
          <button
            onClick={() => confirm(`确认删除选中的 ${selected.size} 台服务器？`) && batchDelete.mutate(Array.from(selected))}
            className="rounded-lg border border-err/40 px-3 py-1.5 text-sm text-err hover:bg-err/10"
          >
            批量删除
          </button>
          <button onClick={() => setSelected(new Set())} className="rounded-lg px-3 py-1.5 text-sm text-muted">取消选择</button>
        </div>
      )}

      {error && <p className="mb-3 text-sm text-ok">{error}</p>}

      {form && (
        <div className="mb-5 rounded-xl border border-border bg-panel p-4">
          <h2 className="mb-3 text-sm font-medium">{form.id ? "编辑服务器" : "添加服务器"}</h2>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <input placeholder="名称" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input placeholder="分组" value={form.group} onChange={(e) => setForm({ ...form, group: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input placeholder="备注" value={form.note} onChange={(e) => setForm({ ...form, note: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input type="number" placeholder="价格" value={form.price} onChange={(e) => setForm({ ...form, price: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input type="number" placeholder="计费周期（天）" value={form.cycle_days} onChange={(e) => setForm({ ...form, cycle_days: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input type="number" placeholder="流量额度（bytes，0=无限）" min={0} value={form.traffic_quota_bytes} onChange={(e) => setForm({ ...form, traffic_quota_bytes: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input type="number" placeholder="流量周期日（1-28）" min={1} max={28} value={form.traffic_cycle_day} onChange={(e) => setForm({ ...form, traffic_cycle_day: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input placeholder="流量时区（如 Asia/Shanghai）" value={form.traffic_timezone} onChange={(e) => setForm({ ...form, traffic_timezone: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <select value={form.traffic_accounting} onChange={(e) => setForm({ ...form, traffic_accounting: e.target.value as FormState["traffic_accounting"] })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm">
              <option value="sum">流量计费：入 + 出</option><option value="in">仅入向</option><option value="out">仅出向</option><option value="max">入/出取最大</option>
            </select>
            <input type="datetime-local" value={form.expire_at} onChange={(e) => setForm({ ...form, expire_at: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input type="number" placeholder="排序" value={form.sort_order} onChange={(e) => setForm({ ...form, sort_order: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input placeholder="标签（逗号分隔）" value={form.tags} onChange={(e) => setForm({ ...form, tags: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={form.auto_renew} onChange={(e) => setForm({ ...form, auto_renew: e.target.checked })} /> 自动续费
            </label>
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={form.hidden} onChange={(e) => setForm({ ...form, hidden: e.target.checked })} /> 对游客隐藏
            </label>
          </div>
          {secret && (
            <div className="mt-3 flex items-center gap-2 rounded-lg bg-ok/10 p-3 text-sm">
              <KeyRound className="h-4 w-4 text-ok" />
              <span className="flex-1 break-all">{secret}</span>
              <button onClick={() => navigator.clipboard?.writeText(secret)} className="flex items-center gap-1 text-muted hover:text-fg">
                <Copy className="h-3.5 w-3.5" /> 复制
              </button>
            </div>
          )}
          <div className="mt-3 flex gap-2">
            <button onClick={() => save.mutate(form)} className="rounded-lg bg-accent px-4 py-1.5 text-sm text-white">保存</button>
            <button onClick={() => setForm(null)} className="rounded-lg border border-border px-4 py-1.5 text-sm text-muted">取消</button>
          </div>
        </div>
      )}

      <div className="overflow-x-auto rounded-xl border border-border bg-panel">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted">
              <th className="px-3 py-3"><button onClick={() => setSelected(selected.size === filtered.length ? new Set() : new Set(filtered.map((s) => s.id)))}>{selected.size === filtered.length && filtered.length > 0 ? <CheckSquare className="h-4 w-4" /> : <Square className="h-4 w-4" />}</button></th>
              <th className="px-4 py-3 font-normal">ID</th>
              <th className="px-4 py-3 font-normal">名称</th>
              <th className="px-4 py-3 font-normal">分组</th>
              <th className="px-4 py-3 font-normal">系统</th>
              <th className="px-4 py-3 font-normal">状态</th>
              <th className="px-4 py-3 font-normal">流量周期 / 用量</th>
              <th className="px-4 py-3 font-normal">到期</th>
              <th className="px-4 py-3 text-right font-normal">操作</th>
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
                  {s.hidden && <span className="ml-2 rounded-full bg-muted/20 px-2 py-0.5 text-xs text-muted">隐藏</span>}
                  {s.tags && (
                    <span className="ml-2 flex flex-wrap gap-1">
                      {s.tags.split(",").filter(Boolean).map((t) => (
                        <span key={t} className="rounded-full bg-accent/10 px-2 py-0.5 text-xs text-accent">{t.trim()}</span>
                      ))}
                    </span>
                  )}
                </td>
                <td className="px-4 py-3">{s.group || "—"}</td>
                <td className="px-4 py-3 text-muted">{s.host?.platform || "—"}</td>
                <td className="px-4 py-3">
                  <span className={`rounded-full px-2 py-0.5 text-xs ${s.online ? "bg-ok/15 text-ok" : "bg-err/15 text-err"}`}>
                    {s.online ? "在线" : "离线"}
                  </span>
                </td>
                <td className="px-4 py-3 text-xs text-muted">
                  {s.traffic_usage ? <div className="space-y-1">
                    <div>{new Date(s.traffic_usage.cycle_start).toLocaleDateString("zh-CN")} — {new Date(s.traffic_usage.cycle_end).toLocaleDateString("zh-CN")}</div>
                    <div>↓ {fmtBytes(s.traffic_usage.in_bytes)} · ↑ {fmtBytes(s.traffic_usage.out_bytes)} · 计费 {fmtBytes(s.traffic_usage.accounted_bytes)}</div>
                    {s.traffic_usage.quota_bytes > 0 && <div>剩余 {fmtBytes(s.traffic_usage.remaining_bytes)} · {s.traffic_usage.percentage?.toFixed(1)}%</div>}
                  </div> : "—"}
                </td>
                <td className="px-4 py-3 text-xs text-muted">{s.expire_at ? fmtDateTime(s.expire_at) : "—"}</td>
                <td className="px-4 py-3">
                  <div className="flex justify-end gap-1">
                    <Link
                      to={`/admin/terminal/${s.id}`}
                      title={s.online ? "打开终端" : "服务器离线"}
                      className={`rounded p-1.5 ${s.online ? "hover:bg-black/5 dark:hover:bg-white/5" : "pointer-events-none opacity-40"}`}
                    >
                      <TerminalSquare className="h-4 w-4" />
                    </Link>
                    <button
                      onClick={() => {
                        setCfgTarget(s);
                        setCfg({ server_url: "", interval: 2, secret: "" });
                      }}
                      title="配置下发"
                      className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5"
                    >
                      <Send className="h-4 w-4" />
                    </button>
                    <button
                      onClick={() => {
                        setExecTarget(s);
                        setExecCmd("uptime");
                        setExecResult("");
                      }}
                      title="远程执行"
                      className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5"
                    >
                      <KeyRound className="h-4 w-4" />
                    </button>
                    <button
                      onClick={() =>
                        setForm({
                          id: s.id, name: s.name, group: s.group, note: s.note, price: s.price, cycle_days: s.cycle_days,
                          expire_at: s.expire_at ? s.expire_at.slice(0, 16) : "", auto_renew: s.auto_renew, tags: s.tags,
                          sort_order: s.sort_order, hidden: s.hidden,
                          traffic_quota_bytes: s.traffic_quota_bytes, traffic_cycle_day: s.traffic_cycle_day || 1,
                          traffic_timezone: s.traffic_timezone || "UTC", traffic_accounting: s.traffic_accounting || "sum",
                        })
                      }
                      title="编辑"
                      className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5"
                    >
                      <Pencil className="h-4 w-4" />
                    </button>
                    <button
                      onClick={() => {
                        if (confirm(`确认删除服务器「${s.name}」？`)) remove.mutate(s.id);
                      }}
                      title="删除"
                      className="rounded p-1.5 text-err hover:bg-err/10"
                    >
                      <Trash2 className="h-4 w-4" />
                    </button>
                  </div>
                </td>
              </tr>
            ))}
            {filtered.length === 0 && (
              <tr><td colSpan={9} className="px-4 py-8 text-center text-muted">暂无服务器</td></tr>
            )}
          </tbody>
        </table>
      </div>

      {/* 远程执行 */}
      {execTarget && (
        <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/40 p-4" onClick={() => setExecTarget(null)}>
          <div className="w-full max-w-lg rounded-xl border border-border bg-panel p-4" onClick={(e) => e.stopPropagation()}>
            <h3 className="mb-3 text-sm font-medium">在「{execTarget.name}」执行命令</h3>
            <input value={execCmd} onChange={(e) => setExecCmd(e.target.value)} className="w-full rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <button onClick={() => runExec.mutate()} className="mt-2 rounded-lg bg-accent px-4 py-1.5 text-sm text-white">执行</button>
            {execResult && <pre className="mt-3 max-h-60 overflow-auto whitespace-pre-wrap rounded-lg bg-bg p-3 text-xs">{execResult}</pre>}
          </div>
        </div>
      )}

      {/* 配置下发 */}
      {cfgTarget && (
        <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/40 p-4" onClick={() => setCfgTarget(null)}>
          <div className="w-full max-w-lg rounded-xl border border-border bg-panel p-4" onClick={(e) => e.stopPropagation()}>
            <h3 className="mb-3 text-sm font-medium">向「{cfgTarget.name}」下发 Agent 配置</h3>
            <div className="grid grid-cols-1 gap-3">
              <input placeholder="Server URL（留空不变）" value={cfg.server_url} onChange={(e) => setCfg({ ...cfg, server_url: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
              <input type="number" placeholder="上报间隔（秒）" value={cfg.interval} onChange={(e) => setCfg({ ...cfg, interval: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
              <input placeholder="密钥（留空不变）" value={cfg.secret} onChange={(e) => setCfg({ ...cfg, secret: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            </div>
            <button onClick={() => applyCfg.mutate()} disabled={!cfgTarget.online} className="mt-3 rounded-lg bg-accent px-4 py-1.5 text-sm text-white disabled:opacity-40">
              下发（需 Agent 重启生效）
            </button>
          </div>
        </div>
      )}

      {/* 分组管理 */}
      {showGroups && (
        <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/40 p-4" onClick={() => setShowGroups(false)}>
          <div className="w-full max-w-md rounded-xl border border-border bg-panel p-4" onClick={(e) => e.stopPropagation()}>
            <h3 className="mb-3 text-sm font-medium">分组管理</h3>
            <div className="mb-3 flex gap-2">
              <input value={newGroup} onChange={(e) => setNewGroup(e.target.value)} placeholder="新分组名称" className="flex-1 rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
              <button onClick={() => newGroup && createGroup.mutate(newGroup)} className="rounded-lg bg-accent px-4 py-2 text-sm text-white">创建</button>
            </div>
            <ul className="space-y-1">
              {groups.map((g) => (
                <li key={g.id} className="flex items-center justify-between rounded-lg border border-border px-3 py-2 text-sm">
                  <span>{g.name}</span>
                  <button onClick={() => confirm(`删除分组「${g.name}」？组内服务器移出该组` ) && deleteGroup.mutate(g.id)} className="text-err hover:opacity-70">
                    <Trash2 className="h-4 w-4" />
                  </button>
                </li>
              ))}
              {groups.length === 0 && <li className="py-4 text-center text-sm text-muted">暂无分组</li>}
            </ul>
          </div>
        </div>
      )}
    </div>
  );
}
