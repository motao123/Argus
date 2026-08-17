import { Fragment, useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Save } from "lucide-react";
import { api } from "../lib/api";
import { useI18n, type TKey } from "../lib/i18n";

// 全局设置：站点、终端外观与后台数据保留策略。
const fieldMeta: { key: string; label: TKey; type: "text" | "number" | "select" | "textarea" | "webhook"; defaultValue?: string; min?: number; max?: number; section?: TKey; note?: TKey; help?: TKey; options?: { v: string; label: TKey }[] }[] = [
  { key: "site_name", label: "settings.siteName", type: "text" },
  { key: "site_desc", label: "settings.siteDesc", type: "text" },
  { key: "favicon", label: "settings.favicon", type: "text" },
  { key: "force_auth", label: "settings.forceAuth", type: "select", help: "settings.forceAuthHelp", options: [{ v: "0", label: "settings.forceAuthOff" }, { v: "1", label: "settings.forceAuthOn" }] },
  { key: "term_font_size", label: "settings.termFontSize", type: "number", help: "settings.termFontSizeHelp" },
  { key: "term_theme", label: "settings.termTheme", type: "select", help: "settings.termThemeHelp", options: [{ v: "dark", label: "settings.termThemeDark" }, { v: "light", label: "settings.termThemeLight" }] },
  { key: "custom_css", label: "settings.customCss", type: "textarea", help: "settings.customCssHelp" },
  { key: "custom_js", label: "settings.customJs", type: "textarea", help: "settings.customJsHelp" },
  { key: "custom_footer", label: "settings.customFooter", type: "textarea", help: "settings.customFooterHelp" },
  { key: "install_base_url", label: "settings.installBaseUrl", type: "text", help: "settings.installBaseUrlHelp" },
  { key: "retention_metric_1m_days", label: "settings.retention1m", type: "number", defaultValue: "1", min: 1, max: 30, section: "settings.retentionSection" },
  { key: "retention_metric_5m_days", label: "settings.retention5m", type: "number", defaultValue: "7", min: 1, max: 365, section: "settings.retentionSection" },
  { key: "retention_metric_1h_days", label: "settings.retention1h", type: "number", defaultValue: "30", min: 1, max: 3650, section: "settings.retentionSection" },
  { key: "retention_service_history_days", label: "settings.retentionService", type: "number", defaultValue: "30", min: 1, max: 3650, section: "settings.retentionSection" },
  { key: "retention_transfer_days", label: "settings.retentionTransfer", type: "number", defaultValue: "365", min: 1, max: 3650, section: "settings.retentionSection" },
  { key: "retention_task_run_days", label: "settings.retentionTaskRun", type: "number", defaultValue: "30", min: 1, max: 3650, section: "settings.retentionSection" },
  { key: "retention_audit_days", label: "settings.retentionAudit", type: "number", defaultValue: "365", min: 1, max: 3650, section: "settings.retentionSection", help: "settings.retentionAuditHelp" },
  { key: "retention_audit_max_rows", label: "settings.retentionAuditRows", type: "number", defaultValue: "5000", min: 100, max: 1000000, section: "settings.retentionSection", help: "settings.retentionAuditRowsHelp" },
  // Agent 自动更新（对标 Nezha：agent 随机 30-90 分钟自查，发现新版本自动升级）
  { key: "upgrade_latest_version", label: "settings.upgradeVersion", type: "text", section: "settings.upgradeSection", note: "settings.upgradeNote" },
  { key: "upgrade_latest_url", label: "settings.upgradeUrl", type: "text", section: "settings.upgradeSection" },
  { key: "upgrade_latest_sha256", label: "settings.upgradeSha256", type: "text", section: "settings.upgradeSection" },
  // 通知与分享（P3：IP 打码 / 登录通知 / 临时分享密钥）
  { key: "mask_ip", label: "settings.maskIp", type: "select", help: "settings.maskIpHelp", options: [{ v: "0", label: "settings.off" }, { v: "1", label: "settings.on" }] },
  { key: "login_notify_webhook_id", label: "settings.loginNotify", type: "webhook", defaultValue: "0", help: "settings.loginNotifyHelp" },
  { key: "temp_share_key", label: "settings.tempShareKey", type: "text", help: "settings.tempShareKeyHelp" },
  { key: "temp_share_expires_at", label: "settings.tempShareExpires", type: "text", help: "settings.tempShareExpiresHelp" },
];

