import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Activity, Radio, Send } from "lucide-react";
import { api, type TraceResult } from "../lib/api";
import { useServers } from "../context/servers";
import { useI18n } from "../lib/i18n";

// 带宽格式化：bit/s → 可读速率
function formatBps(bps: number | undefined): string {
  if (bps === undefined || !isFinite(bps)) return "—";
  const units = ["b/s", "Kb/s", "Mb/s", "Gb/s"];
  let v = bps;
  let u = 0;
  while (v >= 1000 && u < units.length - 1) {
    v /= 1000;
    u++;
  }
  return `${v.toFixed(1)} ${units[u]}`;
}

function TraceTable({ trace, target }: { trace: TraceResult; target: string }) {
  const { t } = useI18n();
  if (trace.error && !trace.ok) {
    return <p className="text-sm text-err">{t("networkTest.traceError", { error: trace.error })}</p>;
  }
  const hops = trace.hops ?? [];
  if (hops.length === 0 && !trace.ok) {
    return <p className="text-sm text-muted">{t("networkTest.noHops")}</p>;
  }
  return (
    <div>
      <p className="mb-2 text-xs text-muted">
        {t("networkTest.target")} {target} · {t("networkTest.traceCount", { count: hops.length })}
      </p>
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="w-full text-left text-sm">
          <thead className="bg-black/5 text-xs text-muted dark:bg-white/5">
            <tr>
              <th className="px-3 py-2 font-normal">{t("networkTest.hop")}</th>
              <th className="px-3 py-2 font-normal">{t("networkTest.ip")}</th>
              <th className="px-3 py-2 font-normal">{t("networkTest.rtt")}</th>
              <th className="px-3 py-2 font-normal">{t("networkTest.loss")}</th>
            </tr>
          </thead>
          <tbody>
            {hops.map((h) => (
              <tr key={h.hop} className="border-t border-border">
                <td className="px-3 py-1.5 tabular-nums">{h.hop}</td>
                <td className="px-3 py-1.5 font-mono text-xs">{h.ip || "(*)"}</td>
                <td className="px-3 py-1.5 tabular-nums">{h.rtt_ms > 0 ? `${h.rtt_ms.toFixed(2)} ms` : "—"}</td>
                <td className="px-3 py-1.5 tabular-nums">{h.loss > 0 ? `${h.loss}%` : "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {trace.raw_text && (
        <details className="mt-2">
          <summary className="cursor-pointer text-xs text-muted">{t("networkTest.rawOutput")}</summary>
          <pre className="mt-1 max-h-64 overflow-auto whitespace-pre-wrap rounded-lg bg-black/5 p-3 text-xs dark:bg-white/5">{trace.raw_text}</pre>
        </details>
      )}
    </div>
  );
}

export default function NetworkTest() {
  const { t } = useI18n();
  const { servers } = useServers();
  const [tab, setTab] = useState<"trace" | "mesh" | "bandwidth">("trace");
  const [error, setError] = useState("");

  // 单源 trace 表单
  const [traceServer, setTraceServer] = useState(0);
  const [traceTarget, setTraceTarget] = useState("");
  const [traceProtocol, setTraceProtocol] = useState("icmp");
  const traceMut = useMutation({
    mutationFn: (v: { id: number; target: string; protocol: string }) => api.trace(v.id, v.target, { protocol: v.protocol }),
    onError: (e) => setError((e as Error).message),
  });

  // mesh trace 表单
  const [meshSources, setMeshSources] = useState<number[]>([]);
  const [meshTargets, setMeshTargets] = useState("");
  const [meshMode, setMeshMode] = useState("all_to_all");
  const meshMut = useMutation({
    mutationFn: (v: { source_ids: number[]; targets: string[]; mode: string }) => api.traceMesh({ ...v, protocol: "icmp" }),
    onError: (e) => setError((e as Error).message),
  });

  // 带宽测速表单
  const [bwSource, setBwSource] = useState(0);
  const [bwTarget, setBwTarget] = useState("");
  const [bwDuration, setBwDuration] = useState(5);
  const bwMut = useMutation({
    mutationFn: (v: { source_id: number; target: string; duration: number }) =>
      api.bandwidthTest({ source_id: v.source_id, target: v.target, duration: v.duration }),
    onError: (e) => setError((e as Error).message),
  });

  const toggleMeshSource = (id: number) => {
    setMeshSources((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]));
  };

  const inputCls = "rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none";

  return (
    <div>
      <div className="mb-5 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">{t("networkTest.title")}</h1>
          <p className="text-sm text-muted">{t("networkTest.subtitle")}</p>
        </div>
      </div>

      <div className="mb-4 flex gap-1 rounded-lg bg-black/5 p-1 dark:bg-white/5">
        {(
          [
            ["trace", "networkTest.tabTrace", Activity],
            ["mesh", "networkTest.tabMesh", Radio],
            ["bandwidth", "networkTest.tabBandwidth", Send],
          ] as const
        ).map(([k, label, Icon]) => (
          <button
            key={k}
            onClick={() => setTab(k)}
            className={`flex items-center gap-1.5 rounded-md px-3 py-1.5 text-sm ${tab === k ? "bg-accent text-white" : "text-muted hover:bg-black/5 dark:hover:bg-white/5"}`}
          >
            <Icon className="h-4 w-4" />
            {t(label)}
          </button>
        ))}
      </div>

      {error && <p className="mb-3 text-sm text-err">{error}</p>}

      {/* 单源路由追踪 */}
      {tab === "trace" && (
        <div className="space-y-4">
          <div className="rounded-xl border border-border bg-panel p-4">
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-4">
              <select value={traceServer} onChange={(e) => setTraceServer(Number(e.target.value))} className={inputCls}>
                <option value={0}>{t("networkTest.pickSource")}</option>
                {servers.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name}
                  </option>
                ))}
              </select>
              <input
                placeholder={t("networkTest.targetPlaceholder")}
                value={traceTarget}
                onChange={(e) => setTraceTarget(e.target.value)}
                className={inputCls}
              />
              <select value={traceProtocol} onChange={(e) => setTraceProtocol(e.target.value)} className={inputCls}>
                <option value="icmp">ICMP</option>
                <option value="tcp">TCP</option>
                <option value="udp">UDP</option>
              </select>
              <button
                onClick={() => traceMut.mutate({ id: traceServer, target: traceTarget, protocol: traceProtocol })}
                disabled={!traceServer || !traceTarget || traceMut.isPending}
                className="flex items-center justify-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-white hover:opacity-90 disabled:opacity-40"
              >
                <Send className="h-4 w-4" />
                {traceMut.isPending ? t("networkTest.running") : t("networkTest.start")}
              </button>
            </div>
          </div>
          {traceMut.data && (
            <div className="rounded-xl border border-border bg-panel p-4">
              <TraceTable trace={traceMut.data.trace} target={traceMut.data.target} />
            </div>
          )}
        </div>
      )}

      {/* 多源路由追踪 */}
      {tab === "mesh" && (
        <div className="space-y-4">
          <div className="rounded-xl border border-border bg-panel p-4">
            <div className="grid grid-cols-1 gap-3 text-sm sm:grid-cols-4">
              <div className="rounded-lg border border-border bg-bg p-2">
                <p className="mb-1.5 text-xs text-muted">{t("networkTest.pickSources")}</p>
                <div className="flex max-h-32 flex-wrap gap-1.5 overflow-auto">
                  {servers.map((s) => (
                    <button
                      key={s.id}
                      onClick={() => toggleMeshSource(s.id)}
                      className={`rounded-full px-2.5 py-1 text-xs ${meshSources.includes(s.id) ? "bg-accent text-white" : "bg-black/5 text-muted dark:bg-white/5"}`}
                    >
                      {s.name}
                    </button>
                  ))}
                </div>
              </div>
              <input
                placeholder={t("networkTest.targetsPlaceholder")}
                value={meshTargets}
                onChange={(e) => setMeshTargets(e.target.value)}
                className={inputCls}
              />
              <select value={meshMode} onChange={(e) => setMeshMode(e.target.value)} className={inputCls}>
                <option value="all_to_all">{t("networkTest.modeAllToAll")}</option>
                <option value="one_to_all">{t("networkTest.modeOneToAll")}</option>
              </select>
              <button
                onClick={() =>
                  meshMut.mutate({
                    source_ids: meshSources,
                    targets: meshTargets.split(",").map((x) => x.trim()).filter(Boolean),
                    mode: meshMode,
                  })
                }
                disabled={meshSources.length === 0 || !meshTargets.trim() || meshMut.isPending}
                className="flex items-center justify-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-white hover:opacity-90 disabled:opacity-40"
              >
                <Send className="h-4 w-4" />
                {meshMut.isPending ? t("networkTest.running") : t("networkTest.start")}
              </button>
            </div>
          </div>
          {meshMut.data && (
            <div className="space-y-4">
              {meshMut.data.results.map((item, i) => (
                <div key={i} className="rounded-xl border border-border bg-panel p-4">
                  <p className="mb-2 text-sm font-medium">
                    {item.source_name} <span className="text-muted">→ {item.target}</span>
                  </p>
                  <TraceTable trace={item.trace} target={item.target} />
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* 带宽测速 */}
      {tab === "bandwidth" && (
        <div className="space-y-4">
          <div className="rounded-xl border border-border bg-panel p-4">
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-4">
              <select value={bwSource} onChange={(e) => setBwSource(Number(e.target.value))} className={inputCls}>
                <option value={0}>{t("networkTest.pickSource")}</option>
                {servers.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name}
                  </option>
                ))}
              </select>
              <input
                placeholder={t("networkTest.bandwidthTargetPlaceholder")}
                value={bwTarget}
                onChange={(e) => setBwTarget(e.target.value)}
                className={inputCls}
              />
              <input
                type="number"
                min={1}
                max={60}
                value={bwDuration}
                onChange={(e) => setBwDuration(Math.max(1, Math.min(60, Number(e.target.value))))}
                className={inputCls}
                title={t("networkTest.durationTitle")}
              />
              <button
                onClick={() => bwMut.mutate({ source_id: bwSource, target: bwTarget, duration: bwDuration })}
                disabled={!bwSource || !bwTarget || bwMut.isPending}
                className="flex items-center justify-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-white hover:opacity-90 disabled:opacity-40"
              >
                <Send className="h-4 w-4" />
                {bwMut.isPending ? t("networkTest.running") : t("networkTest.start")}
              </button>
            </div>
            <p className="mt-2 text-xs text-muted">{t("networkTest.bandwidthHint")}</p>
          </div>
          {bwMut.data && (
            <div className="rounded-xl border border-border bg-panel p-4">
              {bwMut.data.result.ok ? (
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
                  <div>
                    <p className="text-xs text-muted">{t("networkTest.throughput")}</p>
                    <p className="text-2xl font-semibold tabular-nums">{formatBps(bwMut.data.result.bits_per_sec)}</p>
                  </div>
                  <div>
                    <p className="text-xs text-muted">{t("networkTest.bytesSent")}</p>
                    <p className="text-2xl font-semibold tabular-nums">
                      {(bwMut.data.result.bytes_sent ?? 0) / 1048576 > 1
                        ? `${((bwMut.data.result.bytes_sent ?? 0) / 1048576).toFixed(1)} MiB`
                        : `${((bwMut.data.result.bytes_sent ?? 0) / 1024).toFixed(1)} KiB`}
                    </p>
                  </div>
                  <div>
                    <p className="text-xs text-muted">{t("networkTest.duration")}</p>
                    <p className="text-2xl font-semibold tabular-nums">{((bwMut.data.result.duration_ms ?? 0) / 1000).toFixed(1)} s</p>
                  </div>
                  <p className="text-xs text-muted sm:col-span-3">
                    {t("networkTest.sourceTarget", { source: bwMut.data.source_name, target: bwMut.data.target })}
                  </p>
                </div>
              ) : (
                <p className="text-sm text-err">{t("networkTest.bandwidthError", { error: bwMut.data.result.error ?? "" })}</p>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
