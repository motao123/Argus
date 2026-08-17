import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { BellRing, Plus, RotateCw, Trash2 } from "lucide-react";
import { api, type Notification, type NotificationDelivery, type NotificationGroup } from "../lib/api";
import { useI18n, type TKey } from "../lib/i18n";

const deliveryStatusClass: Record<string, string> = {
  pending: "bg-accent/15 text-accent",
  sent: "bg-ok/15 text-ok",
  failed: "bg-err/15 text-err",
};

const deliveryStatusLabel: Record<string, TKey> = {
  pending: "notifications.deliveryStatusPending",
  sent: "notifications.deliveryStatusSent",
  failed: "notifications.deliveryStatusFailed",
};

type TrafficReportCfg = { webhook_id: number; period: "daily" | "weekly" | "monthly"; hour: number; weekday: number; day: number; enabled: boolean };

const weekdayLabels: Record<number, TKey> = {
  0: "notifications.weekdaySun",
  1: "notifications.weekdayMon",
  2: "notifications.weekdayTue",
  3: "notifications.weekdayWed",
  4: "notifications.weekdayThu",
  5: "notifications.weekdayFri",
  6: "notifications.weekdaySat",
};

export default function Notifications() {
  const { t, tErr, fmtDateTime } = useI18n();
  const qc = useQueryClient();
  const { data: notifData } = useQuery({ queryKey: ["notifications"], queryFn: api.notifications });
  const { data: groupData } = useQuery({ queryKey: ["notification-groups"], queryFn: api.notificationGroups });
  const { data: offline } = useQuery({ queryKey: ["offline-notify"], queryFn: api.offlineNotify });
  const { data: traffic } = useQuery({ queryKey: ["traffic-report"], queryFn: api.trafficReport });
  const { data: settingsData } = useQuery({ queryKey: ["settings"], queryFn: api.settings });
  const { data: deliveryData } = useQuery({ queryKey: ["deliveries"], queryFn: () => api.deliveries(0, 50) });
  const notifications = notifData?.notifications ?? [];
  const groups = groupData?.groups ?? [];
  const deliveries = deliveryData?.deliveries ?? [];
  const settings = settingsData?.settings ?? {};

  const [offlineForm, setOfflineForm] = useState<{ webhook_id: number; offline_after: number; enabled: boolean } | null>(null);
  const [trafficForm, setTrafficForm] = useState<TrafficReportCfg | null>(null);
  const [expireDays, setExpireDays] = useState<string | null>(null);
  const [newGroup, setNewGroup] = useState("");
  const [msg, setMsg] = useState("");

  const saveOffline = useMutation({
    mutationFn: api.saveOfflineNotify,
    onSuccess: () => { setMsg(t("notifications.offlineSaved")); qc.invalidateQueries({ queryKey: ["offline-notify"] }); },
    onError: (e) => setMsg(tErr(e)),
  });
  const saveTraffic = useMutation({
    mutationFn: api.saveTrafficReport,
    onSuccess: () => { setMsg(t("notifications.trafficSaved")); qc.invalidateQueries({ queryKey: ["traffic-report"] }); },
    onError: (e) => setMsg(tErr(e)),
  });
  const saveExpire = useMutation({
    mutationFn: (days: string) => api.saveSettings({ expire_notify_days: days }),
    onSuccess: () => { setMsg(t("notifications.expireSaved")); setExpireDays(null); qc.invalidateQueries({ queryKey: ["settings"] }); },
    onError: (e) => setMsg(tErr(e)),
  });
  const createGroup = useMutation({
    mutationFn: async (name: string) => { await api.saveNotificationGroup({ name }); return { ok: true }; },
    onSuccess: () => { setNewGroup(""); qc.invalidateQueries({ queryKey: ["notification-groups"] }); },
    onError: (e) => setMsg(tErr(e)),
  });
  const deleteGroup = useMutation({
    mutationFn: api.deleteNotificationGroup,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["notification-groups"] }),
  });
  const retryDelivery = useMutation({
    mutationFn: api.retryDelivery,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["deliveries"] }),
    onError: (e) => setMsg(tErr(e)),
  });

  const webhookSelect = (id: number, set: (v: number) => void) => (
    <select value={id} onChange={(e) => set(Number(e.target.value))} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm">
      <option value={0}>{t("notifications.noNotify")}</option>
      {notifications.map((n: Notification) => (
        <option key={n.id} value={n.id}>{n.name}</option>
      ))}
    </select>
  );

  const channelRef = (id: number) => (id ? t("notifications.chanId", { id }) : t("notifications.notSet"));
  const channelName = (id: number) => notifications.find((n) => n.id === id)?.name ?? channelRef(id);

  return (
    <div className="space-y-8">
      <div>
        <h1 className="mb-1 flex items-center gap-2 text-xl font-semibold">
          <BellRing className="h-5 w-5 text-accent" /> {t("notifications.title")}
        </h1>
        <p className="mb-4 text-sm text-muted">{t("notifications.subtitle")}</p>
        {msg && <p className="mb-3 text-sm text-ok">{msg}</p>}
      </div>

      {/* 离线/上线通知 */}
      <section className="rounded-xl border border-border bg-panel p-4">
        <h2 className="mb-3 text-sm font-medium">{t("notifications.offlineTitle")}</h2>
        {offlineForm ? (
          <div className="flex flex-wrap items-end gap-3">
            <div>
              <div className="mb-1 text-xs text-muted">{t("notifications.channel")}</div>
              {webhookSelect(offlineForm.webhook_id, (v) => setOfflineForm({ ...offlineForm, webhook_id: v }))}
            </div>
            <div>
              <div className="mb-1 text-xs text-muted">{t("notifications.offlineAfter")}</div>
              <input
                type="number"
                value={offlineForm.offline_after}
                onChange={(e) => setOfflineForm({ ...offlineForm, offline_after: Number(e.target.value) })}
                className="w-28 rounded-lg border border-border bg-bg px-3 py-2 text-sm"
              />
            </div>
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={offlineForm.enabled} onChange={(e) => setOfflineForm({ ...offlineForm, enabled: e.target.checked })} />
              {t("common.enabled")}
            </label>
            <button onClick={() => saveOffline.mutate(offlineForm)} className="rounded-lg bg-accent px-4 py-2 text-sm text-white">{t("common.save")}</button>
            <button onClick={() => setOfflineForm(null)} className="rounded-lg border border-border px-4 py-2 text-sm">{t("common.cancel")}</button>
          </div>
        ) : (
          <div className="flex items-center justify-between text-sm">
            <span className="text-muted">
              {offline?.enabled ? t("notifications.offlineSummary", { seconds: offline.offline_after, channel: channelRef(offline.webhook_id) }) : t("notifications.notEnabled")}
            </span>
            <button
              onClick={() => setOfflineForm({ webhook_id: offline?.webhook_id ?? 0, offline_after: offline?.offline_after ?? 60, enabled: offline?.enabled ?? true })}
              className="rounded-lg border border-border px-3 py-1.5 text-sm"
            >
              {t("notifications.configure")}
            </button>
          </div>
        )}
      </section>

      {/* 流量定时报告 */}
      <section className="rounded-xl border border-border bg-panel p-4">
        <h2 className="mb-3 text-sm font-medium">{t("notifications.trafficTitle")}</h2>
        {trafficForm ? (
          <div className="flex flex-wrap items-end gap-3">
            <div>
              <div className="mb-1 text-xs text-muted">{t("notifications.channel")}</div>
              {webhookSelect(trafficForm.webhook_id, (v) => setTrafficForm({ ...trafficForm, webhook_id: v }))}
            </div>
            <div>
              <div className="mb-1 text-xs text-muted">{t("notifications.trafficPeriod")}</div>
              <select
                value={trafficForm.period}
                onChange={(e) => setTrafficForm({ ...trafficForm, period: e.target.value as TrafficReportCfg["period"] })}
                className="rounded-lg border border-border bg-bg px-3 py-2 text-sm"
              >
                <option value="daily">{t("notifications.periodDaily")}</option>
                <option value="weekly">{t("notifications.periodWeekly")}</option>
                <option value="monthly">{t("notifications.periodMonthly")}</option>
              </select>
            </div>
            {trafficForm.period === "weekly" && (
              <div>
                <div className="mb-1 text-xs text-muted">{t("notifications.sendWeekday")}</div>
                <select
                  value={trafficForm.weekday}
                  onChange={(e) => setTrafficForm({ ...trafficForm, weekday: Number(e.target.value) })}
                  className="rounded-lg border border-border bg-bg px-3 py-2 text-sm"
                >
                  {Object.entries(weekdayLabels).map(([v, label]) => (
                    <option key={v} value={v}>{t(label)}</option>
                  ))}
                </select>
              </div>
            )}
            {trafficForm.period === "monthly" && (
              <div>
                <div className="mb-1 text-xs text-muted">{t("notifications.sendDay")}</div>
                <input
                  type="number"
                  min={1}
                  max={28}
                  value={trafficForm.day}
                  onChange={(e) => setTrafficForm({ ...trafficForm, day: Number(e.target.value) })}
                  className="w-28 rounded-lg border border-border bg-bg px-3 py-2 text-sm"
                />
              </div>
            )}
            <div>
              <div className="mb-1 text-xs text-muted">{t("notifications.sendHour")}</div>
              <input
                type="number"
                min={0}
                max={23}
                value={trafficForm.hour}
                onChange={(e) => setTrafficForm({ ...trafficForm, hour: Number(e.target.value) })}
                className="w-28 rounded-lg border border-border bg-bg px-3 py-2 text-sm"
              />
            </div>
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={trafficForm.enabled} onChange={(e) => setTrafficForm({ ...trafficForm, enabled: e.target.checked })} />
              {t("common.enabled")}
            </label>
            <button onClick={() => saveTraffic.mutate(trafficForm)} className="rounded-lg bg-accent px-4 py-2 text-sm text-white">{t("common.save")}</button>
            <button onClick={() => setTrafficForm(null)} className="rounded-lg border border-border px-4 py-2 text-sm">{t("common.cancel")}</button>
          </div>
        ) : (
          <div className="flex items-center justify-between text-sm">
            <span className="text-muted">
              {traffic?.enabled ? (
                traffic.period === "weekly"
                  ? t("notifications.trafficSummaryWeekly", { weekday: t(weekdayLabels[traffic.weekday] ?? "notifications.weekdayMon"), hour: traffic.hour, channel: channelRef(traffic.webhook_id) })
                  : traffic.period === "monthly"
                    ? t("notifications.trafficSummaryMonthly", { day: traffic.day, hour: traffic.hour, channel: channelRef(traffic.webhook_id) })
                    : t("notifications.trafficSummary", { hour: traffic.hour, channel: channelRef(traffic.webhook_id) })
              ) : t("notifications.notEnabled")}
            </span>
            <button
              onClick={() => setTrafficForm({
                webhook_id: traffic?.webhook_id ?? 0,
                period: (traffic?.period as TrafficReportCfg["period"]) || "daily",
                hour: traffic?.hour ?? 9,
                weekday: traffic?.weekday ?? 1,
                day: traffic?.day ?? 1,
                enabled: traffic?.enabled ?? true,
              })}
              className="rounded-lg border border-border px-3 py-1.5 text-sm"
            >
              {t("notifications.configure")}
            </button>
          </div>
        )}
      </section>

      {/* 到期提醒 */}
      <section className="rounded-xl border border-border bg-panel p-4">
        <h2 className="mb-3 text-sm font-medium">{t("notifications.expireTitle")}</h2>
        {expireDays !== null ? (
          <div className="flex flex-wrap items-end gap-3">
            <div>
              <div className="mb-1 text-xs text-muted">{t("notifications.expireDays")}</div>
              <input
                type="number"
                min={1}
                max={30}
                value={expireDays}
                onChange={(e) => setExpireDays(e.target.value)}
                className="w-28 rounded-lg border border-border bg-bg px-3 py-2 text-sm"
              />
            </div>
            <button onClick={() => expireDays && saveExpire.mutate(expireDays)} className="rounded-lg bg-accent px-4 py-2 text-sm text-white">{t("common.save")}</button>
            <button onClick={() => setExpireDays(null)} className="rounded-lg border border-border px-4 py-2 text-sm">{t("common.cancel")}</button>
          </div>
        ) : (
          <div className="flex items-center justify-between text-sm">
            <span className="text-muted">{t("notifications.expireSummary", { days: settings.expire_notify_days ?? "3" })}</span>
            <button
              onClick={() => setExpireDays(settings.expire_notify_days ?? "3")}
              className="rounded-lg border border-border px-3 py-1.5 text-sm"
            >
              {t("notifications.configure")}
            </button>
          </div>
        )}
      </section>

      {/* 通知分组 */}
      <section className="rounded-xl border border-border bg-panel p-4">
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-sm font-medium">{t("notifications.groupsTitle")}</h2>
          <div className="flex gap-2">
            <input
              value={newGroup}
              onChange={(e) => setNewGroup(e.target.value)}
              placeholder={t("notifications.newGroup")}
              className="w-40 rounded-lg border border-border bg-bg px-3 py-1.5 text-sm"
            />
            <button onClick={() => newGroup && createGroup.mutate(newGroup)} className="flex items-center gap-1 rounded-lg bg-accent px-3 py-1.5 text-sm text-white">
              <Plus className="h-4 w-4" /> {t("common.create")}
            </button>
          </div>
        </div>
        <ul className="space-y-1">
          {groups.map((g: NotificationGroup) => (
            <li key={g.id} className="flex items-center justify-between rounded-lg border border-border px-3 py-2 text-sm">
              <span>{g.name}</span>
              <span className="text-xs text-muted">{t("notifications.members", { value: g.member_ids || t("notifications.emptyMembers") })}</span>
              <button onClick={() => confirm(t("notifications.confirmDeleteGroup", { name: g.name })) && deleteGroup.mutate(g.id)} className="text-err hover:opacity-70">
                <Trash2 className="h-4 w-4" />
              </button>
            </li>
          ))}
          {groups.length === 0 && <li className="py-3 text-center text-sm text-muted">{t("notifications.noGroups")}</li>}
        </ul>
      </section>

      {/* 送达记录（持久队列 + 重试） */}
      <section className="rounded-xl border border-border bg-panel p-4">
        <div className="mb-1 flex items-center justify-between">
          <h2 className="text-sm font-medium">{t("notifications.deliveryTitle")}</h2>
          <span className="text-xs text-muted">{t("notifications.deliveryHint")}</span>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-muted">
                <th className="px-3 py-2.5 font-normal">{t("notifications.deliveryChannel")}</th>
                <th className="px-3 py-2.5 font-normal">{t("notifications.deliveryTitleCol")}</th>
                <th className="px-3 py-2.5 font-normal">{t("notifications.deliveryStatus")}</th>
                <th className="px-3 py-2.5 font-normal">{t("notifications.deliveryAttempts")}</th>
                <th className="px-3 py-2.5 font-normal">{t("notifications.deliveryNextRetry")}</th>
                <th className="px-3 py-2.5 font-normal">{t("notifications.deliverySentAt")}</th>
                <th className="px-3 py-2.5 font-normal">{t("notifications.deliveryError")}</th>
                <th className="px-3 py-2.5 text-right font-normal">{t("alerts.actions")}</th>
              </tr>
            </thead>
            <tbody>
              {deliveries.map((d: NotificationDelivery) => (
                <tr key={d.id} className="border-b border-border last:border-0 hover:bg-black/2 dark:hover:bg-white/2">
                  <td className="max-w-36 truncate px-3 py-2.5">{channelName(d.webhook_id)}</td>
                  <td className="max-w-56 truncate px-3 py-2.5" title={d.title}>{d.title || "—"}</td>
                  <td className="px-3 py-2.5">
                    <span className={`rounded-full px-2 py-0.5 text-xs ${deliveryStatusClass[d.status] ?? "bg-muted/20 text-muted"}`}>
                      {t(deliveryStatusLabel[d.status] ?? "notifications.deliveryStatusPending")}
                    </span>
                  </td>
                  <td className="px-3 py-2.5 tabular text-muted">{d.attempts}/{d.max_attempts}</td>
                  <td className="px-3 py-2.5 tabular text-xs text-muted">{d.next_retry ? fmtDateTime(d.next_retry) : "—"}</td>
                  <td className="px-3 py-2.5 tabular text-xs text-muted">{d.sent_at ? fmtDateTime(d.sent_at) : "—"}</td>
                  <td className="max-w-48 truncate px-3 py-2.5 text-xs text-err" title={d.last_error}>{d.last_error || "—"}</td>
                  <td className="px-3 py-2.5 text-right">
                    {d.status === "failed" && (
                      <button
                        onClick={() => retryDelivery.mutate(d.id)}
                        title={t("notifications.deliveryRetryTitle")}
                        className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5"
                      >
                        <RotateCw className="h-4 w-4" />
                      </button>
                    )}
                  </td>
                </tr>
              ))}
              {deliveries.length === 0 && (
                <tr>
                  <td colSpan={8} className="px-4 py-8 text-center text-muted">
                    {t("notifications.noDeliveries")}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}
