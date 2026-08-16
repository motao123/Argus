import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Save } from "lucide-react";
import { api } from "../lib/api";

// 站点设置：站点名/描述/favicon、私有站点（force_auth）、终端外观（借鉴 komari xtermjs 设置 + nezha force_auth）
const fieldMeta: { key: string; label: string; type: "text" | "number" | "select" | "textarea"; help?: string; options?: { v: string; label: string }[] }[] = [
  { key: "site_name", label: "站点名称", type: "text" },
  { key: "site_desc", label: "站点描述", type: "text" },
  { key: "favicon", label: "Favicon URL", type: "text" },
  { key: "force_auth", label: "私有站点模式", type: "select", help: "开启后游客无法查看任何数据（借鉴 komari 私有站点 + nezha force_auth）", options: [{ v: "0", label: "关闭（游客可看公开视图）" }, { v: "1", label: "开启（强制登录）" }] },
  { key: "term_font_size", label: "终端字号", type: "number", help: "网页终端字体大小（px，默认 13）" },
  { key: "term_theme", label: "终端主题", type: "select", help: "xterm.js 明暗主题", options: [{ v: "dark", label: "深色" }, { v: "light", label: "浅色" }] },
  { key: "custom_css", label: "自定义 CSS", type: "textarea", help: "注入所有页面 <head>，可用于白标定制" },
  { key: "custom_js", label: "自定义 JS", type: "textarea", help: "注入所有页面 </body> 前" },
  { key: "custom_footer", label: "前台页脚 HTML", type: "textarea", help: "注入前台 Powered by 之前" },
];

export default function Settings() {
  const qc = useQueryClient();
  const { data } = useQuery({ queryKey: ["settings"], queryFn: api.settings });
  const current = data?.settings ?? {};
  const [form, setForm] = useState<Record<string, string>>({});
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    if (Object.keys(form).length === 0) {
      const init: Record<string, string> = {};
      for (const f of fieldMeta) init[f.key] = current[f.key] ?? (f.type === "number" ? "13" : f.type === "select" ? f.options?.[0].v ?? "" : "");
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
      <h1 className="mb-1 text-xl font-semibold">站点设置</h1>
      <p className="mb-5 text-sm text-muted">站点信息、访问控制与终端外观（改动即时生效，无需重启）</p>

      <div className="max-w-xl rounded-xl border border-border bg-panel p-5">
        <div className="space-y-4">
          {fieldMeta.map((f) => (
            <label key={f.key} className="block">
              <span className="mb-1 block text-sm font-medium">{f.label}</span>
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
                      {o.label}
                    </option>
                  ))}
                </select>
              ) : (
                <input
                  type={f.type === "number" ? "number" : "text"}
                  value={form[f.key] ?? ""}
                  onChange={(e) => setForm({ ...form, [f.key]: e.target.value })}
                  className="w-full rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
                />
              )}
              {f.help && <span className="mt-1 block text-xs text-muted">{f.help}</span>}
            </label>
          ))}
        </div>
        <div className="mt-5 flex items-center gap-3">
          <button
            onClick={() => save.mutate(form)}
            disabled={save.isPending}
            className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-white hover:opacity-90 disabled:opacity-40"
          >
            <Save className="h-4 w-4" />
            保存设置
          </button>
          {saved && <span className="text-sm text-ok">已保存 ✓</span>}
        </div>
      </div>
    </div>
  );
}
