import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { BellOff, CheckCheck, Pencil, Plus, Send, Trash2, Undo2 } from "lucide-react";
import { api, type Alert, type Notification } from "../lib/api";
import { useI18n, type TKey } from "../lib/i18n";

const metrics: { key: string; label: TKey }[] = [
  { key: "cpu", label: "alerts.metricCpu" },
  { key: "mem", label: "alerts.metricMem" },
  { key: "disk", label: "alerts.metricDisk" },
  { key: "net_in_speed", label: "alerts.metricNetIn" },
  { key: "net_out_speed", label: "alerts.metricNetOut" },
  { key: "load1", label: "alerts.metricLoad" },
  { key: "temperature", label: "alerts.metricTemp" },
  { key: "gpu", label: "alerts.metricGpu" },
  { key: "traffic_in_cycle", label: "alerts.metricTrafficIn" },
  { key: "traffic_out_cycle", label: "alerts.metricTrafficOut" },
  { key: "offline", label: "alerts.metricOffline" },
];

const notifTypes = ["webhook", "bark", "telegram", "email", "serverchan", "javascript", "dingtalk", "wecom", "feishu", "slack", "wxpusher", "matrix"];

// 渠道类型显示名（i18n）
const notifTypeLabels: Record<string, TKey> = {
  webhook: "alerts.typeWebhook",
  bark: "alerts.typeBark",
  telegram: "alerts.typeTelegram",
  email: "alerts.typeEmail",
  serverchan: "alerts.typeServerChan",
  javascript: "alerts.typeJavascript",
  dingtalk: "alerts.typeDingtalk",
  wecom: "alerts.typeWecom",
  feishu: "alerts.typeFeishu",
  slack: "alerts.typeSlack",
  wxpusher: "alerts.typeWxpusher",
  matrix: "alerts.typeMatrix",
};

const typeLabel = (type: string): TKey => notifTypeLabels[type] ?? (type as TKey);

// 预设渠道（dingtalk/wecom/feishu/slack/wxpusher/matrix）动态字段，映射到 Notification.extra 的 JSON key。
// tags 为逗号分隔的数组字段；checkbox 为布尔字段。
type ChannelField =
  | { kind: "text"; key: string; labelKey: TKey }
  | { kind: "tags"; key: string; labelKey: TKey }
  | { kind: "checkbox"; key: string; labelKey: TKey };

const presetFields: Record<string, ChannelField[]> = {
  dingtalk: [
    { kind: "text", key: "access_token", labelKey: "alerts.dingtalkAccessToken" },
    { kind: "text", key: "secret", labelKey: "alerts.dingtalkSecret" },
    { kind: "text", key: "keyword", labelKey: "alerts.dingtalkKeyword" },
    { kind: "tags", key: "at_mobiles", labelKey: "alerts.dingtalkAtMobiles" },
    { kind: "checkbox", key: "at_all", labelKey: "alerts.dingtalkAtAll" },
  ],
  wecom: [
    { kind: "text", key: "key", labelKey: "alerts.wecomKey" },
    { kind: "tags", key: "mentioned_list", labelKey: "alerts.wecomMentionedList" },
    { kind: "tags", key: "mentioned_mobile_list", labelKey: "alerts.wecomMentionedMobileList" },
  ],
  feishu: [
    { kind: "text", key: "token", labelKey: "alerts.feishuToken" },
    { kind: "text", key: "secret", labelKey: "alerts.feishuSecret" },
    { kind: "text", key: "keyword", labelKey: "alerts.feishuKeyword" },
    { kind: "text", key: "msg_type", labelKey: "alerts.feishuMsgType" },
  ],
  slack: [
    { kind: "text", key: "webhook_url", labelKey: "alerts.slackWebhookUrl" },
    { kind: "text", key: "channel", labelKey: "alerts.slackChannel" },
    { kind: "text", key: "username", labelKey: "alerts.slackUsername" },
    { kind: "text", key: "icon_emoji", labelKey: "alerts.slackIconEmoji" },
  ],
  wxpusher: [
    { kind: "text", key: "app_token", labelKey: "alerts.wxpusherAppToken" },
    { kind: "tags", key: "uids", labelKey: "alerts.wxpusherUids" },
    { kind: "tags", key: "topic_ids", labelKey: "alerts.wxpusherTopicIds" },
    { kind: "text", key: "content_type", labelKey: "alerts.wxpusherContentType" },
  ],
  matrix: [
    { kind: "text", key: "homeserver", labelKey: "alerts.matrixHomeserver" },
    { kind: "text", key: "access_token", labelKey: "alerts.matrixAccessToken" },
    { kind: "text", key: "room_id", labelKey: "alerts.matrixRoomId" },
  ],
};

