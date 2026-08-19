import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeftRight, Rocket, X } from "lucide-react";
import { api, type Server, type TransferRecord, type UpgradeJob } from "../lib/api";
import { useI18n, type TKey } from "../lib/i18n";

const statusKeys: Record<string, TKey> = {
  pending: "lifecycle.statusPending", running: "lifecycle.statusRunning", completed: "lifecycle.statusCompleted",
  interrupted: "lifecycle.statusInterrupted", verified: "lifecycle.statusVerified", cancelled: "lifecycle.statusCancelled",
  failed: "lifecycle.statusFailed", failure: "lifecycle.statusFailed", success: "lifecycle.statusSuccess", offline: "lifecycle.statusOffline",
};

export default function Lifecycle() {
  const { t, tErr, fmtDateTime } = useI18n();
  const qc = useQueryClient();
  const { data: serverData } = useQuery({ queryKey: ["servers-list"], queryFn: api.servers });
  const { data: userData } = useQuery({ queryKey: ["users"], queryFn: api.users });
  const { data: transferData } = useQuery({ queryKey: ["transfers"], queryFn: api.transfers });
  const { data: jobData } = useQuery({ queryKey: ["upgrade-jobs"], queryFn: api.upgradeJobs, refetchInterval: 3000 });
  const servers = serverData?.servers ?? [];
  const users = userData?.users ?? [];
  const transfers = transferData?.transfers ?? [];
  const jobs = jobData?.jobs ?? [];

  const [transferForm, setTransferForm] = useState<{ server_id: number; to_user_id: number } | null>(null);
  const [newSecret, setNewSecret] = useState("");
  const [upgradeForm, setUpgradeForm] = useState<{ server_ids: number[]; url: string; sha256: string; version: string; concurrency: number } | null>(null);
  const [msg, setMsg] = useState("");

  const createTransfer = useMutation({
    mutationFn: (f: { server_id: number; to_user_id: number }) => api.createTransfer(f.server_id, f.to_user_id),
    onSuccess: (r) => {
      setNewSecret(t("lifecycle.transferStarted", { id: r.transfer.id, status: t(statusKeys[r.transfer.status] ?? r.transfer.status), secret: r.new_secret }));
      setTransferForm(null);
      qc.invalidateQueries({ queryKey: ["transfers"] });
    },
    onError: (e) => setMsg(tErr(e)),
  });
  const cancelTransfer = useMutation({
    mutationFn: api.cancelTransfer,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["transfers"] }),
    onError: (e) => setMsg(tErr(e)),
  });
  const retryTransfer = useMutation({
    mutationFn: api.retryTransfer,
    onSuccess: (r) => {
      setNewSecret(t("lifecycle.transferRetried", { id: r.transfer.id, secret: r.new_secret }));
      qc.invalidateQueries({ queryKey: ["transfers"] });
    },
    onError: (e) => setMsg(tErr(e)),
  });
  const createJob = useMutation({
    mutationFn: api.createUpgradeJob,
    onSuccess: () => {
      setUpgradeForm(null);
      qc.invalidateQueries({ queryKey: ["upgrade-jobs"] });
    },
    onError: (e) => setMsg(t("lifecycle.upgradeFailed", { error: tErr(e) })),
  });

  const statusBadge = (s: string) => {
    const map: Record<string, string> = {
      pending: "bg-warn/15 text-warn", running: "bg-accent/15 text-accent", completed: "bg-ok/15 text-ok", interrupted: "bg-err/15 text-err", verified: "bg-ok/15 text-ok", cancelled: "bg-muted/20 text-muted", failed: "bg-err/15 text-err",
      success: "bg-ok/15 text-ok", failure: "bg-err/15 text-err", offline: "bg-muted/20 text-muted",
    };
    return <span className={`rounded-full px-2 py-0.5 text-xs ${map[s] ?? "bg-muted/20 text-muted"}`}>{t(statusKeys[s] ?? s)}</span>;
  };

  return (
    <div className="space-y-8">
      <div>
        <h1 className="mb-1 flex items-center gap-2 text-xl font-semibold">
          <ArrowLeftRight className="h-5 w-5 text-accent" /> {t("lifecycle.title")}
        </h1>
        <p className="mb-4 text-sm text-muted">{t("lifecycle.subtitle")}</p>
        {msg && <p className="mb-3 text-sm text-err">{msg}</p>}
      </div>

      {/* 过户 */}
      <section>
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-lg font-semibold">{t("lifecycle.transferTitle")}</h2>
          <button
            onClick={() => setTransferForm({ server_id: servers[0]?.id ?? 0, to_user_id: 0 })}
            className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-white"
          >
            <ArrowLeftRight className="h-4 w-4" /> {t("lifecycle.startTransfer")}
          </button>
        </div>
        {transferForm && (
          <div className="mb-4 flex flex-wrap items-end gap-3 rounded-xl border border-border bg-panel p-4">
            <div>
              <div className="mb-1 text-xs text-muted">{t("lifecycle.server")}</div>
              <select value={transferForm.server_id} onChange={(e) => setTransferForm({ ...transferForm, server_id: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm">
                <option value={0}>{t("common.selectServer")}</option>
                {servers.map((s: Server) => <option key={s.id} value={s.id}>{s.name}</option>)}
              </select>
            </div>
            <div>
              <div className="mb-1 text-xs text-muted">{t("lifecycle.toUser")}</div>
              <select value={transferForm.to_user_id} onChange={(e) => setTransferForm({ ...transferForm, to_user_id: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm">
                <option value={0}>{t("lifecycle.pickUser")}</option>
                {users.map((u) => <option key={u.id} value={u.id}>{u.username}</option>)}
              </select>
            </div>
            <button
              disabled={!transferForm.server_id || !transferForm.to_user_id}
              onClick={() => createTransfer.mutate(transferForm)}
              className="rounded-lg bg-accent px-4 py-2 text-sm text-white disabled:opacity-40"
            >
              {t("lifecycle.start")}
            </button>
            <button onClick={() => setTransferForm(null)} className="rounded-lg border border-border px-4 py-2 text-sm">{t("common.cancel")}</button>
          </div>
        )}
        {newSecret && (
          <div className="mb-4 flex items-start gap-2 rounded-xl border border-warn/30 bg-warn/10 p-3 text-sm">
            <pre className="flex-1 whitespace-pre-wrap break-all text-xs">{newSecret}</pre>
            <button onClick={() => navigator.clipboard?.writeText(newSecret)} className="shrink-0 text-accent">{t("common.copy")}</button>
            <button onClick={() => setNewSecret("")} className="shrink-0 text-muted"><X className="h-4 w-4" /></button>
          </div>
        )}
        <div className="overflow-x-auto rounded-xl border border-border bg-panel">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-muted">
                <th className="px-4 py-2.5">{t("common.id")}</th><th className="px-4 py-2.5">{t("lifecycle.server")}</th><th className="px-4 py-2.5">{t("lifecycle.targetUser")}</th>
                <th className="px-4 py-2.5">{t("common.status")}</th><th className="px-4 py-2.5">{t("lifecycle.time")}</th><th className="px-4 py-2.5 text-right">{t("common.actions")}</th>
              </tr>
            </thead>
            <tbody>
              {transfers.map((tr: TransferRecord) => (
                <tr key={tr.id} className="border-b border-border last:border-0">
                  <td className="px-4 py-2.5 tabular text-muted">#{tr.id}</td>
                  <td className="px-4 py-2.5">{tr.server_name}</td>
                  <td className="px-4 py-2.5">{tr.to_username}</td>
                  <td className="px-4 py-2.5">{statusBadge(tr.status)}</td>
                  <td className="px-4 py-2.5 text-xs text-muted">{fmtDateTime(tr.created_at)}</td>
                  <td className="px-4 py-2.5 text-right">
                    {tr.status === "pending" && (
                      <button onClick={() => confirm(t("lifecycle.confirmCancel", { id: tr.id })) && cancelTransfer.mutate(tr.id)} className="rounded border border-err/40 px-2 py-1 text-xs text-err hover:bg-err/10">
                        {t("common.cancel")}
                      </button>
                    )}
                    {tr.status === "failed" && (
                      <button onClick={() => confirm(t("lifecycle.confirmRetry", { id: tr.id })) && retryTransfer.mutate(tr.id)} className="rounded border border-accent/40 px-2 py-1 text-xs text-accent hover:bg-accent/10">
                        {t("common.retry")}
                      </button>
                    )}
                  </td>
                </tr>
              ))}
              {transfers.length === 0 && <tr><td colSpan={6} className="px-4 py-8 text-center text-muted">{t("lifecycle.noTransfers")}</td></tr>}
            </tbody>
          </table>
        </div>
      </section>

      {/* Agent 升级 */}
      <section>
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-lg font-semibold">{t("lifecycle.upgradeTitle")}</h2>
          <button
            onClick={() => setUpgradeForm({ server_ids: [], url: "", sha256: "", version: "", concurrency: 2 })}
            className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-white"
          >
            <Rocket className="h-4 w-4" /> {t("lifecycle.startUpgrade")}
          </button>
        </div>
        {upgradeForm && (
          <div className="mb-4 grid grid-cols-1 gap-3 rounded-xl border border-border bg-panel p-4 md:grid-cols-2">
            <div className="md:col-span-2">
              <div className="mb-1 text-xs text-muted">{t("lifecycle.targetServers")}</div>
              <select
                multiple
                value={upgradeForm.server_ids.map(String)}
                onChange={(e) => setUpgradeForm({ ...upgradeForm, server_ids: Array.from(e.target.selectedOptions, (o) => Number(o.value)) })}
                className="h-28 w-full rounded-lg border border-border bg-bg px-3 py-2 text-sm"
              >
                {servers.map((s: Server) => <option key={s.id} value={s.id}>{s.name}</option>)}
              </select>
            </div>
            <input placeholder={t("lifecycle.artifactUrl")} value={upgradeForm.url} onChange={(e) => setUpgradeForm({ ...upgradeForm, url: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input placeholder={t("lifecycle.sha256")} value={upgradeForm.sha256} onChange={(e) => setUpgradeForm({ ...upgradeForm, sha256: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 font-mono text-sm" />
            <input placeholder={t("lifecycle.version")} value={upgradeForm.version} onChange={(e) => setUpgradeForm({ ...upgradeForm, version: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <div>
              <div className="mb-1 text-xs text-muted">{t("lifecycle.concurrency")}</div>
              <input
                type="number" min={1} max={16}
                value={upgradeForm.concurrency}
                onChange={(e) => setUpgradeForm({ ...upgradeForm, concurrency: Math.max(1, Math.min(16, Number(e.target.value) || 1)) })}
                className="w-full rounded-lg border border-border bg-bg px-3 py-2 text-sm"
              />
            </div>
            <div className="flex gap-2 md:col-span-2">
              <button
                disabled={upgradeForm.server_ids.length === 0 || !upgradeForm.url || !upgradeForm.sha256}
                onClick={() => createJob.mutate(upgradeForm)}
                className="rounded-lg bg-accent px-4 py-2 text-sm text-white disabled:opacity-40"
              >
                {t("lifecycle.upgradeCount", { count: upgradeForm.server_ids.length })}
              </button>
              <button onClick={() => setUpgradeForm(null)} className="rounded-lg border border-border px-4 py-2 text-sm">{t("common.cancel")}</button>
            </div>
          </div>
        )}
        <div className="space-y-3">
          {jobs.map((j: UpgradeJob) => {
            const counts = j.results.reduce<Record<string, number>>((acc, r) => {
              acc[r.status] = (acc[r.status] ?? 0) + 1;
              return acc;
            }, {});
            const terminal = ["success", "failure", "offline", "interrupted"].reduce((n, s) => n + (counts[s] ?? 0), 0);
            return (
              <div key={j.id} className="rounded-xl border border-border bg-panel p-4">
                <div className="mb-2 flex flex-wrap items-center justify-between gap-2 text-sm">
                  <span className="flex items-center gap-2 font-medium">
                    #{j.id} · v{j.version || "?"} {statusBadge(j.status)}
                  </span>
                  <span className="text-xs text-muted">
                    {t("lifecycle.jobSummary", { total: j.target_count, concurrency: j.concurrency, done: terminal, time: fmtDateTime(j.created_at) })}
                  </span>
                </div>
                <div className="mb-2 truncate font-mono text-xs text-muted" title={j.url}>{j.url}</div>
                <div className="flex flex-wrap gap-2">
                  {j.results.map((r) => (
                    <span key={r.server_id} className="flex items-center gap-1.5 rounded-full border border-border px-2.5 py-1 text-xs">
                      {r.name} {statusBadge(r.status)}
                      {r.error && <span className="text-err" title={r.error}>!</span>}
                    </span>
                  ))}
                </div>
              </div>
            );
          })}
          {jobs.length === 0 && <div className="py-6 text-center text-sm text-muted">{t("lifecycle.noJobs")}</div>}
        </div>
      </section>
    </div>
  );
}
