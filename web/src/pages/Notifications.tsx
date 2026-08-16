import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { BellRing, Plus, Trash2 } from "lucide-react";
import { api, type Notification, type NotificationGroup } from "../lib/api";

export default function Notifications() {
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
    onSuccess: () => { setMsg("离线通知配置已保存"); qc.invalidateQueries({ queryKey: ["offline-notify"] }); },
    onError: (e) => setMsg((e as Error).message),
  });
  const saveTraffic = useMutation({
    mutationFn: api.saveTrafficReport,
    onSuccess: () => { setMsg("流量报告配置已保存"); qc.invalidateQueries({ queryKey: ["traffic-report"] }); },
    onError: (e) => setMsg((e as Error).message),
  });
  const createGroup = useMutation({
    mutationFn: async (name: string) => { await api.saveNotificationGroup({ name }); return { ok: true }; },
    onSuccess: () => { setNewGroup(""); qc.invalidateQueries({ queryKey: ["notification-groups"] }); },
    onError: (e) => setMsg((e as Error).message),
  });
  const deleteGroup = useMutation({
    mutationFn: api.deleteNotificationGroup,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["notification-groups"] }),
  });

  const webhookSelect = (id: number, set: (v: number) => void) => (
    <select value={id} onChange={(e) => set(Number(e.target.value))} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm">
      <option value={0}>不通知</option>
      {notifications.map((n: Notification) => (
        <option key={n.id} value={n.id}>{n.name}</option>
      ))}
    </select>
  );

  return (
    <div className="space-y-8">
      <div>
        <h1 className="mb-1 flex items-center gap-2 text-xl font-semibold">
          <BellRing className="h-5 w-5 text-accent" /> 通知中心
        </h1>
        <p className="mb-4 text-sm text-muted">通知渠道、通知分组、离线/上线通知与流量定时报告</p>
        {msg && <p className="mb-3 text-sm text-ok">{msg}</p>}
      </div>

      {/* 离线/上线通知 */}
      <section className="rounded-xl border border-border bg-panel p-4">
        <h2 className="mb-3 text-sm font-medium">离线/上线通知</h2>
        {offlineForm ? (
          <div className="flex flex-wrap items-end gap-3">
            <div>
              <div className="mb-1 text-xs text-muted">通知渠道</div>
              {webhookSelect(offlineForm.webhook_id, (v) => setOfflineForm({ ...offlineForm, webhook_id: v }))}
            </div>
            <div>
              <div className="mb-1 text-xs text-muted">离线判定（秒）</div>
              <input
                type="number"
                value={offlineForm.offline_after}
                onChange={(e) => setOfflineForm({ ...offlineForm, offline_after: Number(e.target.value) })}
                className="w-28 rounded-lg border border-border bg-bg px-3 py-2 text-sm"
              />
            </div>
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={offlineForm.enabled} onChange={(e) => setOfflineForm({ ...offlineForm, enabled: e.target.checked })} />
              启用
            </label>
            <button onClick={() => saveOffline.mutate(offlineForm)} className="rounded-lg bg-accent px-4 py-2 text-sm text-white">保存</button>
            <button onClick={() => setOfflineForm(null)} className="rounded-lg border border-border px-4 py-2 text-sm">取消</button>
          </div>
        ) : (
          <div className="flex items-center justify-between text-sm">
            <span className="text-muted">
              {offline?.enabled ? `已启用 · ${offline.offline_after}s 判离线 · 通知 ${offline.webhook_id ? `#${offline.webhook_id}` : "未设置"}` : "未启用"}
            </span>
            <button
              onClick={() => setOfflineForm({ webhook_id: offline?.webhook_id ?? 0, offline_after: offline?.offline_after ?? 60, enabled: offline?.enabled ?? true })}
              className="rounded-lg border border-border px-3 py-1.5 text-sm"
            >
              配置
            </button>
          </div>
        )}
      </section>

      {/* 流量定时报告 */}
      <section className="rounded-xl border border-border bg-panel p-4">
        <h2 className="mb-3 text-sm font-medium">流量定时报告</h2>
        {trafficForm ? (
          <div className="flex flex-wrap items-end gap-3">
            <div>
              <div className="mb-1 text-xs text-muted">通知渠道</div>
              {webhookSelect(trafficForm.webhook_id, (v) => setTrafficForm({ ...trafficForm, webhook_id: v }))}
            </div>
            <div>
              <div className="mb-1 text-xs text-muted">发送时间（小时 0-23）</div>
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
              启用
            </label>
            <button onClick={() => saveTraffic.mutate(trafficForm)} className="rounded-lg bg-accent px-4 py-2 text-sm text-white">保存</button>
            <button onClick={() => setTrafficForm(null)} className="rounded-lg border border-border px-4 py-2 text-sm">取消</button>
          </div>
        ) : (
          <div className="flex items-center justify-between text-sm">
            <span className="text-muted">
              {traffic?.enabled ? `已启用 · 每日 ${traffic.hour}:00 发送 · 通知 ${traffic.webhook_id ? `#${traffic.webhook_id}` : "未设置"}` : "未启用"}
            </span>
            <button
              onClick={() => setTrafficForm({ webhook_id: traffic?.webhook_id ?? 0, hour: traffic?.hour ?? 9, enabled: traffic?.enabled ?? true })}
              className="rounded-lg border border-border px-3 py-1.5 text-sm"
            >
              配置
            </button>
          </div>
        )}
      </section>

      {/* 通知分组 */}
      <section className="rounded-xl border border-border bg-panel p-4">
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-sm font-medium">通知分组</h2>
          <div className="flex gap-2">
            <input
              value={newGroup}
              onChange={(e) => setNewGroup(e.target.value)}
              placeholder="新分组名称"
              className="w-40 rounded-lg border border-border bg-bg px-3 py-1.5 text-sm"
            />
            <button onClick={() => newGroup && createGroup.mutate(newGroup)} className="flex items-center gap-1 rounded-lg bg-accent px-3 py-1.5 text-sm text-white">
              <Plus className="h-4 w-4" /> 创建
            </button>
          </div>
        </div>
        <ul className="space-y-1">
          {groups.map((g: NotificationGroup) => (
            <li key={g.id} className="flex items-center justify-between rounded-lg border border-border px-3 py-2 text-sm">
              <span>{g.name}</span>
              <span className="text-xs text-muted">成员: {g.member_ids || "空"}</span>
              <button onClick={() => confirm(`删除分组「${g.name}」？`) && deleteGroup.mutate(g.id)} className="text-err hover:opacity-70">
                <Trash2 className="h-4 w-4" />
              </button>
            </li>
          ))}
          {groups.length === 0 && <li className="py-3 text-center text-sm text-muted">暂无分组</li>}
        </ul>
      </section>
    </div>
  );
}
