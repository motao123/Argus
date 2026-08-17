import { useMemo } from "react";
import { useParams, Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, TerminalSquare } from "lucide-react";
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { useState } from "react";
import { SlaCard } from "../components/SlaCard";
import { api, getToken, type MetricPoint, type TrafficPoint } from "../lib/api";
import { useServers } from "../context/servers";
import { fmtBytes, fmtSpeed } from "../lib/format";
import { useI18n, type TKey } from "../lib/i18n";

// 周期流量卡（24h/30d/12m，借鉴 dash-v2 CycleTransferStats）
function CycleTraffic({ serverId }: { serverId: number }) {
  const { t, fmtDateTime, fmtTime, fmtDate } = useI18n();
  const [period, setPeriod] = useState<"day" | "month" | "year">("day");
  const { data } = useQuery({
    queryKey: ["traffic", serverId, period],
    queryFn: () => api.serverTraffic(serverId, period),
    refetchInterval: 60000,
  });
  const points = data?.points ?? [];
  const usage = data?.usage;
  const maxV = Math.max(1, ...points.map((p) => p.in + p.out));
  const label = (ts: number) =>
    period === "day" ? fmtTime(ts) : fmtDate(ts, { month: "short", day: "numeric" });
  return (
    <div className="mb-4 rounded-xl border border-border bg-panel p-4">
      <div className="mb-2 flex items-center justify-between">
        <span className="text-sm font-medium">{t("serverDetail.cycleTraffic")}</span>
        <div className="flex gap-1 text-xs">
          {([["day", t("serverDetail.periodDay")], ["month", t("serverDetail.periodMonth")], ["year", t("serverDetail.periodYear")]] as const).map(([k, labelText]) => (
            <button
              key={k}
              onClick={() => setPeriod(k)}
              className={`rounded-full px-2.5 py-1 ${period === k ? "bg-accent text-white" : "bg-black/5 dark:bg-white/10 text-muted"}`}
            >
              {labelText}
            </button>
          ))}
        </div>
      </div>
      {usage && (
        <div className="mb-3 space-y-2 text-xs text-muted">
          <div>{fmtDateTime(usage.cycle_start)} — {fmtDateTime(usage.cycle_end)} · {usage.timezone}</div>
          <div className="flex flex-wrap gap-4">
            <span>↓ {fmtBytes(usage.in_bytes)}</span><span>↑ {fmtBytes(usage.out_bytes)}</span>
            <span>{t("serverDetail.accounted", { bytes: fmtBytes(usage.accounted_bytes) })}</span>
            {usage.quota_bytes > 0 && <><span>{t("serverDetail.remaining", { bytes: fmtBytes(usage.remaining_bytes) })}</span><span>{usage.percentage?.toFixed(1)}%</span></>}
          </div>
          {usage.quota_bytes > 0 && <div className="h-2 overflow-hidden rounded-full bg-black/5 dark:bg-white/10"><div className="h-full bg-accent" style={{ width: `${Math.min(100, usage.percentage ?? 0)}%` }} /></div>}
        </div>
      )}
      <div className="flex h-24 items-end gap-[2px]">
        {points.map((p: TrafficPoint) => (
          <div key={p.ts} className="group relative flex-1" title={`${label(p.ts)}\n↓ ${fmtBytes(p.in)} ↑ ${fmtBytes(p.out)}`}>
            <div className="flex h-full w-full flex-col justify-end gap-px">
              <div className="w-full rounded-t-sm bg-ok/70 transition-all" style={{ height: `${(p.in / maxV) * 100}%` }} />
              <div className="w-full rounded-t-sm bg-accent/70 transition-all" style={{ height: `${(p.out / maxV) * 100}%` }} />
            </div>
          </div>
        ))}
        {points.length === 0 && <div className="w-full text-center text-xs text-muted">{t("serverDetail.noTraffic")}</div>}
      </div>
    </div>
  );
}

const periods: { key: "1h" | "24h" | "7d"; label: TKey }[] = [
  { key: "1h", label: "serverDetail.period1h" },
  { key: "24h", label: "serverDetail.period24h" },
  { key: "7d", label: "serverDetail.period7d" },
];

function MetricChart({
  points,
  dataKey,
  name,
  color,
  unit,
}: {
  points: MetricPoint[];
  dataKey: keyof MetricPoint;
  name: string;
  color: string;
  unit: (v: number) => string;
}) {
  const { fmtDateTime } = useI18n();
  return (
    <div className="rounded-xl border border-border bg-panel p-4">
      <div className="mb-2 text-sm font-medium">{name}</div>
      <div className="h-48">
        <ResponsiveContainer width="100%" height="100%">
          <LineChart data={points} margin={{ top: 5, right: 5, bottom: 0, left: -10 }}>
            <CartesianGrid strokeDasharray="3 3" stroke="var(--border-c)" />
            <XAxis dataKey="ts" tickFormatter={(v: number) => fmtDateTime(v)} tick={{ fontSize: 11, fill: "var(--muted)" }} />
            <YAxis tick={{ fontSize: 11, fill: "var(--muted)" }} tickFormatter={(v: number) => unit(v)} width={64} />
            <Tooltip
              labelFormatter={(v: number) => fmtDateTime(v)}
              formatter={(v) => [unit(Number(v)), name]}
              contentStyle={{ background: "var(--panel)", border: "1px solid var(--border-c)", borderRadius: 8 }}
            />
            <Line type="monotone" dataKey={dataKey} stroke={color} strokeWidth={1.8} dot={false} isAnimationActive={false} />
          </LineChart>
        </ResponsiveContainer>
      </div>
    </div>
  );
}

