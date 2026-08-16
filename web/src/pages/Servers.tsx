import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, KeyRound, Pencil, Plus, TerminalSquare, Trash2 } from "lucide-react";
import { api, type Server } from "../lib/api";
import { useServers } from "../context/servers";

interface FormState {
  id?: number;
  name: string;
  group: string;
  note: string;
}

const emptyForm: FormState = { name: "", group: "", note: "" };

export default function Servers() {
  const { servers } = useServers();
  const qc = useQueryClient();
  const [form, setForm] = useState<FormState | null>(null);
  const [secret, setSecret] = useState<string | null>(null);
  const [error, setError] = useState("");
  const [execResult, setExecResult] = useState<string>("");
  const [execTarget, setExecTarget] = useState<Server | null>(null);
  const [execCmd, setExecCmd] = useState("");

  const invalidate = () => qc.invalidateQueries({ queryKey: ["servers"] });

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

  const remove = useMutation({
    mutationFn: api.deleteServer,
    onSuccess: invalidate,
  });

  const runExec = useMutation({
    mutationFn: () => api.exec(execTarget!.id, execCmd),
    onSuccess: (r) => setExecResult(`exit=${r.code}\n${r.output || r.error || ""}`),
    onError: (e) => setExecResult((e as Error).message),
  });

  return (
    <div>
      <div className="mb-5 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">服务器管理</h1>
          <p className="text-sm text-muted">共 {servers.length} 台 · 部署 agent 后自动注册</p>
        </div>
        <button
          onClick={() => {
            setForm(emptyForm);
            setSecret(null);
          }}
          className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-white hover:opacity-90"
        >
          <Plus className="h-4 w-4" />
          添加服务器
        </button>
      </div>

      {/* 表单 */}
      {form && (
        <div className="mb-5 rounded-xl border border-border bg-panel p-4">
          <h2 className="mb-3 text-sm font-medium">{form.id ? "编辑服务器" : "添加服务器"}</h2>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <input
              placeholder="名称"
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent"
            />
            <input
              placeholder="分组"
              value={form.group}
              onChange={(e) => setForm({ ...form, group: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent"
            />
            <input
              placeholder="备注"
              value={form.note}
              onChange={(e) => setForm({ ...form, note: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent"
            />
          </div>
          {secret && (
            <div className="mt-3 flex items-center gap-2 rounded-lg bg-ok/10 p-3 text-sm">
              <KeyRound className="h-4 w-4 text-ok" />
              <span className="flex-1 break-all">{secret}</span>
              <button
                onClick={() => navigator.clipboard?.writeText(secret)}
                className="flex items-center gap-1 text-muted hover:text-fg"
              >
                <Copy className="h-3.5 w-3.5" /> 复制
              </button>
            </div>
          )}
          {error && <p className="mt-2 text-sm text-err">{error}</p>}
          <div className="mt-3 flex gap-2">
            <button
              onClick={() => save.mutate(form)}
              className="rounded-lg bg-accent px-4 py-1.5 text-sm text-white hover:opacity-90"
            >
              保存
            </button>
            <button onClick={() => setForm(null)} className="rounded-lg border border-border px-4 py-1.5 text-sm text-muted">
              取消
            </button>
          </div>
        </div>
      )}

      {/* 列表 */}
      <div className="overflow-hidden rounded-xl border border-border bg-panel">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted">
              <th className="px-4 py-3 font-normal">ID</th>
              <th className="px-4 py-3 font-normal">名称</th>
              <th className="px-4 py-3 font-normal">分组</th>
              <th className="px-4 py-3 font-normal">系统</th>
              <th className="px-4 py-3 font-normal">状态</th>
              <th className="px-4 py-3 font-normal">CPU</th>
              <th className="px-4 py-3 font-normal">备注</th>
              <th className="px-4 py-3 text-right font-normal">操作</th>
            </tr>
          </thead>
          <tbody>
            {servers.map((s) => (
              <tr key={s.id} className="border-b border-border last:border-0 hover:bg-black/2 dark:hover:bg-white/2">
                <td className="px-4 py-3 tabular text-muted">#{s.id}</td>
                <td className="px-4 py-3 font-medium">{s.name}</td>
                <td className="px-4 py-3">{s.group || "—"}</td>
                <td className="px-4 py-3 text-muted">{s.host?.platform || "—"}</td>
                <td className="px-4 py-3">
                  <span className={`rounded-full px-2 py-0.5 text-xs ${s.online ? "bg-ok/15 text-ok" : "bg-err/15 text-err"}`}>
                    {s.online ? "在线" : "离线"}
                  </span>
                </td>
                <td className="px-4 py-3 tabular">{s.online ? `${s.cpu.toFixed(1)}%` : "—"}</td>
                <td className="px-4 py-3 text-muted">{s.note || "—"}</td>
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
                      onClick={() => setForm({ id: s.id, name: s.name, group: s.group, note: s.note })}
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
          </tbody>
        </table>
      </div>

      {/* 远程执行弹层 */}
      {execTarget && (
        <div className="fixed inset-0 z-20 flex items-center justify-center bg-black/40" onClick={() => setExecTarget(null)}>
          <div className="w-full max-w-lg rounded-xl border border-border bg-panel p-5" onClick={(e) => e.stopPropagation()}>
            <h2 className="mb-3 text-sm font-medium">
              在「{execTarget.name}」上执行命令
            </h2>
            <input
              value={execCmd}
              onChange={(e) => setExecCmd(e.target.value)}
              placeholder="例如: uptime"
              className="w-full rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent"
              onKeyDown={(e) => e.key === "Enter" && runExec.mutate()}
            />
            <button
              onClick={() => runExec.mutate()}
              disabled={runExec.isPending}
              className="mt-3 rounded-lg bg-accent px-4 py-1.5 text-sm text-white hover:opacity-90 disabled:opacity-50"
            >
              {runExec.isPending ? "执行中…" : "执行"}
            </button>
            {execResult && (
              <pre className="mt-3 max-h-48 overflow-auto rounded-lg bg-black/5 p-3 text-xs dark:bg-white/5">{execResult}</pre>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
