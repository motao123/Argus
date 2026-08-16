import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { BellRing, Plus, Trash2 } from "lucide-react";
import { api, type Notification, type NotificationGroup } from "../lib/api";
import { useI18n } from "../lib/i18n";

export default function Notifications() {
  const { t, tErr } = useI18n();
  const qc = useQueryClient();
  const { data: notifData } = useQuery({ queryKey: ["notifications"], queryFn: api.notifications });
  const { data: groupData } = useQuery({ queryKey: ["notification-groups"], queryFn: api.notificationGroups });
  const { data: offline } = useQuery({ queryKey: ["offline-notify"], queryFn: api.offlineNotify });
  const { data: traffic } = useQuery({ queryKey: ["traffic-report"], queryFn: api.trafficReport });
  const notifications = notifData?.notifications ?? [];
  const groups = groupData?.groups ?? [];

  const [offlineForm, setOfflineForm] = useState<{ webhook_id: number; offline_after: number; enabled: boolean } | null>(null);
  const [trafficForm, setTrafficForm] = useState<{ webhook_id: number; hour: number; enabled: boolean } | null>(null);
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
  const createGroup = useMutation({
    mutationFn: async (name: string) => { await api.saveNotificationGroup({ name }); return { ok: true }; },
    onSuccess: () => { setNewGroup(""); qc.invalidateQueries({ queryKey: ["notification-groups"] }); },
    onError: (e) => setMsg(tErr(e)),
  });
  const deleteGroup = useMutation({
    mutationFn: api.deleteNotificationGroup,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["notification-groups"] }),
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
              {traffic?.enabled ? t("notifications.trafficSummary", { hour: traffic.hour, channel: channelRef(traffic.webhook_id) }) : t("notifications.notEnabled")}
            </span>
            <button
              onClick={() => setTrafficForm({ webhook_id: traffic?.webhook_id ?? 0, hour: traffic?.hour ?? 9, enabled: traffic?.enabled ?? true })}
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
    </div>
  );
}
