import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeftRight, Rocket, X } from "lucide-react";
import { api, type Server, type TransferRecord, type UpgradeJob } from "../lib/api";
import { fmtDateTime } from "../lib/format";

export default function Lifecycle() {
  const qc = useQueryClient();
  const { data: serverData } = useQuery({ queryKey: ["servers-list"], queryFn: api.servers });
  const { data: userData } = useQuery({ queryKey: ["users"], queryFn: api.users });
  const { data: transferData } = useQuery({ queryKey: ["transfers"], queryFn: api.transfers });
  const { data: jobData } = useQuery({ queryKey: ["upgrade-jobs"], queryFn: api.upgradeJobs });
  const servers = serverData?.servers ?? [];
  const users = userData?.users ?? [];
  const transfers = transferData?.transfers ?? [];
  const jobs = jobData?.jobs ?? [];

  const [transferForm, setTransferForm] = useState<{ server_id: number; to_user_id: number } | null>(null);
  const [newSecret, setNewSecret] = useState("");
  const [upgradeForm, setUpgradeForm] = useState<{ server_ids: number[]; url: string; sha256: string; version: string } | null>(null);
  const [msg, setMsg] = useState("");

  const createTransfer = useMutation({
    mutationFn: (f: { server_id: number; to_user_id: number }) => api.createTransfer(f.server_id, f.to_user_id),
    onSuccess: (r) => {
      setNewSecret(`过户 #${r.transfer.id} 已发起（${r.transfer.status}）。新密钥（交给目标用户配置 Agent）:\n${r.new_secret}`);
      setTransferForm(null);
      qc.invalidateQueries({ queryKey: ["transfers"] });
    },
    onError: (e) => setMsg((e as Error).message),
  });
  const cancelTransfer = useMutation({
    mutationFn: api.cancelTransfer,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["transfers"] }),
    onError: (e) => setMsg((e as Error).message),
  });
  const createJob = useMutation({
    mutationFn: api.createUpgradeJob,
    onSuccess: () => {
      setUpgradeForm(null);
      qc.invalidateQueries({ queryKey: ["upgrade-jobs"] });
    },
    onError: (e) => setMsg("升级失败: " + (e as Error).message),
  });

  const statusBadge = (s: string) => {
    const map: Record<string, string> = {
      pending: "bg-warn/15 text-warn", verified: "bg-ok/15 text-ok", cancelled: "bg-muted/20 text-muted", failed: "bg-err/15 text-err",
      success: "bg-ok/15 text-ok", failure: "bg-err/15 text-err", offline: "bg-muted/20 text-muted",
    };
    return <span className={`rounded-full px-2 py-0.5 text-xs ${map[s] ?? "bg-muted/20 text-muted"}`}>{s}</span>;
  };

  return (
    <div className="space-y-8">
      <div>
        <h1 className="mb-1 flex items-center gap-2 text-xl font-semibold">
          <ArrowLeftRight className="h-5 w-5 text-accent" /> 服务器生命周期
        </h1>
        <p className="mb-4 text-sm text-muted">服务器过户与 Agent 批量升级</p>
        {msg && <p className="mb-3 text-sm text-err">{msg}</p>}
      </div>

      {/* 过户 */}
      <section>
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-lg font-semibold">服务器过户</h2>
          <button
            onClick={() => setTransferForm({ server_id: servers[0]?.id ?? 0, to_user_id: 0 })}
            className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-white"
          >
            <ArrowLeftRight className="h-4 w-4" /> 发起过户
          </button>
        </div>
        {transferForm && (
          <div className="mb-4 flex flex-wrap items-end gap-3 rounded-xl border border-border bg-panel p-4">
            <div>
              <div className="mb-1 text-xs text-muted">服务器</div>
              <select value={transferForm.server_id} onChange={(e) => setTransferForm({ ...transferForm, server_id: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm">
                <option value={0}>选择服务器</option>
                {servers.map((s: Server) => <option key={s.id} value={s.id}>{s.name}</option>)}
              </select>
            </div>
            <div>
              <div className="mb-1 text-xs text-muted">过户给用户</div>
              <select value={transferForm.to_user_id} onChange={(e) => setTransferForm({ ...transferForm, to_user_id: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm">
                <option value={0}>选择用户</option>
                {users.map((u) => <option key={u.id} value={u.id}>{u.username}</option>)}
              </select>
            </div>
            <button
              disabled={!transferForm.server_id || !transferForm.to_user_id}
              onClick={() => createTransfer.mutate(transferForm)}
              className="rounded-lg bg-accent px-4 py-2 text-sm text-white disabled:opacity-40"
            >
              发起
            </button>
            <button onClick={() => setTransferForm(null)} className="rounded-lg border border-border px-4 py-2 text-sm">取消</button>
          </div>
        )}
        {newSecret && (
          <div className="mb-4 flex items-start gap-2 rounded-xl border border-warn/30 bg-warn/10 p-3 text-sm">
            <pre className="flex-1 whitespace-pre-wrap break-all text-xs">{newSecret}</pre>
            <button onClick={() => navigator.clipboard?.writeText(newSecret)} className="shrink-0 text-accent">复制</button>
            <button onClick={() => setNewSecret("")} className="shrink-0 text-muted"><X className="h-4 w-4" /></button>
          </div>
        )}
        <div className="overflow-x-auto rounded-xl border border-border bg-panel">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-muted">
                <th className="px-4 py-2.5">ID</th><th className="px-4 py-2.5">服务器</th><th className="px-4 py-2.5">目标用户</th>
                <th className="px-4 py-2.5">状态</th><th className="px-4 py-2.5">时间</th><th className="px-4 py-2.5 text-right">操作</th>
              </tr>
            </thead>
            <tbody>
              {transfers.map((t: TransferRecord) => (
                <tr key={t.id} className="border-b border-border last:border-0">
                  <td className="px-4 py-2.5 tabular text-muted">#{t.id}</td>
                  <td className="px-4 py-2.5">{t.server_name}</td>
                  <td className="px-4 py-2.5">{t.to_username}</td>
                  <td className="px-4 py-2.5">{statusBadge(t.status)}</td>
                  <td className="px-4 py-2.5 text-xs text-muted">{fmtDateTime(t.created_at)}</td>
                  <td className="px-4 py-2.5 text-right">
                    {t.status === "pending" && (
                      <button onClick={() => confirm(`取消过户 #${t.id}？密钥将回滚`) && cancelTransfer.mutate(t.id)} className="rounded border border-err/40 px-2 py-1 text-xs text-err hover:bg-err/10">
                        取消
                      </button>
                    )}
                  </td>
                </tr>
              ))}
              {transfers.length === 0 && <tr><td colSpan={6} className="px-4 py-8 text-center text-muted">暂无过户记录</td></tr>}
            </tbody>
          </table>
        </div>
      </section>

      {/* Agent 升级 */}
      <section>
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-lg font-semibold">Agent 批量升级</h2>
          <button
            onClick={() => setUpgradeForm({ server_ids: [], url: "", sha256: "", version: "" })}
            className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-white"
          >
            <Rocket className="h-4 w-4" /> 发起升级
          </button>
        </div>
        {upgradeForm && (
          <div className="mb-4 grid grid-cols-1 gap-3 rounded-xl border border-border bg-panel p-4 md:grid-cols-2">
            <div className="md:col-span-2">
              <div className="mb-1 text-xs text-muted">目标服务器（Ctrl 多选）</div>
              <select
                multiple
                value={upgradeForm.server_ids.map(String)}
                onChange={(e) => setUpgradeForm({ ...upgradeForm, server_ids: Array.from(e.target.selectedOptions, (o) => Number(o.value)) })}
                className="h-28 w-full rounded-lg border border-border bg-bg px-3 py-2 text-sm"
              >
                {servers.map((s: Server) => <option key={s.id} value={s.id}>{s.name}</option>)}
              </select>
            </div>
            <input placeholder="制品 URL（http/https）" value={upgradeForm.url} onChange={(e) => setUpgradeForm({ ...upgradeForm, url: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input placeholder="SHA-256" value={upgradeForm.sha256} onChange={(e) => setUpgradeForm({ ...upgradeForm, sha256: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 font-mono text-sm" />
            <input placeholder="版本号（如 0.3.0）" value={upgradeForm.version} onChange={(e) => setUpgradeForm({ ...upgradeForm, version: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <div className="flex gap-2 md:col-span-2">
              <button
                disabled={upgradeForm.server_ids.length === 0 || !upgradeForm.url || !upgradeForm.sha256}
                onClick={() => createJob.mutate(upgradeForm)}
                className="rounded-lg bg-accent px-4 py-2 text-sm text-white disabled:opacity-40"
              >
                升级 {upgradeForm.server_ids.length} 台
              </button>
              <button onClick={() => setUpgradeForm(null)} className="rounded-lg border border-border px-4 py-2 text-sm">取消</button>
            </div>
          </div>
        )}
        <div className="space-y-3">
          {jobs.map((j: UpgradeJob) => (
            <div key={j.id} className="rounded-xl border border-border bg-panel p-4">
              <div className="mb-2 flex items-center justify-between text-sm">
                <span className="font-medium">{j.id} · v{j.version}</span>
                <span className="text-xs text-muted">{fmtDateTime(j.created_at)}</span>
              </div>
              <div className="flex flex-wrap gap-2">
                {Object.values(j.results).map((r) => (
                  <span key={r.server_id} className="flex items-center gap-1.5 rounded-full border border-border px-2.5 py-1 text-xs">
                    {r.name} {statusBadge(r.status)}
                    {r.error && <span className="text-err" title={r.error}>!</span>}
                  </span>
                ))}
              </div>
            </div>
          ))}
          {jobs.length === 0 && <div className="py-6 text-center text-sm text-muted">暂无升级任务</div>}
        </div>
      </section>
    </div>
  );
}
