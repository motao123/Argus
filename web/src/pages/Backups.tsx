import { useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronDown, ChevronRight, FlaskConical, History, Pencil, Play, Plus, ShieldCheck, Trash2 } from "lucide-react";
import { api, type BackupRun, type BackupSchedule } from "../lib/api";
import { useI18n, type TKey } from "../lib/i18n";

const emptyForm = { name: "", cron: "0 3 * * *", target: "", keep_count: 7, enabled: false };
type FormState = typeof emptyForm & { id?: number };

const statusClass: Record<string, string> = {
  success: "bg-ok/15 text-ok",
  failed: "bg-err/15 text-err",
  running: "bg-accent/15 text-accent",
};
const statusKeys: Record<string, TKey> = {
  success: "backups.statusSuccess",
  failed: "backups.statusFailed",
  running: "backups.running",
};
const triggerKeys: Record<string, TKey> = {
  cron: "backups.triggerCron",
  manual: "backups.triggerManual",
};

const fmtBytes = (n: number) =>
  n >= 1 << 30 ? `${(n / (1 << 30)).toFixed(2)} GiB` : n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(2)} MiB` : n >= 1024 ? `${(n / 1024).toFixed(1)} KiB` : `${n} B`;

function Runs({ scheduleId }: { scheduleId: number }) {
  const { t, fmtDateTime } = useI18n();
  const { data } = useQuery({ queryKey: ["backup-runs", scheduleId], queryFn: () => api.backupRuns(scheduleId), refetchInterval: 3000 });
  const runs = data?.runs ?? [];
  return (
    <div className="mt-3 border-t border-border pt-3">
      <div className="mb-2 text-xs font-medium text-muted">{t("backups.runs")}</div>
      {runs.map((r: BackupRun) => (
        <div key={r.id} className="flex flex-wrap items-center gap-2 rounded-lg px-2 py-1.5 text-xs">
          <span className={`rounded-full px-2 py-0.5 ${statusClass[r.status] ?? "bg-muted/20 text-muted"}`}>{t(statusKeys[r.status] ?? r.status)}</span>
          <span>{t(triggerKeys[r.trigger] ?? r.trigger)}</span>
          <span className="tabular-nums text-muted">{fmtDateTime(r.created_at)}</span>
          <span className="tabular-nums text-muted">{fmtBytes(r.size)} · {r.duration_ms}ms</span>
          <span className="max-w-xs truncate font-mono text-[10px] text-muted">{r.target}</span>
          {r.sha256 && <span className="font-mono text-[10px] text-muted">sha256:{r.sha256.slice(0, 12)}…</span>}
          {r.error && <span className="text-err">✕ {r.error}</span>}
        </div>
      ))}
      {runs.length === 0 && <div className="px-2 py-2 text-xs text-muted">{t("backups.noRuns")}</div>}
    </div>
  );
}

function Drill({ schedule }: { schedule: BackupSchedule }) {
  const { t, tErr } = useI18n();
  const qc = useQueryClient();
  const fileRef = useRef<HTMLInputElement>(null);
  const [result, setResult] = useState<string>("");
  const [busy, setBusy] = useState(false);

  const runDrill = async (file?: File) => {
    setBusy(true);
    setResult("");
    try {
      const r = await api.backupDrill(schedule.id, file);
      setResult(`${t("backups.drillResultOk", { size: fmtBytes(r.db_size) })} key=${r.key_id}`);
      qc.invalidateQueries({ queryKey: ["backup-schedules"] });
    } catch (e) {
      setResult(t("backups.drillFailed", { error: tErr(e) }));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mt-3 rounded-lg border border-border p-3">
      <div className="mb-2 flex items-center gap-2 text-xs font-medium">
        <FlaskConical className="h-3.5 w-3.5 text-accent" /> {t("backups.drill")}
      </div>
      <p className="mb-2 text-xs text-muted">{t("backups.drillHelp")}</p>
      <div className="flex flex-wrap gap-2">
        <button
          disabled={busy}
          onClick={() => runDrill()}
          className="rounded-lg border border-border px-3 py-1.5 text-xs disabled:opacity-40"
        >
          {t("backups.drillUsingLatest")}
        </button>
        <button
          disabled={busy}
          onClick={() => fileRef.current?.click()}
          className="rounded-lg border border-border px-3 py-1.5 text-xs disabled:opacity-40"
        >
          {t("backups.drillFile")}
        </button>
        <input ref={fileRef} type="file" accept=".argusenc" className="hidden" onChange={(e) => e.target.files?.[0] && runDrill(e.target.files[0])} />
      </div>
      {result && <div className="mt-2 break-all text-xs text-muted">{result}</div>}
    </div>
  );
}

export default function Backups() {
  const { t, fmtDateTime } = useI18n();
  const qc = useQueryClient();
  const { data } = useQuery({ queryKey: ["backup-schedules"], queryFn: api.backupSchedules, refetchInterval: 5000 });
  const schedules = data?.schedules ?? [];
  const [form, setForm] = useState<FormState | null>(null);
  const [expanded, setExpanded] = useState<number | null>(null);

  const invalidate = () => qc.invalidateQueries({ queryKey: ["backup-schedules"] });
  const save = useMutation({
    mutationFn: (f: FormState) =>
      f.id
        ? api.updateBackupSchedule(f.id, { name: f.name, cron: f.cron, target: f.target, keep_count: f.keep_count, enabled: f.enabled })
        : api.createBackupSchedule({ name: f.name, cron: f.cron, target: f.target, keep_count: f.keep_count, enabled: f.enabled }),
    onSuccess: () => {
      setForm(null);
      invalidate();
    },
  });
  const remove = useMutation({ mutationFn: api.deleteBackupSchedule, onSuccess: invalidate });
  const toggle = useMutation({
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) => api.updateBackupSchedule(id, { enabled }),
    onSuccess: invalidate,
  });
  const run = useMutation({
    mutationFn: api.runBackupSchedule,
    onSuccess: (_, id) => {
      setExpanded(id);
      invalidate();
      qc.invalidateQueries({ queryKey: ["backup-runs", id] });
    },
  });

  return (
    <div>
      <div className="mb-5 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">{t("backups.title")}</h1>
          <p className="text-sm text-muted">{t("backups.subtitle")}</p>
        </div>
        <button onClick={() => setForm(emptyForm)} className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-white">
          <Plus className="h-4 w-4" /> {t("backups.create")}
        </button>
      </div>

      {form && (
        <div className="mb-5 rounded-xl border border-border bg-panel p-4">
          <h2 className="mb-3 text-sm font-medium">{form.id ? t("backups.edit") : t("backups.create")}</h2>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-4">
            <input placeholder={t("backups.name")} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input placeholder={t("backups.cron")} value={form.cron} onChange={(e) => setForm({ ...form, cron: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input type="number" min={1} max={365} value={form.keep_count} onChange={(e) => setForm({ ...form, keep_count: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={form.enabled} onChange={(e) => setForm({ ...form, enabled: e.target.checked })} /> {t("backups.enabled")}
            </label>
            <input placeholder={t("backups.target")} value={form.target} onChange={(e) => setForm({ ...form, target: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm sm:col-span-4" />
          </div>
          <div className="mt-2 flex gap-4 text-xs text-muted">
            <span>{t("backups.cronHelp")}</span>
            <span>{t("backups.targetHelp")}</span>
          </div>
          <div className="mt-3 flex gap-2">
            <button onClick={() => save.mutate(form)} className="rounded-lg bg-accent px-4 py-1.5 text-sm text-white">{t("common.save")}</button>
            <button onClick={() => setForm(null)} className="rounded-lg border border-border px-4 py-1.5 text-sm text-muted">{t("common.cancel")}</button>
          </div>
        </div>
      )}

      <div className="space-y-3">
        {schedules.map((s: BackupSchedule) => (
          <div key={s.id} className="rounded-xl border border-border bg-panel p-4">
            <div className="flex items-center justify-between">
              <div className="flex flex-wrap items-center gap-3">
                <span className="font-medium">{s.name}</span>
                <span className="rounded-full bg-accent/10 px-2 py-0.5 text-xs text-accent">{s.cron}</span>
                <span className={`rounded-full px-2 py-0.5 text-xs ${s.enabled ? "bg-ok/15 text-ok" : "bg-muted/20 text-muted"}`}>
                  {s.enabled ? t("common.enabled") : t("common.disabled")}
                </span>
                {s.last_status && (
                  <span className={`rounded-full px-2 py-0.5 text-xs ${statusClass[s.last_status] ?? "bg-muted/20 text-muted"}`}>
                    {t(statusKeys[s.last_status] ?? s.last_status)}
                  </span>
                )}
                <span className="text-xs text-muted">
                  {s.last_run_at ? fmtDateTime(s.last_run_at) : t("backups.statusIdle")}
                  {s.last_size > 0 ? ` · ${fmtBytes(s.last_size)}` : ""}
                </span>
                {s.last_error && <span className="text-xs text-err">✕ {s.last_error}</span>}
              </div>
              <div className="flex gap-1">
                <button disabled={run.isPending} onClick={() => run.mutate(s.id)} className="flex items-center gap-1 rounded-lg px-2 py-1.5 text-sm text-ok hover:bg-ok/10" title={t("backups.runNow")}>
                  <Play className="h-4 w-4" />
                </button>
                <button onClick={() => setExpanded(expanded === s.id ? null : s.id)} className="flex items-center gap-1 rounded-lg px-2 py-1.5 text-sm hover:bg-black/5">
                  {expanded === s.id ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
                  <History className="h-4 w-4" />
                </button>
                <button onClick={() => setForm({ name: s.name, cron: s.cron, target: s.target, keep_count: s.keep_count, enabled: s.enabled, id: s.id })} className="rounded p-1.5">
                  <Pencil className="h-4 w-4" />
                </button>
                <button onClick={() => confirm(t("backups.confirmDelete", { name: s.name })) && remove.mutate(s.id)} className="rounded p-1.5 text-err">
                  <Trash2 className="h-4 w-4" />
                </button>
              </div>
            </div>
            <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted">
              <span className="max-w-md truncate font-mono">{s.target}</span>
              <span>{t("backups.keepCount")}: {s.keep_count}</span>
              {s.key_source && <span title={t("backups.keySourceHelp")}>{t("backups.keySource")}: {s.key_source}</span>}
              {s.key_id && <span className="font-mono">key={s.key_id}</span>}
            </div>
            {expanded === s.id && (
              <>
                <Runs scheduleId={s.id} />
                <Drill schedule={s} />
              </>
            )}
          </div>
        ))}
        {schedules.length === 0 && (
          <div className="rounded-xl border border-dashed border-border py-16 text-center text-sm text-muted">{t("backups.noSchedules")}</div>
        )}
      </div>

      <div className="mt-6 flex items-center gap-2 rounded-lg bg-ok/5 p-3 text-xs text-muted">
        <ShieldCheck className="h-4 w-4 shrink-0 text-ok" />
        {t("backups.keySourceHelp")}
      </div>
    </div>
  );
}
