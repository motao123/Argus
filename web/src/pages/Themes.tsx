// 主题管理（里程碑8）：上传 ZIP / 市场安装 / 启用 / 回滚 / 删除。
// 主题包仅 CSS 变量/CSS + 受限静态资源，禁止 JS；后端负责安全校验与原子安装。
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, Download, Palette, RotateCcw, Trash2, Upload } from "lucide-react";
import { api, type MarketThemeEntry, type ThemeInfo } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { useTheme } from "../lib/theme";

export default function Themes() {
  const { t, tErr } = useI18n();
  const { refresh } = useTheme();
  const qc = useQueryClient();
  const { data } = useQuery({ queryKey: ["themes"], queryFn: api.themeList });
  const { data: marketData } = useQuery({ queryKey: ["theme-market"], queryFn: api.themeMarket });
  const themes = data?.themes ?? [];
  const market = marketData?.themes ?? [];
  const [tab, setTab] = useState<"installed" | "market">("installed");
  const [file, setFile] = useState<File | null>(null);
  const [sha256, setSha256] = useState("");
  const [error, setError] = useState("");

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ["themes"] });
    qc.invalidateQueries({ queryKey: ["theme-market"] });
  };
  const afterChange = (msg: string) => {
    invalidate();
    void refresh(); // 激活/回滚/删除后刷新服务端主题注入
    setError("");
    alert(msg);
  };

  const upload = useMutation({
    mutationFn: (f: File) => api.themeUpload(f, sha256.trim()),
    onSuccess: (r) => {
      afterChange(t("themes.uploadDone", { name: r.theme.name, version: r.theme.version }));
      setFile(null);
      setSha256("");
    },
    onError: (e: Error) => setError(tErr(e)),
  });
  const activate = useMutation({
    mutationFn: api.themeActivate,
    onSuccess: () => {
      // 列表刷新后出现「启用中」徽标即可，无需弹窗
      invalidate();
      void refresh();
      setError("");
    },
    onError: (e: Error) => setError(tErr(e)),
  });
  const rollback = useMutation({
    mutationFn: api.themeRollback,
    onSuccess: (_, name) => afterChange(t("themes.rollbackDone", { name })),
    onError: (e: Error) => setError(tErr(e)),
  });
  const del = useMutation({
    mutationFn: api.themeDelete,
    onSuccess: (_, name) => afterChange(t("themes.deleteDone", { name })),
    onError: (e: Error) => setError(tErr(e)),
  });
  const install = useMutation({
    mutationFn: api.themeMarketInstall,
    onSuccess: (_, name) => afterChange(t("themes.installDone", { name })),
    onError: (e: Error) => setError(tErr(e)),
  });

  const fmtSize = (n: number) =>
    n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(1)} MiB` : n >= 1024 ? `${(n / 1024).toFixed(1)} KiB` : `${n} B`;

  return (
    <div>
      <h1 className="mb-1 flex items-center gap-2 text-xl font-semibold">
        <Palette className="h-5 w-5 text-accent" /> {t("themes.title")}
      </h1>
      <p className="mb-4 text-sm text-muted">{t("themes.subtitle")}</p>
      <div className="mb-4 flex flex-wrap items-center gap-2">
        {([["installed", t("themes.installed")], ["market", t("themes.market")]] as const).map(([k, label]) => (
          <button
            key={k}
            onClick={() => setTab(k)}
            className={`rounded-full px-4 py-1.5 text-sm ${tab === k ? "bg-accent text-white" : "bg-panel border border-border text-muted"}`}
          >
            {label}
          </button>
        ))}
        {tab === "installed" && (
          <label className="ml-auto flex cursor-pointer items-center gap-2 rounded-lg bg-accent px-4 py-1.5 text-sm text-white hover:opacity-90">
            <Upload className="h-4 w-4" />
            {t("themes.upload")}
            <input
              type="file"
              accept=".zip,application/zip"
              className="hidden"
              onChange={(e) => setFile(e.target.files?.[0] ?? null)}
            />
          </label>
        )}
      </div>

      {error && <div className="mb-4 rounded-lg bg-err/10 px-4 py-2 text-sm text-err">{error}</div>}

      {file && (
        <div className="mb-4 rounded-xl border border-border bg-panel p-4">
          <div className="mb-2 flex items-center gap-2 text-sm">
            <span className="font-medium">{file.name}</span>
            <span className="text-xs text-muted">{fmtSize(file.size)}</span>
          </div>
          <div className="flex items-center gap-2">
            <input
              value={sha256}
              onChange={(e) => setSha256(e.target.value)}
              placeholder={t("themes.sha256")}
              className="flex-1 rounded-lg border border-border bg-bg px-3 py-1.5 text-sm outline-none"
            />
            <button
              disabled={upload.isPending}
              onClick={() => upload.mutate(file)}
              className="rounded-lg bg-accent px-4 py-1.5 text-sm text-white disabled:opacity-40"
            >
              {upload.isPending ? t("themes.uploading") : t("themes.uploadBtn")}
            </button>
            <button onClick={() => setFile(null)} className="rounded-lg border border-border px-3 py-1.5 text-sm">
              {t("common.cancel")}
            </button>
          </div>
        </div>
      )}

      {tab === "installed" && (
        <div className="space-y-3">
          {themes.map((th: ThemeInfo) => (
            <div key={th.name} className="rounded-xl border border-border bg-panel p-4">
              <div className="flex items-start justify-between gap-3">
                <div className="flex min-w-0 items-start gap-3">
                  {th.preview && (
                    <img
                      src={`/theme-assets/${encodeURIComponent(th.name)}/${th.preview}`}
                      alt={th.name}
                      className="mt-0.5 h-14 w-20 shrink-0 rounded-lg border border-border object-cover"
                      loading="lazy"
                    />
                  )}
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2 font-medium">
                      {th.display_name || th.name}
                      <span className="text-xs text-muted">v{th.version}</span>
                      {th.name === "default" && (
                        <span className="rounded-full bg-muted/20 px-2 py-0.5 text-xs text-muted">{t("themes.builtin")}</span>
                      )}
                      {th.active && (
                        <span className="rounded-full bg-ok/15 px-2 py-0.5 text-xs text-ok">{t("themes.active")}</span>
                      )}
                      {th.rollback && (
                        <span className="rounded-full bg-warn/15 px-2 py-0.5 text-xs text-warn">{t("themes.rollbackAvailable")}</span>
                      )}
                    </div>
                    <p className="mt-1 text-xs text-muted">
                      {th.author ? t("themes.author", { name: th.author }) + " · " : ""}
                      {th.argus ? t("themes.compat", { constraint: th.argus }) + " · " : ""}
                      {t("themes.entry", { path: th.entry })}
                    </p>
                    {th.name === "default" && <p className="mt-1 text-xs text-muted">{t("themes.defaultDesc")}</p>}
                  </div>
                </div>
                <div className="flex shrink-0 items-center gap-1">
                  {th.name !== "default" && !th.active && (
                    <button
                      onClick={() => activate.mutate(th.name)}
                      className="rounded-full bg-accent px-3 py-1 text-xs text-white hover:opacity-90"
                    >
                      {t("themes.activate")}
                    </button>
                  )}
                  {th.name !== "default" && th.rollback && (
                    <button
                      onClick={() => rollback.mutate(th.name)}
                      title={t("themes.rollback")}
                      className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5"
                    >
                      <RotateCcw className="h-4 w-4" />
                    </button>
                  )}
                  {th.name !== "default" && (
                    <button
                      onClick={() => confirm(t("themes.confirmDelete", { name: th.name })) && del.mutate(th.name)}
                      title={t("common.delete")}
                      className="rounded p-1.5 text-err hover:bg-err/10"
                    >
                      <Trash2 className="h-4 w-4" />
                    </button>
                  )}
                </div>
              </div>
            </div>
          ))}
        </div>
      )}

      {tab === "market" && (
        <div className="space-y-3">
          {market.map((m: MarketThemeEntry) => (
            <div key={m.name} className="flex items-center justify-between rounded-xl border border-border bg-panel p-4">
              <div>
                <div className="font-medium">
                  {m.display_name || m.name} <span className="text-xs text-muted">v{m.version}</span>
                </div>
                <p className="mt-1 text-xs text-muted">
                  {m.author ? t("themes.author", { name: m.author }) + " · " : ""}
                  {t("themes.sizeOf", { size: fmtSize(m.size) })}
                  {m.description ? ` · ${m.description}` : ""}
                </p>
              </div>
              <button
                disabled={m.installed}
                onClick={() => install.mutate(m.name)}
                className="flex items-center gap-1.5 rounded-lg bg-accent px-4 py-1.5 text-sm text-white disabled:opacity-40"
              >
                <Download className="h-4 w-4" />
                {m.installed ? t("themes.installed") : t("themes.install")}
              </button>
            </div>
          ))}
          {market.length === 0 && <div className="py-8 text-center text-sm text-muted">{t("themes.marketEmpty")}</div>}
        </div>
      )}
    </div>
  );
}
