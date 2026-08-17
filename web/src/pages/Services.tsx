import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Pencil, Plus, Trash2 } from "lucide-react";
import {
  CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis,
} from "recharts";
import { api, type HTTPMethod, type ServiceHistoryPoint, type ServiceItem } from "../lib/api";
import { useServers } from "../context/servers";
import { useI18n } from "../lib/i18n";

// 请求头 JSON（[{"key","value"}]）与「每行 Key: Value」文本互转。
function headersToLines(raw: string | undefined): string {
  if (!raw) return "";
  try {
    const arr = JSON.parse(raw) as { key: string; value: string }[];
    return arr.map((h) => (h.value === "" ? h.key : `${h.key}: ${h.value}`)).join("\n");
  } catch {
    return "";
  }
}

function linesToHeaders(text: string): string {
  const out: { key: string; value: string }[] = [];
  for (const line of text.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    const idx = trimmed.indexOf(":");
    if (idx <= 0) continue; // 无冒号或空 key 的行忽略
    out.push({ key: trimmed.slice(0, idx).trim(), value: trimmed.slice(idx + 1).trim() });
  }
  return out.length ? JSON.stringify(out) : "";
}

// 仅 POST/PUT/PATCH 允许携带请求体。
const bodyMethods: HTTPMethod[] = ["POST", "PUT", "PATCH"];
const httpMethods: HTTPMethod[] = ["GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"];

