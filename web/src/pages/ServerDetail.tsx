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
import { api, type MetricPoint } from "../lib/api";
import { useServers } from "../context/servers";
import { fmtBytes, fmtSpeed, fmtTime, fmtUptime } from "../lib/format";

const periods = [
  { key: "1h", label: "1 小时" },
  { key: "24h", label: "24 小时" },
  { key: "7d", label: "7 天" },
] as const;

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
  return (
    <div className="rounded-xl border border-border bg-panel p-4">
      <div className="mb-2 text-sm font-medium">{name}</div>
      <div className="h-48">
        <ResponsiveContainer width="100%" height="100%">
          <LineChart data={points} margin={{ top: 5, right: 5, bottom: 0, left: -10 }}>
            <CartesianGrid strokeDasharray="3 3" stroke="var(--border-c)" />
            <XAxis dataKey="ts" tickFormatter={(v: number) => fmtTime(v)} tick={{ fontSize: 11, fill: "var(--muted)" }} />
            <YAxis tick={{ fontSize: 11, fill: "var(--muted)" }} tickFormatter={(v: number) => unit(v)} width={64} />
            <Tooltip
              labelFormatter={(v: number) => new Date(v * 1000).toLocaleString("zh-CN", { hour12: false })}
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
  const { servers } = useServers();
  const [period, setPeriod] = useState<(typeof periods)[number]["key"]>("1h");

  const server = servers.find((s) => s.id === serverId);
  const { data } = useQuery({
    queryKey: ["metrics", serverId, period],
    queryFn: () => api.metrics(serverId, period),
    refetchInterval: period === "1h" ? 30000 : 120000,
  });
  const points = data?.points ?? [];

  if (!server) {
    return <div className="text-sm text-muted">服务器不存在或已删除</div>;
  }

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
              {server.host?.platform ?? "未知系统"}
              {server.host?.cpu_model ? ` · ${server.host.cpu_model}` : ""} · 已运行 {fmtUptime(server.uptime)}
            </p>
          </div>
        </div>
        <Link
          to={`/terminal/${server.id}`}
          className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-white hover:opacity-90"
        >
          <TerminalSquare className="h-4 w-4" />
          网页终端
        </Link>
      </div>

      {/* 实时指标 */}
      <div className="mb-4 grid grid-cols-2 gap-3 lg:grid-cols-4">
        {[
          { label: "CPU", value: `${server.cpu.toFixed(1)}%` },
          { label: "内存", value: `${fmtBytes(server.mem_used)} / ${fmtBytes(server.mem_total)}` },
          { label: "磁盘", value: `${fmtBytes(server.disk_used)} / ${fmtBytes(server.disk_total)}` },
          { label: "负载", value: server.load1.toFixed(2) },
          { label: "下行速率", value: fmtSpeed(server.net_in_speed) },
          { label: "上行速率", value: fmtSpeed(server.net_out_speed) },
          { label: "分组", value: server.group || "默认" },
          { label: "备注", value: server.note || "—" },
        ].map(({ label, value }) => (
          <div key={label} className="rounded-xl border border-border bg-panel p-3">
            <div className="text-xs text-muted">{label}</div>
            <div className="tabular mt-1 text-lg font-medium">{value}</div>
          </div>
        ))}
      </div>

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
            {p.label}
          </button>
        ))}
      </div>
      <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
        <MetricChart points={points} dataKey="cpu" name="CPU 使用率" color="#6366f1" unit={(v) => `${v.toFixed(0)}%`} />
        <MetricChart points={points} dataKey="load1" name="负载 (1min)" color="#f59e0b" unit={(v) => v.toFixed(1)} />
        <MetricChart points={points} dataKey="net_in" name="下行速率" color="#22c55e" unit={fmtSpeed} />
        <MetricChart points={points} dataKey="net_out" name="上行速率" color="#ef4444" unit={fmtSpeed} />
      </div>
    </div>
  );
}