export default function ServerDetail() {
  const { id } = useParams();
  const serverId = Number(id);
  const { servers: liveServers } = useServers();
  const { t, fmtDuration } = useI18n();
  const [period, setPeriod] = useState<(typeof periods)[number]["key"]>("1h");

  // 合并 WS 实时与 REST（离线服务器也能打开详情）
  const { data: restData } = useQuery({ queryKey: ["servers-public"], queryFn: api.servers });
  const restServers = restData?.servers ?? [];
  const server = useMemo(() => {
    const live = liveServers.find((s) => s.id === serverId);
    const rest = restServers.find((s) => s.id === serverId);
    if (live && rest) return { ...rest, ...live };
    return live ?? rest;
  }, [liveServers, restServers, serverId]);
  const { data } = useQuery({
    queryKey: ["metrics", serverId, period],
    queryFn: () => api.metrics(serverId, period),
    refetchInterval: period === "1h" ? 30000 : 120000,
  });
  // 实时段拼接：1h 周期时把 WS 快照追加为最新点（借鉴 dash-v2 use-chart-history）
  const points = useMemo(() => {
    const base = data?.points ?? [];
    if (period !== "1h" || !server) return base;
    const last = base.length > 0 ? base[base.length - 1].ts : 0;
    const nowTs = Math.floor(Date.now() / 1000);
    if (nowTs - last < 30) return base; // 历史已含近期点
    return [
      ...base,
      {
        ts: nowTs,
        cpu: server.cpu,
        net_in: server.net_in_speed,
        net_out: server.net_out_speed,
        load1: server.load1,
        mem_used: server.mem_used,
        mem_total: server.mem_total,
        disk_used: server.disk_used,
        disk_total: server.disk_total,
        process_count: server.process_count,
        tcp_established: server.tcp_established,
        tcp_listen: server.tcp_listen,
        udp_count: server.udp_count,
        disk_read_speed: server.disk_read_speed,
        disk_write_speed: server.disk_write_speed,
        disk_read_iops: server.disk_read_iops,
        disk_write_iops: server.disk_write_iops,
      },
    ];
  }, [data, period, server]);

  if (!server) {
    return <div className="text-sm text-muted">{t("serverDetail.notFound")}</div>;
  }

  const unavailable = t("serverDetail.unavailable");
  const temp = server.temperature_availability?.available ? `${server.temperature.toFixed(1)}°C` : unavailable;
  const gpu = server.gpu?.available
    ? t("serverDetail.gpuValue", { count: server.gpu.devices?.length ?? 0, util: server.gpu_util.toFixed(1) })
    : unavailable;
  const process = server.process_availability?.available ? String(server.process_count) : unavailable;
  const tcp = server.socket_availability?.available
    ? t("serverDetail.tcpValue", { established: server.tcp_established, listen: server.tcp_listen })
    : unavailable;
  const udp = server.socket_availability?.available ? String(server.udp_count) : unavailable;
  const diskIo = server.disk_io_availability?.available
    ? t("serverDetail.diskIoValue", { read: fmtSpeed(server.disk_read_speed), write: fmtSpeed(server.disk_write_speed) })
    : unavailable;
  const diskIops = server.disk_io_availability?.available
    ? t("serverDetail.diskIopsValue", { read: server.disk_read_iops.toFixed(1), write: server.disk_write_iops.toFixed(1) })
    : unavailable;

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Link to="/" className="rounded-lg p-2 hover:bg-black/5 dark:hover:bg-white/5">
            <ArrowLeft className="h-4 w-4" />
          </Link>
          <div>
            <h1 className="flex items-center gap-2 text-xl font-semibold">
              {server.name}
              <span className={`h-2.5 w-2.5 rounded-full ${server.online ? "bg-ok" : "bg-err"}`} />
            </h1>
            <p className="text-xs text-muted">
              {server.host?.platform ?? t("serverDetail.unknownPlatform")}
              {server.host?.cpu_model ? ` · ${server.host.cpu_model}` : ""} · {t("serverDetail.uptime", { uptime: fmtDuration(server.uptime) })}
            </p>
          </div>
        </div>
        {getToken() ? (
          <Link
            to={`/admin/terminal/${serverId}`}
            className="flex items-center gap-2 rounded-lg border border-border px-3 py-2 text-sm text-muted hover:text-fg"
            title={server.online ? t("serverDetail.terminalOnline") : t("serverDetail.terminalOffline")}
          >
            <TerminalSquare className="h-4 w-4" />
            {t("serverDetail.terminal")}
          </Link>
        ) : (
          <Link to="/login" className="flex items-center gap-2 rounded-lg border border-border px-3 py-2 text-sm text-muted hover:text-fg">
            <TerminalSquare className="h-4 w-4" />
            {t("serverDetail.loginForTerminal")}
          </Link>
        )}
      </div>

      {/* 实时指标 */}
      <div className="mb-4 grid grid-cols-2 gap-3 lg:grid-cols-4">
        {[
          { label: t("serverDetail.cpu"), value: `${server.cpu.toFixed(1)}%` },
          { label: t("serverDetail.mem"), value: `${fmtBytes(server.mem_used)} / ${fmtBytes(server.mem_total)}` },
          { label: t("serverDetail.disk"), value: `${fmtBytes(server.disk_used)} / ${fmtBytes(server.disk_total)}` },
          { label: t("serverDetail.load"), value: server.load1.toFixed(2) },
          { label: t("serverDetail.latency"), value: server.latency_ms > 0 ? `${server.latency_ms}ms` : "—" },
          { label: t("serverDetail.netIn"), value: fmtSpeed(server.net_in_speed) },
          { label: t("serverDetail.netOut"), value: fmtSpeed(server.net_out_speed) },
          { label: t("serverDetail.temperature"), value: temp },
          { label: t("serverDetail.gpu"), value: gpu },
          { label: t("serverDetail.process"), value: process },
          { label: t("serverDetail.tcp"), value: tcp },
          { label: t("serverDetail.udp"), value: udp },
          { label: t("serverDetail.diskIo"), value: diskIo },
          { label: t("serverDetail.diskIops"), value: diskIops },
          { label: t("serverDetail.group"), value: server.group || t("common.default") },
          { label: t("serverDetail.note"), value: server.note || "—" },
        ].map(({ label, value }) => (
          <div key={label} className="rounded-xl border border-border bg-panel p-3">
            <div className="text-xs text-muted">{label}</div>
            <div className="tabular mt-1 text-lg font-medium">{value}</div>
          </div>
        ))}
      </div>

      <CycleTraffic serverId={serverId} />

      {/* 月度 SLA/SLO（排除维护窗口） */}
      <SlaCard serverId={serverId} />

      {/* 历史图表 */}
      <div className="mb-3 flex gap-2">
        {periods.map((p) => (
          <button
            key={p.key}
            onClick={() => setPeriod(p.key)}
            className={`rounded-lg px-3 py-1.5 text-sm ${
              period === p.key ? "bg-accent text-white" : "bg-panel border border-border text-muted hover:text-fg"
            }`}
          >
            {t(p.label)}
          </button>
        ))}
      </div>
      <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
        <MetricChart points={points} dataKey="cpu" name={t("serverDetail.chartCpu")} color="#6366f1" unit={(v) => `${v.toFixed(0)}%`} />
        <MetricChart points={points} dataKey="load1" name={t("serverDetail.chartLoad")} color="#f59e0b" unit={(v) => v.toFixed(1)} />
        <MetricChart points={points} dataKey="net_in" name={t("serverDetail.netIn")} color="#22c55e" unit={fmtSpeed} />
        <MetricChart points={points} dataKey="net_out" name={t("serverDetail.netOut")} color="#ef4444" unit={fmtSpeed} />
        <MetricChart points={points} dataKey="disk_read_speed" name={t("serverDetail.chartDiskRead")} color="#06b6d4" unit={fmtSpeed} />
        <MetricChart points={points} dataKey="disk_write_speed" name={t("serverDetail.chartDiskWrite")} color="#ec4899" unit={fmtSpeed} />
        <MetricChart points={points} dataKey="disk_read_iops" name={t("serverDetail.chartReadIops")} color="#14b8a6" unit={(v) => v.toFixed(1)} />
        <MetricChart points={points} dataKey="disk_write_iops" name={t("serverDetail.chartWriteIops")} color="#f97316" unit={(v) => v.toFixed(1)} />
        <MetricChart points={points} dataKey="process_count" name={t("serverDetail.chartProcess")} color="#8b5cf6" unit={(v) => v.toFixed(0)} />
        <MetricChart points={points} dataKey="tcp_established" name={t("serverDetail.chartTcpEstablished")} color="#10b981" unit={(v) => v.toFixed(0)} />
        <MetricChart points={points} dataKey="tcp_listen" name={t("serverDetail.chartTcpListen")} color="#84cc16" unit={(v) => v.toFixed(0)} />
        <MetricChart points={points} dataKey="udp_count" name={t("serverDetail.chartUdp")} color="#eab308" unit={(v) => v.toFixed(0)} />
      </div>
    </div>
  );
}
