import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { api, type ServiceItem } from "../lib/api";
import { useServers } from "../context/servers";

// 最近 30 天可用率色块（真实数据，借鉴 dash-v2 ServiceTracker）
function UptimeBlocks({ svcId }: { svcId: number }) {
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
    <div className="flex gap-[2px]" title="最近 30 天可用率">
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
  const qc = useQueryClient();
  const { servers } = useServers();
  const { data } = useQuery({ queryKey: ["services"], queryFn: api.services, refetchInterval: 10000 });
  const services = data?.services ?? [];

  const [form, setForm] = useState<Partial<ServiceItem> | null>(null);
  const [error, setError] = useState("");

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
          <h1 className="text-xl font-semibold">服务监控</h1>
          <p className="text-sm text-muted">HTTP / TCP / Ping 探测，由指定服务器上的 Agent 执行</p>
        </div>
        <button
          onClick={() => setForm({ server_id: servers[0]?.id ?? 0, name: "", type: "http", target: "", interval: 60, enabled: true })}
          className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-white hover:opacity-90"
        >
          <Plus className="h-4 w-4" />
          新建服务
        </button>
      </div>

      {error && <p className="mb-3 text-sm text-err">{error}</p>}

      {form && (
        <div className="mb-5 rounded-xl border border-border bg-panel p-4">
          <h2 className="mb-3 text-sm font-medium">服务配置</h2>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-6">
            <select
              value={form.server_id ?? 0}
              onChange={(e) => setForm({ ...form, server_id: Number(e.target.value) })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            >
              <option value={0}>选择探测服务器</option>
              {servers.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </select>
            <input
              placeholder="名称"
              value={form.name ?? ""}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            />
            <select
              value={form.type ?? "http"}
              onChange={(e) => setForm({ ...form, type: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            >
              <option value="http">HTTP</option>
              <option value="tcp">TCP</option>
              <option value="ping">Ping</option>
            </select>
            <input
              placeholder="目标（URL / host:port / host）"
              value={form.target ?? ""}
              onChange={(e) => setForm({ ...form, target: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none sm:col-span-2"
            />
            <input
              type="number"
              placeholder="间隔秒"
              value={form.interval ?? 60}
              onChange={(e) => setForm({ ...form, interval: Number(e.target.value) })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            />
          </div>
          <div className="mt-3 flex gap-2">
            <button
              onClick={() => save.mutate(form)}
              disabled={!form.server_id || !form.name || !form.target}
              className="rounded-lg bg-accent px-4 py-1.5 text-sm text-white hover:opacity-90 disabled:opacity-40"
            >
              保存
            </button>
            <button onClick={() => setForm(null)} className="rounded-lg border border-border px-4 py-1.5 text-sm text-muted">
              取消
            </button>
          </div>
        </div>
      )}

      <div className="space-y-3">
        {services.map((svc) => (
          <div key={svc.id} className="rounded-xl border border-border bg-panel p-4">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-3">
                <span
                  className={`h-2.5 w-2.5 rounded-full ${svc.last_up ? "bg-ok shadow-[0_0_6px] shadow-ok" : "bg-err"}`}
                />
                <span className="font-medium">{svc.name}</span>
                <span className="rounded-full bg-accent/10 px-2 py-0.5 text-xs text-accent">
                  {typeLabels[svc.type] ?? svc.type}
                </span>
                <span className="text-xs text-muted">
                  {svc.target} · 每 {svc.interval}s
                </span>
              </div>
              <div className="flex items-center gap-3">
                <span className="tabular text-sm text-muted">
                  {svc.last_up ? "正常" : "异常"} · {svc.last_delay}ms
                </span>
                <span className={`tabular rounded-full px-2 py-0.5 text-xs ${svc.today_up_rate >= 99 ? "bg-ok/15 text-ok" : svc.today_up_rate >= 90 ? "bg-warn/15 text-warn" : "bg-err/15 text-err"}`}>
                  今日 {svc.today_up_rate.toFixed(1)}%
                </span>
                <UptimeBlocks svcId={svc.id} />
                <button onClick={() => setForm({ ...svc })} className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5">
                  <Pencil className="h-4 w-4" />
                </button>
                <button
                  onClick={() => confirm(`删除服务「${svc.name}」？`) && remove.mutate(svc.id)}
                  className="rounded p-1.5 text-err hover:bg-err/10"
                >
                  <Trash2 className="h-4 w-4" />
                </button>
              </div>
            </div>
          </div>
        ))}
        {services.length === 0 && (
          <div className="rounded-xl border border-dashed border-border py-16 text-center text-sm text-muted">
            暂无服务监控 —— 添加 HTTP/TCP/Ping 探测任务
          </div>
        )}
      </div>
    </div>
  );
}