const isPresetType = (type: string) => type in presetFields;

const emptyAlert = {
  name: "", metric: "cpu", min: null as number | null, max: null as number | null, duration: 30,
  notify: true, webhook_id: 0, group_id: 0, trigger_cron_id: 0, trigger_ratio: null as number | null, enabled: true,
};

// 静默时长选项（小时）
const silenceOptions = [1, 6, 24, 72];

export default function Alerts() {
  const { t, fmtDateTime } = useI18n();
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
  // 预设渠道动态字段值（key → 字符串/布尔；tags 字段暂存逗号分隔字符串，保存时拆分）
  const [extraObj, setExtraObj] = useState<Record<string, string | boolean>>({});
  const [error, setError] = useState("");
  const [testResult, setTestResult] = useState("");
  // 静默表单：{alertId, hours}
  const [silenceForm, setSilenceForm] = useState<{ id: number; hours: number } | null>(null);

  // 打开通知渠道表单：编辑时 extra 已被脱敏（空串），动态字段一律从空开始，避免回显凭据。
  const openChannelForm = (n?: Notification) => {
    setExtraObj({});
    setNForm(
      n
        ? { ...n }
        : { name: "", type: "webhook", url: "", method: "POST", headers: "{}", body: '{"title":"{{title}}","content":"{{content}}"}', chat_id: "", extra: "" },
    );
  };

  // 由动态字段构造渠道专属 extra JSON；空对象返回 "{}"（保存时跳过，编辑场景保留原值）。
  const buildExtra = (type: string, obj: Record<string, string | boolean>): string => {
    const out: Record<string, unknown> = {};
    for (const f of presetFields[type] ?? []) {
      const v = obj[f.key];
      if (f.kind === "checkbox") {
        out[f.key] = v === true;
      } else if (typeof v === "string" && v.trim() !== "") {
        if (f.kind === "tags") {
          const parts = v.split(",").map((s) => s.trim()).filter(Boolean);
          if (parts.length > 0) out[f.key] = parts;
        } else {
          out[f.key] = v.trim();
        }
      }
    }
    return JSON.stringify(out);
  };

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

  const ackAlert = useMutation({
    mutationFn: api.ackAlert,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["alerts"] }),
    onError: (e) => setError((e as Error).message),
  });
  const unackAlert = useMutation({
    mutationFn: api.unackAlert,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["alerts"] }),
    onError: (e) => setError((e as Error).message),
  });
  const silenceAlert = useMutation({
    mutationFn: async ({ id, hours }: { id: number; hours: number }) => {
      const until = new Date(Date.now() + hours * 3600 * 1000).toISOString();
      return api.silenceAlert(id, until);
    },
    onSuccess: () => {
      setSilenceForm(null);
      qc.invalidateQueries({ queryKey: ["alerts"] });
    },
    onError: (e) => setError((e as Error).message),
  });
  const unsilenceAlert = useMutation({
    mutationFn: api.unsilenceAlert,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["alerts"] }),
    onError: (e) => setError((e as Error).message),
  });

  const saveN = useMutation({
    mutationFn: async (n: Partial<Notification>) => {
      // 编辑时仅提交实际改动字段：读取已脱敏（url 掩码/headers/body/extra 空），
      // 掩码 URL 与空凭据字段不回传，避免覆盖原值（对齐 nezha 脱敏规范）
      const payload: Partial<Notification> = { id: n.id };
      if (n.name) payload.name = n.name;
      if (n.type) payload.type = n.type;
      if (n.url && !n.url.endsWith("/***")) payload.url = n.url;
      if (n.method) payload.method = n.method;
      if (n.headers && n.headers !== "{}") payload.headers = n.headers;
      if (n.body && n.body !== "{}") payload.body = n.body;
      if (n.chat_id) payload.chat_id = n.chat_id;
      if (n.extra && n.extra !== "{}") payload.extra = n.extra;
      return api.saveNotification(payload);
    },
    onSuccess: () => {
      setNForm(null);
      setExtraObj({});
      invalidate();
    },
    onError: (e) => setError((e as Error).message),
  });

  // 保存通知渠道：预设渠道由动态字段构造 extra；无任何配置时不提交（编辑场景保留原值）。
  const submitChannel = () => {
    if (!nForm) return;
    const type = nForm.type ?? "webhook";
    const payload: Partial<Notification> = { ...nForm };
    if (isPresetType(type)) {
      const extra = buildExtra(type, extraObj);
      payload.extra = extra === "{}" ? "" : extra;
    }
    saveN.mutate(payload);
  };
  const deleteN = useMutation({ mutationFn: api.deleteNotification, onSuccess: invalidate });
  const testN = useMutation({
    mutationFn: (id: number) => api.testMessage(id),
    onSuccess: (r) => setTestResult(t("alerts.testSent", { target: r.sent_to })),
    onError: (e) => setTestResult(t("alerts.testFailed", { error: (e as Error).message })),
  });

  return (
    <div>
      <div className="mb-5 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">{t("alerts.title")}</h1>
          <p className="text-sm text-muted">{t("alerts.subtitle")}</p>
        </div>
        <button
          onClick={() => setForm(emptyAlert)}
          className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-white hover:opacity-90"
        >
          <Plus className="h-4 w-4" />
          {t("alerts.newRule")}
        </button>
      </div>

      {error && <p className="mb-3 text-sm text-err">{error}</p>}

      {form && (
        <div className="mb-5 rounded-xl border border-border bg-panel p-4">
          <h2 className="mb-3 text-sm font-medium">{form.id ? t("alerts.editRule") : t("alerts.newRule")}</h2>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-4">
            <input
              placeholder={t("alerts.ruleName")}
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
                  {t(m.label)}
                </option>
              ))}
            </select>
            <input
              type="number"
              placeholder={t("alerts.min")}
              value={form.min ?? ""}
              onChange={(e) => setForm({ ...form, min: e.target.value === "" ? null : Number(e.target.value) })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent"
            />
            <input
              type="number"
              placeholder={t("alerts.max")}
              value={form.max ?? ""}
              onChange={(e) => setForm({ ...form, max: e.target.value === "" ? null : Number(e.target.value) })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent"
            />
          </div>
          <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-4">
            <label className="flex items-center gap-2 text-sm">
              {t("alerts.durationLabel")}
              <input
                type="number"
                value={form.duration}
                onChange={(e) => setForm({ ...form, duration: Number(e.target.value) })}
                className="w-20 rounded-lg border border-border bg-bg px-2 py-1.5 text-sm outline-none"
              />
              {t("alerts.seconds")}
            </label>
            <select
              value={form.webhook_id}
              onChange={(e) => setForm({ ...form, webhook_id: Number(e.target.value) })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            >
              <option value={0}>{t("alerts.noNotify")}</option>
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
              {t("common.enabled")}
            </label>
          </div>
          <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-4">
            <select
              value={form.trigger_cron_id}
              onChange={(e) => setForm({ ...form, trigger_cron_id: Number(e.target.value) })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            >
              <option value={0}>{t("alerts.triggerCronNone")}</option>
              {crons.map((c) => (
                <option key={c.id} value={c.id}>
                  {t("alerts.triggerCron", { name: c.name })}
                </option>
              ))}
            </select>
            <input
              type="number"
              placeholder={t("alerts.triggerRatio")}
              value={form.trigger_ratio ?? ""}
              onChange={(e) => setForm({ ...form, trigger_ratio: e.target.value === "" ? null : Number(e.target.value) })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            />
            <select
              value={form.group_id}
              onChange={(e) => setForm({ ...form, group_id: Number(e.target.value) })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            >
              <option value={0}>{t("alerts.notifGroupNone")}</option>
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
              {t("common.save")}
            </button>
            <button onClick={() => setForm(null)} className="rounded-lg border border-border px-4 py-1.5 text-sm text-muted">
              {t("common.cancel")}
            </button>
          </div>
        </div>
      )}

      {/* 静默表单 */}
      {silenceForm && (
        <div className="mb-5 flex flex-wrap items-center gap-3 rounded-xl border border-border bg-panel p-4">
          <span className="text-sm font-medium">{t("alerts.silenceFor")}</span>
          <select
            value={silenceForm.hours}
            onChange={(e) => setSilenceForm({ ...silenceForm, hours: Number(e.target.value) })}
            className="rounded-lg border border-border bg-bg px-3 py-1.5 text-sm outline-none"
          >
            {silenceOptions.map((h) => (
              <option key={h} value={h}>{t("alerts.silenceHours", { hours: h })}</option>
            ))}
          </select>
          <button
            onClick={() => silenceAlert.mutate(silenceForm)}
            className="rounded-lg bg-accent px-4 py-1.5 text-sm text-white hover:opacity-90"
          >
            {t("alerts.silenceApply")}
          </button>
          <button onClick={() => setSilenceForm(null)} className="rounded-lg border border-border px-4 py-1.5 text-sm text-muted">
            {t("common.cancel")}
          </button>
        </div>
      )}

      <div className="overflow-hidden rounded-xl border border-border bg-panel">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted">
              <th className="px-4 py-3 font-normal">{t("servers.name")}</th>
              <th className="px-4 py-3 font-normal">{t("alerts.metric")}</th>
              <th className="px-4 py-3 font-normal">{t("alerts.threshold")}</th>
              <th className="px-4 py-3 font-normal">{t("alerts.duration")}</th>
              <th className="px-4 py-3 font-normal">{t("alerts.notify")}</th>
              <th className="px-4 py-3 font-normal">{t("alerts.status")}</th>
              <th className="px-4 py-3 text-right font-normal">{t("alerts.actions")}</th>
            </tr>
          </thead>
          <tbody>
            {alerts.map((a: Alert) => (
              <tr key={a.id} className="border-b border-border last:border-0 hover:bg-black/2 dark:hover:bg-white/2">
                <td className="px-4 py-3 font-medium">{a.name}</td>
                <td className="px-4 py-3">{metrics.find((m) => m.key === a.metric)?.label ? t(metrics.find((m) => m.key === a.metric)!.label) : a.metric}</td>
                <td className="px-4 py-3 tabular">
                  {a.metric === "offline" ? t("alerts.offlineTrigger") : t("alerts.range", { min: a.min ?? "—", max: a.max ?? "—" })}
                </td>
                <td className="px-4 py-3 tabular">{a.duration}s</td>
                <td className="px-4 py-3">{a.notify ? notifications.find((n) => n.id === a.webhook_id)?.name ?? `#${a.webhook_id}` : t("alerts.notifOff")}</td>
                <td className="px-4 py-3">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <span className={`rounded-full px-2 py-0.5 text-xs ${a.enabled ? "bg-ok/15 text-ok" : "bg-muted/20 text-muted"}`}>
                      {a.enabled ? t("common.enabled") : t("common.disabled")}
                    </span>
                    {a.acked_at && (
                      <span className="rounded-full bg-accent/15 px-2 py-0.5 text-xs text-accent" title={fmtDateTime(a.acked_at)}>
                        {t("alerts.ackedBadge", { by: a.acked_by || "—" })}
                      </span>
                    )}
                    {a.silence_to && (
                      <span className="rounded-full bg-muted/20 px-2 py-0.5 text-xs text-muted" title={fmtDateTime(a.silence_from ?? "")}>
                        {t("alerts.silencedBadge", { until: fmtDateTime(a.silence_to) })}
                      </span>
                    )}
                  </div>
                </td>
                <td className="px-4 py-3">
                  <div className="flex justify-end gap-1">
                    {a.acked_at ? (
                      <button
                        onClick={() => unackAlert.mutate(a.id)}
                        title={t("alerts.unackTitle")}
                        className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5"
                      >
                        <Undo2 className="h-4 w-4" />
                      </button>
                    ) : (
                      <button
                        onClick={() => ackAlert.mutate(a.id)}
                        title={t("alerts.ackTitle")}
                        className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5"
                      >
                        <CheckCheck className="h-4 w-4" />
                      </button>
                    )}
                    {a.silence_to ? (
                      <button
                        onClick={() => unsilenceAlert.mutate(a.id)}
                        title={t("alerts.unsilenceTitle")}
                        className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5"
                      >
                        <Undo2 className="h-4 w-4" />
                      </button>
                    ) : (
                      <button
                        onClick={() => setSilenceForm({ id: a.id, hours: 24 })}
                        title={t("alerts.silenceTitle")}
                        className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5"
                      >
                        <BellOff className="h-4 w-4" />
                      </button>
                    )}
                    <button
                      onClick={() => setForm({ ...emptyAlert, ...a, id: a.id })}
                      className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5"
                    >
                      <Pencil className="h-4 w-4" />
                    </button>
                    <button
                      onClick={() => confirm(t("alerts.confirmDeleteRule", { name: a.name })) && deleteAlert.mutate(a.id)}
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
                  {t("alerts.noRules")}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {/* 通知渠道 */}
      <div className="mb-3 mt-8 flex items-center justify-between">
        <h2 className="text-lg font-semibold">{t("alerts.notifChannels")}</h2>
        <button
          onClick={() => openChannelForm()}
          className="flex items-center gap-2 rounded-lg border border-border px-3 py-1.5 text-sm hover:bg-black/5 dark:hover:bg-white/5"
        >
          <Plus className="h-4 w-4" />
          {t("alerts.add")}
        </button>
      </div>

      {nForm && (
        <div className="mb-5 rounded-xl border border-border bg-panel p-4">
          <h3 className="mb-3 text-sm font-medium">{t("alerts.channelOf", { type: t(typeLabel(nForm.type ?? "webhook")) })}</h3>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <input
              placeholder={t("servers.name")}
              value={nForm.name ?? ""}
              onChange={(e) => setNForm({ ...nForm, name: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            />
            <select
              value={nForm.type ?? "webhook"}
              onChange={(e) => {
                setNForm({ ...nForm, type: e.target.value });
                setExtraObj({});
              }}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            >
              {notifTypes.map((type) => (
                <option key={type} value={type}>{t(typeLabel(type))}</option>
              ))}
            </select>
            {isPresetType(nForm.type ?? "") ? (
              (presetFields[nForm.type ?? ""] ?? []).map((f) =>
                f.kind === "checkbox" ? (
                  <label key={f.key} className="flex items-center gap-2 text-sm">
                    <input
                      type="checkbox"
                      checked={extraObj[f.key] === true}
                      onChange={(e) => setExtraObj((o) => ({ ...o, [f.key]: e.target.checked }))}
                      className="h-4 w-4"
                    />
                    {t(f.labelKey)}
                  </label>
                ) : (
                  <input
                    key={f.key}
                    placeholder={t(f.labelKey)}
                    value={typeof extraObj[f.key] === "string" ? (extraObj[f.key] as string) : ""}
                    onChange={(e) => setExtraObj((o) => ({ ...o, [f.key]: e.target.value }))}
                    className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none sm:col-span-2"
                  />
                ),
              )
            ) : (
              <>
                <input
                  placeholder={t("alerts.url")}
                  value={nForm.url ?? ""}
                  onChange={(e) => setNForm({ ...nForm, url: e.target.value })}
                  className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
                />
                <input
                  placeholder={t("alerts.chatId")}
                  value={nForm.chat_id ?? ""}
                  onChange={(e) => setNForm({ ...nForm, chat_id: e.target.value })}
                  className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
                />
                <input
                  placeholder={t("alerts.headers")}
                  value={nForm.headers ?? ""}
                  onChange={(e) => setNForm({ ...nForm, headers: e.target.value })}
                  className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none sm:col-span-2"
                />
                <textarea
                  placeholder={t("alerts.body")}
                  value={nForm.body ?? ""}
                  onChange={(e) => setNForm({ ...nForm, body: e.target.value })}
                  className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none sm:col-span-2"
                  rows={2}
                />
              </>
            )}
          </div>
          <div className="mt-3 flex gap-2">
            <button
              onClick={submitChannel}
              className="rounded-lg bg-accent px-4 py-1.5 text-sm text-white hover:opacity-90"
            >
              {t("common.save")}
            </button>
            <button onClick={() => { setNForm(null); setExtraObj({}); }} className="rounded-lg border border-border px-4 py-1.5 text-sm text-muted">
              {t("common.cancel")}
            </button>
          </div>
        </div>
      )}
      {testResult && <p className="mb-3 text-sm text-muted">{testResult}</p>}

      <div className="overflow-hidden rounded-xl border border-border bg-panel">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted">
              <th className="px-4 py-3 font-normal">{t("servers.name")}</th>
              <th className="px-4 py-3 font-normal">{t("alerts.type")}</th>
              <th className="px-4 py-3 font-normal">URL</th>
              <th className="px-4 py-3 text-right font-normal">{t("alerts.actions")}</th>
            </tr>
          </thead>
          <tbody>
            {notifications.map((n) => (
              <tr key={n.id} className="border-b border-border last:border-0">
                <td className="px-4 py-3 font-medium">{n.name}</td>
                <td className="px-4 py-3 text-xs text-muted">{t(typeLabel(n.type))}</td>
                <td className="max-w-md truncate px-4 py-3 text-muted">{n.url}</td>
                <td className="px-4 py-3 text-right">
                  <button
                    onClick={() => testN.mutate(n.id)}
                    title={t("alerts.sendTestTitle")}
                    className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5"
                  >
                    <Send className="h-4 w-4" />
                  </button>
                  <button
                    onClick={() => openChannelForm(n)}
                    className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5"
                  >
                    <Pencil className="h-4 w-4" />
                  </button>
                  <button
                    onClick={() => confirm(t("alerts.confirmDeleteChannel", { name: n.name })) && deleteN.mutate(n.id)}
                    className="rounded p-1.5 text-err hover:bg-err/10"
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                </td>
              </tr>
            ))}
            {notifications.length === 0 && (
              <tr>
                <td colSpan={4} className="px-4 py-8 text-center text-muted">
                  {t("alerts.noChannels")}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
