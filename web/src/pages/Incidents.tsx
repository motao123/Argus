// 状态页管理（里程碑9）：事故时间线 + 维护窗口 CRUD。
// 读取公开（状态页资源），管理操作按 owner/admin 隔离。
import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CalendarClock, CheckCircle2, Pencil, Plus, Repeat, Trash2, TriangleAlert } from "lucide-react";
import { api, type Incident, type MaintenanceWindow, type Server } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { fmtMinutes, severityTone, windowState } from "../lib/status";

// ---- 时间输入转换（datetime-local ↔ ISO）----
function toLocalInput(iso: string | null | undefined): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function fromLocalInput(v: string): string {
  if (!v) return "";
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return "";
  return d.toISOString();
}

// ---- 服务器多选 ----
function ServerPicker({ value, onChange, servers }: { value: string; onChange: (v: string) => void; servers: Server[] }) {
  const { t } = useI18n();
  const ids = useMemo(() => new Set(value.split(",").map(Number).filter(Boolean)), [value]);
  const toggle = (id: number) => {
    const next = new Set(ids);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    onChange(Array.from(next).sort((a, b) => a - b).join(","));
  };
  const chip = (on: boolean) =>
    `rounded-full px-2.5 py-1 text-xs border ${on ? "border-accent bg-accent/10 text-accent" : "border-border text-muted hover:text-fg"}`;
  return (
    <div className="flex flex-wrap gap-1.5">
      <button type="button" onClick={() => onChange("")} className={chip(value === "")}>
        {t("incidents.allServers")}
      </button>
      {servers.map((s) => (
        <button key={s.id} type="button" onClick={() => toggle(s.id)} className={chip(ids.has(s.id))}>
          {s.name}
        </button>
      ))}
    </div>
  );
}

const inputCls = "rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent";
const btnCls = "rounded-lg bg-accent px-4 py-1.5 text-sm text-white hover:opacity-90 disabled:opacity-40";
const ghostCls = "rounded-lg border border-border px-4 py-1.5 text-sm text-muted hover:text-fg";

const emptyIncident = { title: "", severity: "major" as Incident["severity"], status: "ongoing" as Incident["status"], server_ids: "", notes: "", start_at: "", end_at: "" };
const emptyWindow = { title: "", server_ids: "", start_at: "", end_at: "", recurring: false };