export default function Settings() {
  const { t } = useI18n();
  const qc = useQueryClient();
  const { data } = useQuery({ queryKey: ["settings"], queryFn: api.settings });
  const { data: notifData } = useQuery({ queryKey: ["notifications"], queryFn: api.notifications });
  const notifications = notifData?.notifications ?? [];
  const current = data?.settings ?? {};
  const [form, setForm] = useState<Record<string, string>>({});
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    if (Object.keys(form).length === 0) {
      const init: Record<string, string> = {};
      for (const f of fieldMeta) init[f.key] = current[f.key] ?? f.defaultValue ?? (f.type === "number" ? "13" : f.type === "select" ? f.options?.[0].v ?? "" : "");
      setForm(init);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [current]);

  const save = useMutation({
    mutationFn: (v: Record<string, string>) => api.saveSettings(v),
    onSuccess: () => {
      setSaved(true);
      qc.invalidateQueries({ queryKey: ["settings"] });
      setTimeout(() => setSaved(false), 2000);
    },
  });

  return (
    <div>
      <h1 className="mb-1 text-xl font-semibold">{t("settings.title")}</h1>
      <p className="mb-5 text-sm text-muted">{t("settings.subtitle")}</p>

      <div className="max-w-xl rounded-xl border border-border bg-panel p-5">
        <div className="space-y-4">
          {fieldMeta.map((f, index) => (
            <Fragment key={f.key}>
              {f.section && f.section !== fieldMeta[index - 1]?.section && (
                <div className="border-t border-border pt-4">
                  <h2 className="text-base font-semibold">{t(f.section)}</h2>
                  <p className="mt-1 text-xs text-muted">{t(f.note ?? "settings.retentionNote")}</p>
                </div>
              )}
            <label className="block">
              <span className="mb-1 block text-sm font-medium">{t(f.label)}</span>
              {f.type === "textarea" ? (
                <textarea
                  rows={4}
                  value={form[f.key] ?? ""}
                  onChange={(e) => setForm({ ...form, [f.key]: e.target.value })}
                  className="w-full rounded-lg border border-border bg-bg px-3 py-2 font-mono text-xs outline-none"
                />
              ) : f.type === "select" ? (
                <select
                  value={form[f.key] ?? ""}
                  onChange={(e) => setForm({ ...form, [f.key]: e.target.value })}
                  className="w-full rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
                >
                  {f.options?.map((o) => (
                    <option key={o.v} value={o.v}>
                      {t(o.label)}
                    </option>
                  ))}
                </select>
              ) : f.type === "webhook" ? (
                <select
                  value={form[f.key] ?? "0"}
                  onChange={(e) => setForm({ ...form, [f.key]: e.target.value })}
                  className="w-full rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
                >
                  <option value="0">{t("settings.noChannel")}</option>
                  {notifications.map((n) => (
                    <option key={n.id} value={String(n.id)}>
                      {n.name}
                    </option>
                  ))}
                </select>
              ) : (
                <input
                  type={f.type === "number" ? "number" : "text"}
                  min={f.min}
                  max={f.max}
                  value={form[f.key] ?? ""}
                  onChange={(e) => setForm({ ...form, [f.key]: e.target.value })}
                  className="w-full rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
                />
              )}
              {f.help && <span className="mt-1 block text-xs text-muted">{t(f.help)}</span>}
            </label>
            </Fragment>
          ))}
        </div>
        <div className="mt-5 flex items-center gap-3">
          <button
            onClick={() => save.mutate(form)}
            disabled={save.isPending}
            className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-white hover:opacity-90 disabled:opacity-40"
          >
            <Save className="h-4 w-4" />
            {t("settings.saveSettings")}
          </button>
          {saved && <span className="text-sm text-ok">{t("settings.saved")}</span>}
        </div>
      </div>
    </div>
  );
}
