import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, Play, ShieldCheck, Trash2, Zap } from "lucide-react";
import { api, type PluginInfo } from "../lib/api";

export default function Plugins() {
  const qc = useQueryClient();
  const { data } = useQuery({ queryKey: ["plugins"], queryFn: api.plugins });
  const { data: marketData } = useQuery({ queryKey: ["plugin-market"], queryFn: api.pluginMarket });
  const plugins = data?.plugins ?? [];
  const market = marketData?.plugins ?? [];
  const [tab, setTab] = useState<"installed" | "market">("installed");
  const [logs, setLogs] = useState<PluginInfo | null>(null);

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ["plugins"] });
    qc.invalidateQueries({ queryKey: ["plugin-market"] });
  };
  const toggle = useMutation({ mutationFn: ({ name, enabled }: { name: string; enabled: boolean }) => api.pluginToggle(name, enabled), onSuccess: invalidate });
  const approve = useMutation({ mutationFn: ({ name, approved }: { name: string; approved: boolean }) => api.pluginApprove(name, approved), onSuccess: invalidate });
  const run = useMutation({ mutationFn: api.pluginRun, onSuccess: invalidate });
  const del = useMutation({ mutationFn: api.pluginDelete, onSuccess: invalidate });
  const install = useMutation({ mutationFn: api.pluginInstall, onSuccess: invalidate });

  return (
    <div>
      <h1 className="mb-1 flex items-center gap-2 text-xl font-semibold">
        <Zap className="h-5 w-5 text-accent" /> 插件
      </h1>
      <p className="mb-4 text-sm text-muted">
        插件为 JS 沙箱（goja）。网络请求需声明 <code>allow_fetch</code> 且由管理员批准，防止 SSRF。
      </p>
      <div className="mb-4 flex gap-1">
        {([["installed", "已安装"], ["market", "市场"]] as const).map(([k, label]) => (
          <button
            key={k}
            onClick={() => setTab(k)}
            className={`rounded-full px-4 py-1.5 text-sm ${tab === k ? "bg-accent text-white" : "bg-panel border border-border text-muted"}`}
          >
            {label}
          </button>
        ))}
      </div>

      {tab === "installed" && (
        <div className="space-y-3">
          {plugins.map((p: PluginInfo) => (
            <div key={p.name} className="rounded-xl border border-border bg-panel p-4">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2 font-medium">
                    {p.name}
                    <span className="text-xs text-muted">v{p.version}</span>
                    {p.permissions_allow_fetch && (
                      <span className="rounded-full bg-warn/15 px-2 py-0.5 text-xs text-warn">需网络</span>
                    )}
                    {p.approved ? (
                      <span className="rounded-full bg-ok/15 px-2 py-0.5 text-xs text-ok">已批准</span>
                    ) : p.permissions_allow_fetch ? (
                      <span className="rounded-full bg-err/15 px-2 py-0.5 text-xs text-err">待批准</span>
                    ) : null}
                  </div>
                  <p className="mt-1 text-xs text-muted">{p.description}</p>
                  <p className="mt-1 text-xs text-muted">cron: {p.cron || "无"} · 上次运行: {p.last_run || "—"}</p>
                </div>
                <div className="flex shrink-0 items-center gap-1">
                  {p.permissions_allow_fetch && (
                    <button
                      onClick={() => approve.mutate({ name: p.name, approved: !p.approved })}
                      title="批准/撤销网络权限"
                      className={`rounded p-1.5 ${p.approved ? "text-ok" : "text-muted"}`}
                    >
                      <ShieldCheck className="h-4 w-4" />
                    </button>
                  )}
                  <button onClick={() => setLogs(p)} title="查看日志" className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5">
                    <CheckCircle2 className="h-4 w-4" />
                  </button>
                  <button onClick={() => run.mutate(p.name)} title="立即运行" className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5">
                    <Play className="h-4 w-4" />
                  </button>
                  <button
                    onClick={() => toggle.mutate({ name: p.name, enabled: !p.enabled })}
                    className={`rounded-full px-3 py-1 text-xs ${p.enabled ? "bg-ok/15 text-ok" : "bg-muted/20 text-muted"}`}
                  >
                    {p.enabled ? "已启用" : "已停用"}
                  </button>
                  <button
                    onClick={() => confirm(`删除插件「${p.name}」？`) && del.mutate(p.name)}
                    title="删除"
                    className="rounded p-1.5 text-err hover:bg-err/10"
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                </div>
              </div>
            </div>
          ))}
          {plugins.length === 0 && <div className="py-8 text-center text-sm text-muted">暂无已安装插件</div>}
        </div>
      )}

      {tab === "market" && (
        <div className="space-y-3">
          {market.map((m) => (
            <div key={m.name} className="flex items-center justify-between rounded-xl border border-border bg-panel p-4">
              <div>
                <div className="font-medium">{m.name} <span className="text-xs text-muted">v{m.version}</span></div>
                <p className="text-xs text-muted">{m.description}</p>
              </div>
              <button
                disabled={m.installed}
                onClick={() => install.mutate(m.name)}
                className="rounded-lg bg-accent px-4 py-1.5 text-sm text-white disabled:opacity-40"
              >
                {m.installed ? "已安装" : "安装"}
              </button>
            </div>
          ))}
          {market.length === 0 && <div className="py-8 text-center text-sm text-muted">市场为空（将插件目录放入 data/market/plugins）</div>}
        </div>
      )}

      {logs && (
        <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/40 p-4" onClick={() => setLogs(null)}>
          <div className="w-full max-w-lg rounded-xl border border-border bg-panel p-4" onClick={(e) => e.stopPropagation()}>
            <h3 className="mb-3 text-sm font-medium">「{logs.name}」日志</h3>
            <pre className="max-h-72 overflow-auto whitespace-pre-wrap rounded-lg bg-bg p-3 text-xs">
              {logs.logs?.join("\n") || "暂无日志"}
            </pre>
            <button onClick={() => setLogs(null)} className="mt-3 rounded-lg border border-border px-4 py-1.5 text-sm">关闭</button>
          </div>
        </div>
      )}
    </div>
  );
}
