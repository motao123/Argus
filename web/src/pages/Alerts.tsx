import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Pencil, Plus, Send, Trash2 } from "lucide-react";
import { api, type Alert, type Notification } from "../lib/api";

const metrics = [
  { key: "cpu", label: "CPU 使用率 (%)" },
  { key: "mem", label: "内存使用率 (%)" },
  { key: "disk", label: "磁盘使用率 (%)" },
  { key: "net_in_speed", label: "下行速率 (B/s)" },
  { key: "net_out_speed", label: "上行速率 (B/s)" },
  { key: "load1", label: "负载 (1min)" },
  { key: "temperature", label: "温度 (°C)" },
  { key: "gpu", label: "GPU 使用率 (%)" },
  { key: "traffic_in_cycle", label: "本月入向流量 (字节)" },
  { key: "traffic_out_cycle", label: "本月出向流量 (字节)" },
  { key: "offline", label: "离线" },
];

const notifTypes = ["webhook", "bark", "telegram", "email", "serverchan"];

const emptyAlert = {
  name: "", metric: "cpu", min: null as number | null, max: null as number | null, duration: 30,
  notify: true, webhook_id: 0, group_id: 0, trigger_cron_id: 0, trigger_ratio: null as number | null, enabled: true,
};

export default function Alerts() {
  const qc = useQueryClient();
  const { data: alertData } = useQuery({ queryKey: ["alerts"], queryFn: api.alerts });
  const { data: notifData } = useQuery({ queryKey: ["notifications"], queryFn: api.notifications });
  const { data: cronData } = useQuery({ queryKey: ["crons"], queryFn: api.crons });
  const { data: groupData } = useQuery({ queryKey: ["notification-groups"], queryFn: api.notificationGroups });
  const alerts = alertData?.alerts ?? [];
  const notifications = notifData?.notifications ?? [];
  const crons = cronData?.crons ?? [];
  const notifGroups = groupData?.groups ?? [];

  const [form, setForm] = useState<(typeof emptyAlert) & { id?: number } | null>(null);
  const [nForm, setNForm] = useState<Partial<Notification> | null>(null);
  const [error, setError] = useState("");
  const [testResult, setTestResult] = useState("");

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ["alerts"] });
    qc.invalidateQueries({ queryKey: ["notifications"] });
  };

  const saveAlert = useMutation({
    mutationFn: (a: typeof emptyAlert & { id?: number }) => api.saveAlert(a),
    onSuccess: () => {
      setForm(null);
      invalidate();
    },
    onError: (e) => setError((e as Error).message),
  });
  const deleteAlert = useMutation({ mutationFn: api.deleteAlert, onSuccess: invalidate });

  const saveN = useMutation({
    mutationFn: async (n: Partial<Notification>) => {
      // 编辑时仅提交实际改动字段：读取已脱敏（url 掩码/headers/body 空），
      // 掩码 URL 与空凭据字段不回传，避免覆盖原值（对齐 nezha 脱敏规范）
      const payload: Partial<Notification> = { id: n.id };
      if (n.name) payload.name = n.name;
      if (n.type) payload.type = n.type;
      if (n.url && !n.url.endsWith("/***")) payload.url = n.url;
      if (n.method) payload.method = n.method;
      if (n.headers && n.headers !== "{}") payload.headers = n.headers;
      if (n.body && n.body !== "{}") payload.body = n.body;
      if (n.chat_id) payload.chat_id = n.chat_id;
      return api.saveNotification(payload);
    },
    onSuccess: () => {
      setNForm(null);
      invalidate();
    },
    onError: (e) => setError((e as Error).message),
  });
  const deleteN = useMutation({ mutationFn: api.deleteNotification, onSuccess: invalidate });
  const testN = useMutation({
    mutationFn: (id: number) => api.testMessage(id),
    onSuccess: (r) => setTestResult(`已投递至 ${r.sent_to}（异步发送，请查收）`),
    onError: (e) => setTestResult("发送失败: " + (e as Error).message),
  });

  return (
    <div>
      <div className="mb-5 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">报警规则</h1>
          <p className="text-sm text-muted">指标超出阈值并持续设定时长后触发通知</p>
        </div>
        <button
          onClick={() => setForm(emptyAlert)}
          className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-white hover:opacity-90"
        >
          <Plus className="h-4 w-4" />
          新建规则
        </button>
      </div>

      {error && <p className="mb-3 text-sm text-err">{error}</p>}

      {form && (
        <div className="mb-5 rounded-xl border border-border bg-panel p-4">
          <h2 className="mb-3 text-sm font-medium">{form.id ? "编辑规则" : "新建规则"}</h2>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-4">
            <input
              placeholder="规则名称"
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent"
            />
            <select
              value={form.metric}
              onChange={(e) => setForm({ ...form, metric: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            >
              {metrics.map((m) => (
                <option key={m.key} value={m.key}>
                  {m.label}
                </option>
              ))}
            </select>
            <input
              type="number"
              placeholder="下限 (可选)"
              value={form.min ?? ""}
              onChange={(e) => setForm({ ...form, min: e.target.value === "" ? null : Number(e.target.value) })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent"
            />
            <input
              type="number"
              placeholder="上限 (可选)"
              value={form.max ?? ""}
              onChange={(e) => setForm({ ...form, max: e.target.value === "" ? null : Number(e.target.value) })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent"
            />
          </div>
          <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-4">
            <label className="flex items-center gap-2 text-sm">
              持续
              <input
                type="number"
                value={form.duration}
                onChange={(e) => setForm({ ...form, duration: Number(e.target.value) })}
                className="w-20 rounded-lg border border-border bg-bg px-2 py-1.5 text-sm outline-none"
              />
              秒
            </label>
            <select
              value={form.webhook_id}
              onChange={(e) => setForm({ ...form, webhook_id: Number(e.target.value) })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            >
              <option value={0}>不通知</option>
              {notifications.map((n) => (
                <option key={n.id} value={n.id}>
                  {n.name}
                </option>
              ))}
            </select>
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={form.enabled}
                onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
              />
              启用
            </label>
          </div>
          <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-4">
            <select
              value={form.trigger_cron_id}
              onChange={(e) => setForm({ ...form, trigger_cron_id: Number(e.target.value) })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            >
              <option value={0}>触发任务：无</option>
              {crons.map((c) => (
                <option key={c.id} value={c.id}>
                  触发「{c.name}」
                </option>
              ))}
            </select>
            <input
              type="number"
              placeholder="采样达标比例 % (1-100，留空=全部采样)"
              value={form.trigger_ratio ?? ""}
              onChange={(e) => setForm({ ...form, trigger_ratio: e.target.value === "" ? null : Number(e.target.value) })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            />
            <select
              value={form.group_id}
              onChange={(e) => setForm({ ...form, group_id: Number(e.target.value) })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            >
              <option value={0}>通知分组：无</option>
              {notifGroups.map((g) => (
                <option key={g.id} value={g.id}>
                  {g.name}
                </option>
              ))}
            </select>
          </div>
          <div className="mt-3 flex gap-2">
            <button
              onClick={() => saveAlert.mutate(form)}
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

      <div className="overflow-hidden rounded-xl border border-border bg-panel">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted">
              <th className="px-4 py-3 font-normal">名称</th>
              <th className="px-4 py-3 font-normal">指标</th>
              <th className="px-4 py-3 font-normal">阈值</th>
              <th className="px-4 py-3 font-normal">持续</th>
              <th className="px-4 py-3 font-normal">通知</th>
              <th className="px-4 py-3 font-normal">状态</th>
              <th className="px-4 py-3 text-right font-normal">操作</th>
            </tr>
          </thead>
          <tbody>
            {alerts.map((a: Alert) => (
              <tr key={a.id} className="border-b border-border last:border-0 hover:bg-black/2 dark:hover:bg-white/2">
                <td className="px-4 py-3 font-medium">{a.name}</td>
                <td className="px-4 py-3">{metrics.find((m) => m.key === a.metric)?.label ?? a.metric}</td>
                <td className="px-4 py-3 tabular">
                  {a.metric === "offline" ? "离线即触发" : `${a.min ?? "—"} ~ ${a.max ?? "—"}`}
                </td>
                <td className="px-4 py-3 tabular">{a.duration}s</td>
                <td className="px-4 py-3">{a.notify ? notifications.find((n) => n.id === a.webhook_id)?.name ?? `#${a.webhook_id}` : "关闭"}</td>
                <td className="px-4 py-3">
                  <span className={`rounded-full px-2 py-0.5 text-xs ${a.enabled ? "bg-ok/15 text-ok" : "bg-muted/20 text-muted"}`}>
                    {a.enabled ? "启用" : "停用"}
                  </span>
                </td>
                <td className="px-4 py-3">
                  <div className="flex justify-end gap-1">
                    <button
                      onClick={() => setForm({ ...emptyAlert, ...a, id: a.id })}
                      className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5"
                    >
                      <Pencil className="h-4 w-4" />
                    </button>
                    <button
                      onClick={() => confirm(`删除规则「${a.name}」？`) && deleteAlert.mutate(a.id)}
                      className="rounded p-1.5 text-err hover:bg-err/10"
                    >
                      <Trash2 className="h-4 w-4" />
                    </button>
                  </div>
                </td>
              </tr>
            ))}
            {alerts.length === 0 && (
              <tr>
                <td colSpan={7} className="px-4 py-10 text-center text-muted">
                  暂无报警规则
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {/* 通知渠道 */}
      <div className="mb-3 mt-8 flex items-center justify-between">
        <h2 className="text-lg font-semibold">通知渠道</h2>
        <button
          onClick={() => setNForm({ name: "", type: "webhook", url: "", method: "POST", headers: "{}", body: '{"title":"{{title}}","content":"{{content}}"}', chat_id: "" })}
          className="flex items-center gap-2 rounded-lg border border-border px-3 py-1.5 text-sm hover:bg-black/5 dark:hover:bg-white/5"
        >
          <Plus className="h-4 w-4" />
          添加
        </button>
      </div>

      {nForm && (
        <div className="mb-5 rounded-xl border border-border bg-panel p-4">
          <h3 className="mb-3 text-sm font-medium">通知渠道（{nForm.type ?? "webhook"}）</h3>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <input
              placeholder="名称"
              value={nForm.name ?? ""}
              onChange={(e) => setNForm({ ...nForm, name: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            />
            <select
              value={nForm.type ?? "webhook"}
              onChange={(e) => setNForm({ ...nForm, type: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            >
              {notifTypes.map((t) => (
                <option key={t} value={t}>{t}</option>
              ))}
            </select>
            <input
              placeholder="URL（webhook/bark/serverchan）"
              value={nForm.url ?? ""}
              onChange={(e) => setNForm({ ...nForm, url: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            />
            <input
              placeholder="目标（telegram chat_id / email 收件人）"
              value={nForm.chat_id ?? ""}
              onChange={(e) => setNForm({ ...nForm, chat_id: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            />
            <input
              placeholder='请求头 JSON，如 {"X-Token":"abc"}'
              value={nForm.headers ?? ""}
              onChange={(e) => setNForm({ ...nForm, headers: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none sm:col-span-2"
            />
            <textarea
              placeholder='Body 模板，支持 {{title}} / {{content}}'
              value={nForm.body ?? ""}
              onChange={(e) => setNForm({ ...nForm, body: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none sm:col-span-2"
              rows={2}
            />
          </div>
          <div className="mt-3 flex gap-2">
            <button
              onClick={() => saveN.mutate(nForm)}
              className="rounded-lg bg-accent px-4 py-1.5 text-sm text-white hover:opacity-90"
            >
              保存
            </button>
            <button onClick={() => setNForm(null)} className="rounded-lg border border-border px-4 py-1.5 text-sm text-muted">
              取消
            </button>
          </div>
        </div>
      )}
      {testResult && <p className="mb-3 text-sm text-muted">{testResult}</p>}

      <div className="overflow-hidden rounded-xl border border-border bg-panel">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted">
              <th className="px-4 py-3 font-normal">名称</th>
              <th className="px-4 py-3 font-normal">类型</th>
              <th className="px-4 py-3 font-normal">URL</th>
              <th className="px-4 py-3 text-right font-normal">操作</th>
            </tr>
          </thead>
          <tbody>
            {notifications.map((n) => (
              <tr key={n.id} className="border-b border-border last:border-0">
                <td className="px-4 py-3 font-medium">{n.name}</td>
                <td className="px-4 py-3 text-xs text-muted">{n.type}</td>
                <td className="max-w-md truncate px-4 py-3 text-muted">{n.url}</td>
                <td className="px-4 py-3 text-right">
                  <button
                    onClick={() => testN.mutate(n.id)}
                    title="发送测试消息"
                    className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5"
                  >
                    <Send className="h-4 w-4" />
                  </button>
                  <button
                    onClick={() => setNForm(n)}
                    className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5"
                  >
                    <Pencil className="h-4 w-4" />
                  </button>
                  <button
                    onClick={() => confirm(`删除渠道「${n.name}」？`) && deleteN.mutate(n.id)}
                    className="rounded p-1.5 text-err hover:bg-err/10"
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                </td>
              </tr>
            ))}
            {notifications.length === 0 && (
              <tr>
                <td colSpan={3} className="px-4 py-8 text-center text-muted">
                  暂无通知渠道
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
