import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Play, Pencil, Plus, Trash2 } from "lucide-react";
import { api, type Cron } from "../lib/api";
import { fmtDateTime } from "../lib/format";

const emptyCron = { name: "", expression: "*/5 * * * *", command: "", server_ids: "", enabled: true };

export default function Crons() {
  const qc = useQueryClient();
  const { data } = useQuery({ queryKey: ["crons"], queryFn: api.crons });
  const crons = data?.crons ?? [];

  const [form, setForm] = useState<(typeof emptyCron) & { id?: number } | null>(null);
  const [result, setResult] = useState<{ id: number; text: string } | null>(null);

  const invalidate = () => qc.invalidateQueries({ queryKey: ["crons"] });

  const save = useMutation({
    mutationFn: (c: typeof emptyCron & { id?: number }) => api.saveCron(c),
    onSuccess: () => {
      setForm(null);
      invalidate();
    },
  });
  const remove = useMutation({ mutationFn: api.deleteCron, onSuccess: invalidate });
  const run = useMutation({
    mutationFn: api.runCron,
    onSuccess: (r, id) => setResult({ id, text: r.result }),
  });

  return (
    <div>
      <div className="mb-5 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">定时任务</h1>
          <p className="text-sm text-muted">cron 表达式定时向指定服务器下发命令（空 = 全部在线服务器）</p>
        </div>
        <button
          onClick={() => setForm(emptyCron)}
          className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-white hover:opacity-90"
        >
          <Plus className="h-4 w-4" />
          新建任务
        </button>
      </div>

      {form && (
        <div className="mb-5 rounded-xl border border-border bg-panel p-4">
          <h2 className="mb-3 text-sm font-medium">{form.id ? "编辑任务" : "新建任务"}</h2>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-4">
            <input
              placeholder="任务名称"
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            />
            <input
              placeholder="cron 表达式"
              value={form.expression}
              onChange={(e) => setForm({ ...form, expression: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            />
            <input
              placeholder="服务器 ID（逗号分隔，空=全部）"
              value={form.server_ids}
              onChange={(e) => setForm({ ...form, server_ids: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none sm:col-span-2"
            />
            <input
              placeholder="要执行的命令"
              value={form.command}
              onChange={(e) => setForm({ ...form, command: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none sm:col-span-3"
            />
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={form.enabled} onChange={(e) => setForm({ ...form, enabled: e.target.checked })} />
              启用
            </label>
          </div>
          <div className="mt-3 flex gap-2">
            <button onClick={() => save.mutate(form)} className="rounded-lg bg-accent px-4 py-1.5 text-sm text-white hover:opacity-90">
              保存
            </button>
            <button onClick={() => setForm(null)} className="rounded-lg border border-border px-4 py-1.5 text-sm text-muted">
              取消
            </button>
          </div>
        </div>
      )}

      <div className="space-y-3">
        {crons.map((c: Cron) => (
          <div key={c.id} className="rounded-xl border border-border bg-panel p-4">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-3">
                <span className="font-medium">{c.name}</span>
                <span className="rounded-full bg-accent/10 px-2 py-0.5 text-xs tabular text-accent">{c.expression}</span>
                <span className={`rounded-full px-2 py-0.5 text-xs ${c.enabled ? "bg-ok/15 text-ok" : "bg-muted/20 text-muted"}`}>
                  {c.enabled ? "启用" : "停用"}
                </span>
                <span className="text-xs text-muted">
                  目标: {c.server_ids || "全部"} · 上次执行: {fmtDateTime(c.last_run_at)}
                </span>
              </div>
              <div className="flex gap-1">
                <button
                  onClick={() => run.mutate(c.id)}
                  title="立即执行"
                  className="flex items-center gap-1 rounded-lg px-2 py-1.5 text-sm text-ok hover:bg-ok/10"
                >
                  <Play className="h-4 w-4" /> 执行
                </button>
                <button
                  onClick={() => setForm({ ...emptyCron, ...c, id: c.id })}
                  className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5"
                >
                  <Pencil className="h-4 w-4" />
                </button>
                <button
                  onClick={() => confirm(`删除任务「${c.name}」？`) && remove.mutate(c.id)}
                  className="rounded p-1.5 text-err hover:bg-err/10"
                >
                  <Trash2 className="h-4 w-4" />
                </button>
              </div>
            </div>
            <div className="mt-2 rounded-lg bg-black/5 p-2 font-mono text-xs dark:bg-white/5">$ {c.command}</div>
            {c.last_result && (
              <pre className="mt-2 max-h-32 overflow-auto whitespace-pre-wrap rounded-lg bg-black/5 p-2 text-xs text-muted dark:bg-white/5">
                {c.last_result}
              </pre>
            )}
            {result && result.id === c.id && (
              <pre className="mt-2 max-h-32 overflow-auto whitespace-pre-wrap rounded-lg bg-ok/10 p-2 text-xs">
                {result.text}
              </pre>
            )}
          </div>
        ))}
        {crons.length === 0 && (
          <div className="rounded-xl border border-dashed border-border py-16 text-center text-sm text-muted">
            暂无定时任务
          </div>
        )}
      </div>
    </div>
  );
}