// 延迟趋势折线图（1d 分钟级延迟，借鉴 dash-v2 ServiceTracker）
function DelayTrend({ svcId, name }: { svcId: number; name: string }) {
  const { t, fmtTime, fmtDateTime } = useI18n();
  const { data } = useQuery({
    queryKey: ["svc-history", svcId, "1d"],
    queryFn: () => api.serviceHistory(svcId, "1d"),
    staleTime: 2 * 60 * 1000,
  });
  const points = data?.points ?? [];
  if (points.length === 0) return <div className="py-4 text-center text-xs text-muted">{t("services.noDelayData")}</div>;
  return (
    <div className="mt-3 h-32">
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={points} margin={{ top: 5, right: 5, bottom: 0, left: -15 }}>
          <CartesianGrid strokeDasharray="3 3" stroke="var(--border-c)" />
          <XAxis dataKey="ts" tickFormatter={(v: number) => fmtTime(v)} tick={{ fontSize: 10, fill: "var(--muted)" }} />
          <YAxis tick={{ fontSize: 10, fill: "var(--muted)" }} width={50} />
          <Tooltip
            labelFormatter={(v: number) => fmtDateTime(v)}
            formatter={(v) => [t("services.tooltipRate", { delay: Number(v).toFixed(1), rate: points.find((p: ServiceHistoryPoint) => p.ts === v)?.up_rate ?? "" }), name]}
            contentStyle={{ background: "var(--panel)", border: "1px solid var(--border-c)", borderRadius: 8 }}
          />
          <Line type="monotone" dataKey="delay" stroke="var(--color-accent)" strokeWidth={1.5} dot={false} isAnimationActive={false} />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}

// 最近 30 天可用率色块（真实数据，借鉴 dash-v2 ServiceTracker）
function UptimeBlocks({ svcId }: { svcId: number }) {
  const { t } = useI18n();
  const { data } = useQuery({
    queryKey: ["svc-history", svcId, "30d"],
    queryFn: () => api.serviceHistory(svcId, "30d"),
    staleTime: 5 * 60 * 1000,
  });
  const points = data?.points ?? [];
  // 按天聚合（6h 步长 → 每天 4 块）
  const byDay = new Map<number, { up: number; total: number }>();
  for (const p of points) {
    const day = Math.floor(p.ts / 86400);
    const e = byDay.get(day) ?? { up: 0, total: 0 };
    e.total += 1;
    if (p.up_rate >= 90) e.up += 1;
    byDay.set(day, e);
  }
  const days = Array.from(byDay.values()).slice(-30);
  return (
    <div className="flex gap-[2px]" title={t("services.uptime30d")}>
      {days.map((d, i) => {
        const rate = d.total ? (d.up / d.total) * 100 : 0;
        return (
          <span
            key={i}
            className={`h-3.5 w-1.5 rounded-sm ${
              rate >= 99 ? "bg-ok/80" : rate >= 90 ? "bg-warn/80" : "bg-err/80"
            }`}
          />
        );
      })}
    </div>
  );
}

const typeLabels: Record<string, string> = { http: "HTTP", tcp: "TCP", ping: "Ping" };

export default function Services() {
  const { t } = useI18n();
  const qc = useQueryClient();
  const { servers } = useServers();
  const { data } = useQuery({ queryKey: ["services"], queryFn: api.services, refetchInterval: 10000 });
  const { data: notificationData } = useQuery({ queryKey: ["notifications"], queryFn: api.notifications });
  const { data: groupData } = useQuery({ queryKey: ["notification-groups"], queryFn: api.notificationGroups });
  const { data: cronData } = useQuery({ queryKey: ["crons"], queryFn: api.crons });
  const services = data?.services ?? [];
  const notifications = notificationData?.notifications ?? [];
  const notificationGroups = groupData?.groups ?? [];
  const crons = cronData?.crons ?? [];

  const [form, setForm] = useState<Partial<ServiceItem> | null>(null);
  const [error, setError] = useState("");
  const [expanded, setExpanded] = useState<number | null>(null);
  const [headerLines, setHeaderLines] = useState("");

  const invalidate = () => qc.invalidateQueries({ queryKey: ["services"] });

  const save = useMutation({
    mutationFn: async (svc: Partial<ServiceItem>): Promise<{ ok: boolean }> => {
      if (svc.id) {
        await api.saveService(svc);
        return { ok: true };
      }
      await api.saveService(svc);
      return { ok: true };
    },
    onSuccess: () => {
      setForm(null);
      invalidate();
    },
    onError: (e) => setError((e as Error).message),
  });
  const remove = useMutation({ mutationFn: api.deleteService, onSuccess: invalidate });

  return (
    <div>
      <div className="mb-5 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">{t("services.title")}</h1>
          <p className="text-sm text-muted">{t("services.subtitle")}</p>
        </div>
        <button
          onClick={() => {
            setHeaderLines("");
            setForm({ server_id: servers[0]?.id ?? 0, name: "", type: "http", target: "", interval: 60, enabled: true, hidden: false, notify: false, http_method: "GET", verify_tls: true, timeout: 10, expected_status_min: 200, expected_status_max: 399, ping_count: 4, cert_warn: true });
          }}
          className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-white hover:opacity-90"
        >
          <Plus className="h-4 w-4" />
          {t("services.newService")}
        </button>
      </div>

      {error && <p className="mb-3 text-sm text-err">{error}</p>}

      {form && (
        <div className="mb-5 rounded-xl border border-border bg-panel p-4">
          <h2 className="mb-3 text-sm font-medium">{t("services.config")}</h2>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-6">
            <select
              value={form.server_id ?? 0}
              onChange={(e) => setForm({ ...form, server_id: Number(e.target.value) })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            >
              <option value={0}>{t("services.pickServer")}</option>
              {servers.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </select>
            <input
              placeholder={t("services.name")}
              value={form.name ?? ""}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            />
            <select
              value={form.type ?? "http"}
              onChange={(e) => setForm({ ...form, type: e.target.value as ServiceItem["type"] })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            >
              <option value="http">HTTP</option>
              <option value="tcp">TCP</option>
              <option value="ping">Ping</option>
            </select>
            <input
              placeholder={t("services.target")}
              value={form.target ?? ""}
              onChange={(e) => setForm({ ...form, target: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none sm:col-span-2"
            />
            <input
              type="number"
              placeholder={t("services.intervalSec")}
              value={form.interval ?? 60}
              onChange={(e) => setForm({ ...form, interval: Number(e.target.value) })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            />
          </div>
          <div className="mt-3 grid grid-cols-2 gap-3 text-sm sm:grid-cols-6">
            {form.type === "http" && <>
              <select value={form.http_method ?? "GET"} onChange={(e) => setForm({ ...form, http_method: e.target.value as HTTPMethod })} className="rounded-lg border border-border bg-bg px-3 py-2">{httpMethods.map(m => <option key={m} value={m}>{m}</option>)}</select>
              <label className="flex items-center gap-2"><input type="checkbox" checked={form.verify_tls !== false} onChange={(e) => setForm({ ...form, verify_tls: e.target.checked })} />{t("services.verifyTls")}</label>
              <input type="number" title={t("services.expectedMin")} value={form.expected_status_min ?? 200} onChange={(e) => setForm({ ...form, expected_status_min: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2" />
              <input type="number" title={t("services.expectedMax")} value={form.expected_status_max ?? 399} onChange={(e) => setForm({ ...form, expected_status_max: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2" />
              <label className="flex items-center gap-2"><input type="checkbox" checked={form.cert_warn ?? true} onChange={(e) => setForm({ ...form, cert_warn: e.target.checked })} />{t("services.certWarn")}</label>
            </>}
            {form.type === "ping" && <input type="number" title={t("services.pingCount")} value={form.ping_count ?? 4} onChange={(e) => setForm({ ...form, ping_count: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2" />}
            <input type="number" title={t("services.timeoutSec")} value={form.timeout ?? 10} onChange={(e) => setForm({ ...form, timeout: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2" />
            <label className="flex items-center gap-2"><input type="checkbox" checked={form.hidden ?? false} onChange={(e) => setForm({ ...form, hidden: e.target.checked })} />{t("common.hidden")}</label>
            <label className="flex items-center gap-2"><input type="checkbox" checked={form.notify ?? false} onChange={(e) => setForm({ ...form, notify: e.target.checked })} />{t("services.notify")}</label>
            <select value={form.notification_group_id ?? 0} onChange={(e) => setForm({ ...form, notification_group_id: Number(e.target.value), notify_webhook_id: 0 })} className="rounded-lg border border-border bg-bg px-3 py-2"><option value={0}>{t("services.notifGroup")}</option>{notificationGroups.map(g => <option key={g.id} value={g.id}>{g.name}</option>)}</select>
            <select value={form.notify_webhook_id ?? 0} onChange={(e) => setForm({ ...form, notify_webhook_id: Number(e.target.value), notification_group_id: 0 })} className="rounded-lg border border-border bg-bg px-3 py-2"><option value={0}>{t("services.webhook")}</option>{notifications.map(n => <option key={n.id} value={n.id}>{n.name}</option>)}</select>
            <select value={form.failure_trigger_cron_id ?? 0} onChange={(e) => setForm({ ...form, failure_trigger_cron_id: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2"><option value={0}>{t("services.failureCron")}</option>{crons.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}</select>
            <select value={form.recovery_trigger_cron_id ?? 0} onChange={(e) => setForm({ ...form, recovery_trigger_cron_id: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2"><option value={0}>{t("services.recoveryCron")}</option>{crons.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}</select>
          </div>
          {form.type === "http" && <div className="mt-3 grid grid-cols-1 gap-3 text-sm sm:grid-cols-3">
            <label className="block">
              <span className="mb-1 block text-xs text-muted">{t("services.requestHeaders")}</span>
              <textarea
                value={headerLines}
                onChange={(e) => setHeaderLines(e.target.value)}
                rows={3}
                placeholder={t("services.requestHeadersPlaceholder")}
                className="w-full rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
              />
            </label>
            <label className="block">
              <span className="mb-1 block text-xs text-muted">{t("services.requestBody")}</span>
              <textarea
                value={form.request_body ?? ""}
                onChange={(e) => setForm({ ...form, request_body: e.target.value })}
                rows={3}
                disabled={!bodyMethods.includes(form.http_method ?? "GET")}
                placeholder={t("services.requestBodyPlaceholder")}
                className="w-full rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none disabled:opacity-40"
              />
            </label>
            <label className="block">
              <span className="mb-1 block text-xs text-muted">{t("services.assertContains")}</span>
              <input
                value={form.assert_contains ?? ""}
                onChange={(e) => setForm({ ...form, assert_contains: e.target.value })}
                placeholder={t("services.assertContainsPlaceholder")}
                className="w-full rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
              />
            </label>
          </div>}
          <div className="mt-3 flex gap-2">
            <button
              onClick={() => save.mutate({ ...form, request_headers: linesToHeaders(headerLines) })}
              disabled={!form.server_id || !form.name || !form.target}
              className="rounded-lg bg-accent px-4 py-1.5 text-sm text-white hover:opacity-90 disabled:opacity-40"
            >
              {t("common.save")}
            </button>
            <button onClick={() => setForm(null)} className="rounded-lg border border-border px-4 py-1.5 text-sm text-muted">
              {t("common.cancel")}
            </button>
          </div>
        </div>
      )}

      <div className="space-y-3">
        {services.map((svc) => (
          <div key={svc.id} className="rounded-xl border border-border bg-panel p-4">
            <button className="flex w-full items-center justify-between text-left" onClick={() => setExpanded(expanded === svc.id ? null : svc.id)}>
              <div className="flex items-center gap-3">
                <span
                  className={`h-2.5 w-2.5 rounded-full ${svc.last_up === null ? "bg-muted" : svc.last_up ? "bg-ok shadow-[0_0_6px] shadow-ok" : "bg-err"}`}
                />
                <span className="font-medium">{svc.name}</span>
                <span className="rounded-full bg-accent/10 px-2 py-0.5 text-xs text-accent">
                  {typeLabels[svc.type] ?? svc.type}
                </span>
                <span className="text-xs text-muted">
                  {svc.target} · {t("services.every", { interval: svc.interval })}
                </span>
              </div>
              <div className="flex items-center gap-3">
                <span className="tabular text-sm text-muted">
                  {svc.last_up === null ? t("common.unknown") : svc.last_up ? t("services.statusOk") : t("services.statusDown")} · {svc.last_delay === null ? "—" : `${svc.last_delay}ms`}
                </span>
                <span className={`tabular rounded-full px-2 py-0.5 text-xs ${(svc.today_up_rate ?? -1) >= 99 ? "bg-ok/15 text-ok" : (svc.today_up_rate ?? -1) >= 90 ? "bg-warn/15 text-warn" : svc.today_up_rate === null ? "bg-black/5 text-muted" : "bg-err/15 text-err"}`}>
                  {t("services.today")} {svc.today_up_rate === null ? "—" : `${svc.today_up_rate.toFixed(1)}%`}
                </span>
                <UptimeBlocks svcId={svc.id} />
                <span className="text-xs text-muted">{expanded === svc.id ? t("services.expandCollapse") : t("services.expandTrend")}</span>
              </div>
            </button>
            <div className="flex justify-end gap-1 pr-1 pt-2">
              <button title={t("common.edit")} onClick={() => { setHeaderLines(headersToLines(svc.request_headers)); setForm({ ...svc }); }} className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5">
                <Pencil className="h-4 w-4" />
              </button>
              <button
                onClick={() => confirm(t("services.deleteConfirm", { name: svc.name })) && remove.mutate(svc.id)}
                className="rounded p-1.5 text-err hover:bg-err/10"
              >
                <Trash2 className="h-4 w-4" />
              </button>
            </div>
            {expanded === svc.id && <div>
              <div className="mt-3 grid grid-cols-2 gap-2 text-xs text-muted sm:grid-cols-6">
                <span>{t("services.availability", { value: svc.availability === null ? "—" : `${svc.availability}%` })}</span>
                <span>{t("services.delay", { value: svc.min_delay === null ? "—" : `${svc.min_delay}/${svc.avg_delay}/${svc.max_delay}ms` })}</span>
                <span>{t("services.packetLoss", { value: svc.loss_rate === null ? "—" : `${svc.loss_rate}%` })}</span>
                <span>{t("services.statusCode", { value: svc.status_code ?? "—" })}</span><span>{t("services.cert", { value: svc.cert_days === null ? "—" : t("services.certDays", { days: svc.cert_days }) })}</span>
                <span>{t("services.phases", { value: [svc.dns_ms, svc.connect_ms, svc.tls_ms, svc.ttfb_ms].map(v => v === null ? "—" : `${v}ms`).join(" / ") })}</span>
              </div>
              {/* 延迟分位数（滑动窗口，样本不足 30 时后端返回 null → 显示 —） */}
              <div className="mt-2 grid grid-cols-2 gap-2 text-xs text-muted sm:grid-cols-5">
                <span>{t("services.latencyP50", { value: svc.delay_p50 === null ? "—" : `${svc.delay_p50}ms` })}</span>
                <span>{t("services.latencyP95", { value: svc.delay_p95 === null ? "—" : `${svc.delay_p95}ms` })}</span>
                <span>{t("services.latencyP99", { value: svc.delay_p99 === null ? "—" : `${svc.delay_p99}ms` })}</span>
                <span>{t("services.stddev", { value: svc.delay_stddev_ms === null ? "—" : `${svc.delay_stddev_ms}ms` })}</span>
                <span>{t("services.jitter", { value: svc.delay_jitter_ms === null ? "—" : `${svc.delay_jitter_ms}ms` })}</span>
              </div><DelayTrend svcId={svc.id} name={svc.name} />
            </div>}
          </div>
        ))}
        {services.length === 0 && (
          <div className="rounded-xl border border-dashed border-border py-16 text-center text-sm text-muted">
            {t("services.noServices")}
          </div>
        )}
      </div>
    </div>
  );
}