export default function Incidents() {
  const { t, fmtDateTime, fmtDuration } = useI18n();
  const qc = useQueryClient();
  const [error, setError] = useState("");
  const { data: incData } = useQuery({ queryKey: ["incidents"], queryFn: api.incidents });
  const { data: mwData } = useQuery({ queryKey: ["maintenance-windows"], queryFn: api.maintenanceWindows });
  const { data: srvData } = useQuery({ queryKey: ["servers-public"], queryFn: api.servers });
  const incidents = incData?.incidents ?? [];
  const windows = mwData?.windows ?? [];
  const servers = srvData?.servers ?? [];

  const [incForm, setIncForm] = useState<(typeof emptyIncident) & { id?: number } | null>(null);
  const [mwForm, setMwForm] = useState<(typeof emptyWindow) & { id?: number } | null>(null);

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ["incidents"] });
    qc.invalidateQueries({ queryKey: ["maintenance-windows"] });
  };
  const onErr = (e: unknown) => setError((e as Error).message);

  const saveInc = useMutation({
    mutationFn: (v: (typeof emptyIncident) & { id?: number }) =>
      api.saveIncident({ ...v, start_at: fromLocalInput(v.start_at), end_at: v.end_at ? fromLocalInput(v.end_at) : undefined }),
    onSuccess: () => { setIncForm(null); invalidate(); },
    onError: onErr,
  });
  const resolveInc = useMutation({ mutationFn: (id: number) => api.resolveIncident(id), onSuccess: invalidate, onError: onErr });
  const deleteInc = useMutation({ mutationFn: (id: number) => api.deleteIncident(id), onSuccess: invalidate, onError: onErr });

  const saveMw = useMutation({
    mutationFn: (v: (typeof emptyWindow) & { id?: number }) =>
      api.saveMaintenanceWindow({ ...v, start_at: fromLocalInput(v.start_at), end_at: fromLocalInput(v.end_at) }),
    onSuccess: () => { setMwForm(null); invalidate(); },
    onError: onErr,
  });
  const deleteMw = useMutation({ mutationFn: (id: number) => api.deleteMaintenanceWindow(id), onSuccess: invalidate, onError: onErr });

  // 事故排序：进行中在前（按严重级别），其后按开始时间倒序
  const sortedIncidents = useMemo(
    () =>
      [...incidents].sort((a, b) => {
        if (a.status !== b.status) return a.status === "ongoing" ? -1 : 1;
        return new Date(b.start_at).getTime() - new Date(a.start_at).getTime();
      }),
    [incidents],
  );

  const severityLabel: Record<Incident["severity"], string> = {
    minor: t("incidents.severityMinor"),
    major: t("incidents.severityMajor"),
    critical: t("incidents.severityCritical"),
  };

  return (
    <div>
      <div className="mb-5">
        <h1 className="flex items-center gap-2 text-xl font-semibold">
          <TriangleAlert className="h-5 w-5 text-accent" /> {t("incidents.title")}
        </h1>
        <p className="text-sm text-muted">{t("incidents.subtitle")}</p>
      </div>
      {error && <p className="mb-3 text-sm text-err">{error}</p>}

      {/* ---- 事故 ---- */}
      <IncidentSection
        incidents={sortedIncidents}
        servers={servers}
        form={incForm}
        setForm={setIncForm}
        save={saveInc}
        resolve={resolveInc}
        del={deleteInc}
      />

      {/* ---- 维护窗口 ---- */}
      <div className="rounded-xl border border-border bg-panel">
        <div className="flex items-center justify-between border-b border-border px-4 py-3">
          <h2 className="flex items-center gap-2 text-sm font-medium">
            <CalendarClock className="h-4 w-4 text-accent" /> {t("mw.title")}
          </h2>
          <button onClick={() => setMwForm(emptyWindow)} className="flex items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-sm text-white hover:opacity-90">
            <Plus className="h-4 w-4" /> {t("mw.create")}
          </button>
        </div>
        <p className="border-b border-border px-4 py-2 text-xs text-muted">{t("mw.subtitle")}</p>

        {mwForm && (
          <div className="border-b border-border p-4">
            <h3 className="mb-3 text-sm font-medium">{mwForm.id ? t("mw.edit") : t("mw.create")}</h3>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
              <input placeholder={t("mw.titleField")} value={mwForm.title} onChange={(e) => setMwForm({ ...mwForm, title: e.target.value })} className={inputCls} />
              <input type="datetime-local" aria-label={t("mw.startAt")} value={mwForm.start_at} onChange={(e) => setMwForm({ ...mwForm, start_at: e.target.value })} className={inputCls} />
              <input type="datetime-local" aria-label={t("mw.endAt")} value={mwForm.end_at} onChange={(e) => setMwForm({ ...mwForm, end_at: e.target.value })} className={inputCls} />
              <label className="flex items-center gap-2 text-sm">
                <input type="checkbox" checked={mwForm.recurring} onChange={(e) => setMwForm({ ...mwForm, recurring: e.target.checked })} />
                <Repeat className="h-4 w-4 text-muted" /> {t("mw.recurring")}
              </label>
            </div>
            {mwForm.recurring && <p className="mt-2 text-xs text-muted">{t("mw.recurringHint")}</p>}
            <div className="mt-3">
              <ServerPicker value={mwForm.server_ids} onChange={(v) => setMwForm({ ...mwForm, server_ids: v })} servers={servers} />
            </div>
            <div className="mt-3 flex gap-2">
              <button onClick={() => saveMw.mutate(mwForm)} disabled={saveMw.isPending} className={btnCls}>{t("common.save")}</button>
              <button onClick={() => setMwForm(null)} className={ghostCls}>{t("common.cancel")}</button>
            </div>
          </div>
        )}

        {windows.length === 0 ? (
          <p className="px-4 py-10 text-center text-sm text-muted">{t("mw.empty")}</p>
        ) : (
          <ul className="divide-y divide-border">
            {windows.map((w) => {
              const st = windowState(w);
              return (
                <li key={w.id} className="flex items-start gap-3 px-4 py-3">
                  <span className={`mt-1.5 h-2.5 w-2.5 shrink-0 rounded-full ${st === "active" ? "bg-warn" : st === "upcoming" ? "bg-accent" : "bg-muted"}`} />
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-medium">{w.title}</span>
                      {w.recurring && (
                        <span className="flex items-center gap-1 rounded-full bg-accent/10 px-2 py-0.5 text-xs text-accent">
                          <Repeat className="h-3 w-3" /> {t("mw.recurring")}
                        </span>
                      )}
                      <span className={`rounded-full px-2 py-0.5 text-xs ${st === "active" ? "bg-warn/10 text-warn" : st === "upcoming" ? "bg-accent/10 text-accent" : "bg-black/5 text-muted dark:bg-white/10"}`}>
                        {st === "active" ? t("mw.active") : st === "upcoming" ? t("mw.upcoming") : t("mw.ended")}
                      </span>
                      {w.server_ids === "" && <span className="text-xs text-muted">{t("mw.allServers")}</span>}
                    </div>
                    <div className="mt-0.5 text-xs text-muted">
                      {fmtDateTime(w.start_at)} — {fmtDateTime(w.end_at)} · {fmtMinutes((new Date(w.end_at).getTime() - new Date(w.start_at).getTime()) / 60000)}
                    </div>
                  </div>
                  <div className="flex shrink-0 gap-1">
                    <button onClick={() => setMwForm({ ...emptyWindow, ...w, start_at: toLocalInput(w.start_at), end_at: toLocalInput(w.end_at) })} className="rounded-lg p-1.5 text-muted hover:bg-black/5 dark:hover:bg-white/5" title={t("common.edit")}>
                      <Pencil className="h-4 w-4" />
                    </button>
                    <button onClick={() => { if (window.confirm(t("mw.deleteConfirm"))) deleteMw.mutate(w.id); }} className="rounded-lg p-1.5 text-err hover:bg-err/10" title={t("common.delete")}>
                      <Trash2 className="h-4 w-4" />
                    </button>
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </div>
  );
}

// ---- 事故区块（列表 + 创建/编辑表单）----
function IncidentSection({
  incidents,
  servers,
  form,
  setForm,
  save,
  resolve,
  del,
}: {
  incidents: Incident[];
  servers: Server[];
  form: ((typeof emptyIncident) & { id?: number }) | null;
  setForm: (v: ((typeof emptyIncident) & { id?: number }) | null) => void;
  save: { mutate: (v: (typeof emptyIncident) & { id?: number }) => void; isPending: boolean };
  resolve: { mutate: (id: number) => void };
  del: { mutate: (id: number) => void };
}) {
  const { t, fmtDateTime, fmtDuration } = useI18n();
  const severityLabel: Record<Incident["severity"], string> = {
    minor: t("incidents.severityMinor"),
    major: t("incidents.severityMajor"),
    critical: t("incidents.severityCritical"),
  };
  return (
    <div className="mb-6 rounded-xl border border-border bg-panel">
      <div className="flex items-center justify-between border-b border-border px-4 py-3">
        <h2 className="text-sm font-medium">{t("incidents.timeline")}</h2>
        <button onClick={() => setForm(emptyIncident)} className="flex items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-sm text-white hover:opacity-90">
          <Plus className="h-4 w-4" /> {t("incidents.create")}
        </button>
      </div>

      {form && (
        <div className="border-b border-border p-4">
          <h3 className="mb-3 text-sm font-medium">{form.id ? t("incidents.edit") : t("incidents.create")}</h3>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <input placeholder={t("incidents.titleField")} value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} className={inputCls} />
            <select value={form.severity} onChange={(e) => setForm({ ...form, severity: e.target.value as Incident["severity"] })} className={inputCls}>
              {(["minor", "major", "critical"] as const).map((s) => (
                <option key={s} value={s}>{severityLabel[s]}</option>
              ))}
            </select>
            <select value={form.status} onChange={(e) => setForm({ ...form, status: e.target.value as Incident["status"] })} className={inputCls}>
              <option value="ongoing">{t("incidents.ongoing")}</option>
              <option value="resolved">{t("incidents.resolved")}</option>
            </select>
            <input type="datetime-local" aria-label={t("incidents.startAt")} value={form.start_at} onChange={(e) => setForm({ ...form, start_at: e.target.value })} className={inputCls} />
          </div>
          {form.status === "resolved" && (
            <input type="datetime-local" aria-label={t("incidents.endAt")} value={form.end_at} onChange={(e) => setForm({ ...form, end_at: e.target.value })} className={`${inputCls} mt-3 w-full sm:w-64`} placeholder={t("incidents.endAt")} />
          )}
          <textarea placeholder={t("incidents.notes")} value={form.notes} onChange={(e) => setForm({ ...form, notes: e.target.value })} className={`${inputCls} mt-3 w-full`} rows={2} />
          <div className="mt-3">
            <ServerPicker value={form.server_ids} onChange={(v) => setForm({ ...form, server_ids: v })} servers={servers} />
          </div>
          <div className="mt-3 flex gap-2">
            <button onClick={() => save.mutate(form)} disabled={save.isPending} className={btnCls}>{t("common.save")}</button>
            <button onClick={() => setForm(null)} className={ghostCls}>{t("common.cancel")}</button>
          </div>
        </div>
      )}

      {incidents.length === 0 ? (
        <p className="px-4 py-10 text-center text-sm text-muted">{t("incidents.empty")}</p>
      ) : (
        <ul className="divide-y divide-border">
          {incidents.map((inc) => {
            const tone = severityTone(inc.severity);
            const dur = inc.end_at ? Math.max(0, (new Date(inc.end_at).getTime() - new Date(inc.start_at).getTime()) / 60000) : null;
            return (
              <li key={inc.id} className="flex items-start gap-3 px-4 py-3">
                <span className={`mt-1.5 h-2.5 w-2.5 shrink-0 rounded-full ${tone === "err" ? "bg-err" : tone === "warn" ? "bg-warn" : "bg-ok"}`} />
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="font-medium">{inc.title}</span>
                    <span className={`rounded-full px-2 py-0.5 text-xs ${tone === "err" ? "bg-err/10 text-err" : tone === "warn" ? "bg-warn/10 text-warn" : "bg-ok/10 text-ok"}`}>{severityLabel[inc.severity]}</span>
                    <span className={`rounded-full px-2 py-0.5 text-xs ${inc.status === "ongoing" ? "bg-err/10 text-err" : "bg-ok/10 text-ok"}`}>
                      {inc.status === "ongoing" ? t("incidents.ongoing") : t("incidents.resolved")}
                    </span>
                    {inc.server_ids === "" && <span className="text-xs text-muted">{t("incidents.affectsAll")}</span>}
                  </div>
                  <div className="mt-0.5 text-xs text-muted">
                    {fmtDateTime(inc.start_at)} — {inc.end_at ? fmtDateTime(inc.end_at) : t("incidents.ongoing")}
                    {dur !== null && <> · {fmtDuration(Math.round(dur * 60))}</>}
                  </div>
                  {inc.notes && <p className="mt-1 break-all text-xs text-muted">{inc.notes}</p>}
                </div>
                <div className="flex shrink-0 gap-1">
                  {inc.status === "ongoing" && (
                    <button onClick={() => resolve.mutate(inc.id)} className="rounded-lg p-1.5 text-ok hover:bg-ok/10" title={t("incidents.resolve")}>
                      <CheckCircle2 className="h-4 w-4" />
                    </button>
                  )}
                  <button
                    onClick={() => setForm({ ...emptyIncident, ...inc, start_at: toLocalInput(inc.start_at), end_at: toLocalInput(inc.end_at) })}
                    className="rounded-lg p-1.5 text-muted hover:bg-black/5 dark:hover:bg-white/5"
                    title={t("common.edit")}
                  >
                    <Pencil className="h-4 w-4" />
                  </button>
                  <button
                    onClick={() => { if (window.confirm(t("incidents.deleteConfirm"))) del.mutate(inc.id); }}
                    className="rounded-lg p-1.5 text-err hover:bg-err/10"
                    title={t("common.delete")}
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
