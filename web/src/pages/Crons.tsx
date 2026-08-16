import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronDown, ChevronRight, History, Pencil, Play, Plus, Trash2 } from "lucide-react";
import { api, type Cron, type TaskRun } from "../lib/api";
import { fmtDateTime } from "../lib/format";

const emptyCron = { name: "", expression: "*/5 * * * *", command: "", server_ids: "", enabled: true, skip_if_running: true };
const statusClass: Record<string, string> = {
  success: "bg-ok/15 text-ok", failed: "bg-err/15 text-err", partial_failure: "bg-warn/15 text-warn",
  running: "bg-accent/15 text-accent", queued: "bg-accent/15 text-accent", skipped: "bg-muted/20 text-muted",
};
const statusText: Record<string, string> = { success: "成功", failed: "失败", partial_failure: "部分失败", running: "执行中", queued: "排队中", skipped: "已跳过", offline: "离线", timeout: "超时" };
const triggerText: Record<string, string> = { scheduled: "定时", manual: "手动", alert_failure: "告警故障", alert_recovery: "告警恢复" };

function RunHistory({ cronId }: { cronId: number }) {
  const [selected, setSelected] = useState<number | null>(null);
  const { data } = useQuery({ queryKey: ["cron-runs", cronId], queryFn: () => api.cronRuns(cronId), refetchInterval: 3000 });
  const { data: detail } = useQuery({ queryKey: ["cron-run", cronId, selected], queryFn: () => api.cronRun(cronId, selected!), enabled: selected !== null, refetchInterval: (q) => ["queued", "running"].includes(q.state.data?.status ?? "") ? 2000 : false });
  const runs = data?.runs ?? [];
  return <div className="mt-3 border-t border-border pt-3">
    <div className="mb-2 text-xs font-medium text-muted">最近运行</div>
    <div className="space-y-1">
      {runs.map((r: TaskRun) => <div key={r.id}>
        <button onClick={() => setSelected(selected === r.id ? null : r.id)} className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left text-xs hover:bg-black/5 dark:hover:bg-white/5">
          {selected === r.id ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
          <span className="tabular-nums">#{r.id}</span><span>{triggerText[r.trigger] ?? r.trigger}</span>
          <span className={`rounded-full px-2 py-0.5 ${statusClass[r.status] ?? "bg-muted/20"}`}>{statusText[r.status] ?? r.status}</span>
          <span className="text-muted">{fmtDateTime(r.created_at)} · {r.duration_ms}ms · {r.target_count} 目标</span>
        </button>
        {selected === r.id && detail && <div className="ml-6 mt-1 space-y-2 rounded-lg bg-black/5 p-2 dark:bg-white/5">
          {detail.error && <div className="text-xs text-err">{detail.error}</div>}
          {(detail.results ?? []).map(x => <div key={x.id} className="rounded border border-border bg-bg p-2 text-xs">
            <div className="flex gap-2"><b>{x.server_name || `#${x.server_id}`}</b><span>{statusText[x.status] ?? x.status}</span><span className="text-muted">exit {x.exit_code} · {x.duration_ms}ms{x.truncated ? " · 输出已截断" : ""}</span></div>
            {x.stdout && <pre className="mt-1 max-h-48 overflow-auto whitespace-pre-wrap text-muted">{x.stdout}</pre>}
            {x.stderr && <pre className="mt-1 max-h-48 overflow-auto whitespace-pre-wrap text-err">{x.stderr}</pre>}
            {x.error && <div className="mt-1 text-err">{x.error}</div>}
          </div>)}
        </div>}
      </div>)}
      {runs.length === 0 && <div className="px-2 py-3 text-xs text-muted">暂无运行历史</div>}
    </div>
  </div>;
}

export default function Crons() {
  const qc = useQueryClient();
  const { data } = useQuery({ queryKey: ["crons"], queryFn: api.crons });
  const crons = data?.crons ?? [];
  const [form, setForm] = useState<(typeof emptyCron) & { id?: number } | null>(null);
  const [history, setHistory] = useState<number | null>(null);
  const invalidate = () => qc.invalidateQueries({ queryKey: ["crons"] });
  const save = useMutation({ mutationFn: (c: typeof emptyCron & { id?: number }) => api.saveCron(c), onSuccess: () => { setForm(null); invalidate(); } });
  const remove = useMutation({ mutationFn: api.deleteCron, onSuccess: invalidate });
  const run = useMutation({ mutationFn: api.runCron, onSuccess: (r, id) => { setHistory(id); qc.invalidateQueries({ queryKey: ["cron-runs", id] }); qc.invalidateQueries({ queryKey: ["cron-run", id, r.run_id] }); } });

  return <div>
    <div className="mb-5 flex items-center justify-between"><div><h1 className="text-xl font-semibold">定时任务</h1><p className="text-sm text-muted">定时、手动和告警联动执行均记录逐目标历史</p></div><button onClick={() => setForm(emptyCron)} className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-white"><Plus className="h-4 w-4" />新建任务</button></div>
    {form && <div className="mb-5 rounded-xl border border-border bg-panel p-4"><h2 className="mb-3 text-sm font-medium">{form.id ? "编辑任务" : "新建任务"}</h2><div className="grid grid-cols-1 gap-3 sm:grid-cols-4">
      <input placeholder="任务名称" value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
      <input placeholder="cron 表达式" value={form.expression} onChange={e => setForm({ ...form, expression: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
      <input placeholder="服务器 ID（逗号分隔，空=全部在线）" value={form.server_ids} onChange={e => setForm({ ...form, server_ids: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm sm:col-span-2" />
      <input placeholder="要执行的命令" value={form.command} onChange={e => setForm({ ...form, command: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm sm:col-span-2" />
      <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={form.enabled} onChange={e => setForm({ ...form, enabled: e.target.checked })} />启用</label>
      <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={form.skip_if_running} onChange={e => setForm({ ...form, skip_if_running: e.target.checked })} />运行中跳过</label>
    </div><div className="mt-3 flex gap-2"><button onClick={() => save.mutate(form)} className="rounded-lg bg-accent px-4 py-1.5 text-sm text-white">保存</button><button onClick={() => setForm(null)} className="rounded-lg border border-border px-4 py-1.5 text-sm text-muted">取消</button></div></div>}
    <div className="space-y-3">{crons.map((c: Cron) => <div key={c.id} className="rounded-xl border border-border bg-panel p-4">
      <div className="flex items-center justify-between"><div className="flex flex-wrap items-center gap-3"><span className="font-medium">{c.name}</span><span className="rounded-full bg-accent/10 px-2 py-0.5 text-xs text-accent">{c.expression}</span><span className={`rounded-full px-2 py-0.5 text-xs ${c.enabled ? "bg-ok/15 text-ok" : "bg-muted/20 text-muted"}`}>{c.enabled ? "启用" : "停用"}</span><span className="text-xs text-muted">目标: {c.server_ids || "全部在线"} · 上次: {fmtDateTime(c.last_run_at)}</span></div>
      <div className="flex gap-1"><button disabled={run.isPending} onClick={() => run.mutate(c.id)} className="flex items-center gap-1 rounded-lg px-2 py-1.5 text-sm text-ok hover:bg-ok/10"><Play className="h-4 w-4" />执行</button><button onClick={() => setHistory(history === c.id ? null : c.id)} className="flex items-center gap-1 rounded-lg px-2 py-1.5 text-sm hover:bg-black/5"><History className="h-4 w-4" />历史</button><button onClick={() => setForm({ ...emptyCron, ...c, id: c.id })} className="rounded p-1.5"><Pencil className="h-4 w-4" /></button><button onClick={() => confirm(`删除任务「${c.name}」？`) && remove.mutate(c.id)} className="rounded p-1.5 text-err"><Trash2 className="h-4 w-4" /></button></div></div>
      <div className="mt-2 rounded-lg bg-black/5 p-2 font-mono text-xs dark:bg-white/5">$ {c.command}</div>
      {c.last_result && <pre className="mt-2 max-h-24 overflow-auto whitespace-pre-wrap rounded-lg bg-black/5 p-2 text-xs text-muted dark:bg-white/5">{c.last_result}</pre>}
      {history === c.id && <RunHistory cronId={c.id} />}
    </div>)}{crons.length === 0 && <div className="rounded-xl border border-dashed border-border py-16 text-center text-sm text-muted">暂无定时任务</div>}</div>
  </div>;
}
