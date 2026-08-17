// 指标对比（P2）：多选服务器 + 单指标，叠加绘制多台服务器的历史曲线。
import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { Search } from "lucide-react";
import { api, type CompareSeries, type MetricPoint, type Server } from "../lib/api";
import { fmtSpeed } from "../lib/format";
import { useI18n, type TKey } from "../lib/i18n";

// 与后端 maxCompareServers 一致
const MAX_SERVERS = 10;

type Period = "1h" | "24h" | "7d";

const PERIODS: { key: Period; label: TKey }[] = [
  { key: "1h", label: "serverDetail.period1h" },
  { key: "24h", label: "serverDetail.period24h" },
  { key: "7d", label: "serverDetail.period7d" },
];

type MetricKey = "cpu" | "mem" | "disk" | "net_in" | "net_out" | "load1";

interface MetricDef {
  key: MetricKey;
  label: TKey;
  color: string;
  unit: (v: number) => string;
  /** 从指标点取值（内存/磁盘换算为使用率百分比）。 */
  value: (p: MetricPoint) => number | null;
}

const METRICS: MetricDef[] = [
  { key: "cpu", label: "compare.metricCpu", color: "#6366f1", unit: (v) => `${v.toFixed(0)}%`, value: (p) => p.cpu },
  { key: "mem", label: "compare.metricMem", color: "#f59e0b", unit: (v) => `${v.toFixed(1)}%`, value: (p) => (p.mem_total > 0 ? (p.mem_used / p.mem_total) * 100 : null) },
  { key: "disk", label: "compare.metricDisk", color: "#22c55e", unit: (v) => `${v.toFixed(1)}%`, value: (p) => (p.disk_total > 0 ? (p.disk_used / p.disk_total) * 100 : null) },
  { key: "net_in", label: "compare.metricNetIn", color: "#06b6d4", unit: fmtSpeed, value: (p) => p.net_in },
  { key: "net_out", label: "compare.metricNetOut", color: "#ef4444", unit: fmtSpeed, value: (p) => p.net_out },
  { key: "load1", label: "compare.metricLoad", color: "#8b5cf6", unit: (v) => v.toFixed(2), value: (p) => p.load1 },
];

// 服务器曲线配色（10 台上限）
const SERVER_COLORS = ["#6366f1", "#f59e0b", "#22c55e", "#06b6d4", "#ef4444", "#8b5cf6", "#ec4899", "#84cc16", "#14b8a6", "#f97316"];

export default function Compare() {
  const { t, tErr, fmtDateTime, fmtTime, fmtDate } = useI18n();
  const { data: listData } = useQuery({ queryKey: ["servers-public"], queryFn: api.servers });
  const servers = listData?.servers ?? [];

  const [selected, setSelected] = useState<number[]>([]);
  const [metricKey, setMetricKey] = useState<MetricKey>("cpu");
  const [period, setPeriod] = useState<Period>("24h");
  const [query, setQuery] = useState("");
  const [limitHit, setLimitHit] = useState(false);
  const initialized = useRef(false);

  // 初始默认勾选前 MAX_SERVERS 台，开箱即有曲线可看
  useEffect(() => {
    if (initialized.current || servers.length === 0) return;
    initialized.current = true;
    setSelected(servers.slice(0, MAX_SERVERS).map((s) => s.id));
  }, [servers]);

  const toggle = (id: number) => {
    if (selected.includes(id)) {
      setSelected(selected.filter((x) => x !== id));
      setLimitHit(false);
      return;
    }
    if (selected.length >= MAX_SERVERS) {
      setLimitHit(true);
      return;
    }
    setSelected([...selected, id]);
    setLimitHit(false);
  };

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return servers;
    return servers.filter((s) => s.name.toLowerCase().includes(q) || String(s.id) === q);
  }, [servers, query]);

  const metric = METRICS.find((m) => m.key === metricKey)!;
  const idsKey = selected.join(",");

  const { data, isFetching, error } = useQuery({
    queryKey: ["metrics-compare", idsKey, period],
    queryFn: () => api.metricsCompare(selected, period),
    enabled: selected.length > 0,
    refetchInterval: 60000,
  });

  // 按 ts 对齐合并为单数据集：每行 { ts, [server_id]: 值 }，缺桶的服务器留空（connectNulls 衔接）
  const chartData = useMemo(() => {
    if (!data) return [];
    const byTs = new Map<number, Record<number, number | null>>();
    for (const s of data.series) {
      for (const p of s.points) {
        let row = byTs.get(p.ts);
        if (!row) {
          row = {};
          byTs.set(p.ts, row);
        }
        row[s.server_id] = metric.value(p);
      }
    }
    return [...byTs.entries()]
      .sort((a, b) => a[0] - b[0])
      .map(([ts, vals]) => ({ ts, ...vals }));
  }, [data, metric]);

  const hasData = (data?.series ?? []).some((s) => s.points.length > 0);
  const pctMetric = metricKey === "cpu" || metricKey === "mem" || metricKey === "disk";

  return (
    <div>
      <div className="mb-4">
        <h1 className="text-xl font-semibold">{t("compare.title")}</h1>
        <p className="text-xs text-muted">{t("compare.subtitle")}</p>
      </div>

      <div className="mb-4 grid grid-cols-1 gap-4 xl:grid-cols-3">
        {/* 服务器多选 */}
        <div className="rounded-xl border border-border bg-panel p-4">
          <div className="mb-2 flex items-center justify-between">
            <span className="text-sm font-medium">{t("compare.servers")}</span>
            <span className="text-xs text-muted">{t("compare.selectedCount", { count: selected.length, max: MAX_SERVERS })}</span>
          </div>
          <div className="relative mb-2">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t("common.search")}
              className="w-full rounded-lg border border-border bg-transparent py-1.5 pl-8 pr-3 text-sm outline-none focus:border-accent"
            />
          </div>
          <div className="max-h-56 space-y-1 overflow-y-auto pr-1">
            {filtered.length === 0 && <div className="py-4 text-center text-xs text-muted">{t("compare.noServers")}</div>}
            {filtered.map((s: Server) => (
              <label key={s.id} className="flex cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-sm hover:bg-black/5 dark:hover:bg-white/5">
                <input
                  type="checkbox"
                  checked={selected.includes(s.id)}
                  onChange={() => toggle(s.id)}
                  className="h-3.5 w-3.5 accent-accent"
                />
                <span className={`h-2 w-2 shrink-0 rounded-full ${s.online ? "bg-ok" : "bg-err"}`} />
                <span className="flex-1 truncate">{s.name || `#${s.id}`}</span>
                {s.group && <span className="shrink-0 rounded bg-black/5 px-1.5 py-0.5 text-[10px] text-muted dark:bg-white/10">{s.group}</span>}
              </label>
            ))}
          </div>
          {limitHit && <div className="mt-2 text-xs text-warn">{t("compare.maxServers")}</div>}
        </div>

        {/* 指标 + 时间范围 */}
        <div className="rounded-xl border border-border bg-panel p-4 xl:col-span-2">
          <div className="mb-3">
            <div className="mb-2 text-sm font-medium">{t("compare.metric")}</div>
            <div className="flex flex-wrap gap-2">
              {METRICS.map((m) => (
                <button
                  key={m.key}
                  onClick={() => setMetricKey(m.key)}
                  className={`flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm ${
                    metricKey === m.key ? "bg-accent text-white" : "border border-border text-muted hover:text-fg"
                  }`}
                >
                  <span className="h-2 w-2 rounded-full" style={{ background: m.color }} />
                  {t(m.label)}
                </button>
              ))}
            </div>
          </div>
          <div>
            <div className="mb-2 text-sm font-medium">{t("compare.period")}</div>
            <div className="flex flex-wrap gap-2">
              {PERIODS.map((p) => (
                <button
                  key={p.key}
                  onClick={() => setPeriod(p.key)}
                  className={`rounded-lg px-3 py-1.5 text-sm ${
                    period === p.key ? "bg-accent text-white" : "border border-border text-muted hover:text-fg"
                  }`}
                >
                  {t(p.label)}
                </button>
              ))}
            </div>
          </div>
        </div>
      </div>

      {/* 叠加曲线 */}
      <div className="rounded-xl border border-border bg-panel p-4">
        <div className="mb-2 flex items-center justify-between">
          <span className="text-sm font-medium">{t(metric.label)}</span>
          {isFetching && <span className="text-xs text-muted">{t("common.loading")}</span>}
        </div>
        {error ? (
          <div className="py-10 text-center text-sm text-err">{tErr(error)}</div>
        ) : selected.length === 0 ? (
          <div className="py-10 text-center text-sm text-muted">{t("compare.noSelection")}</div>
        ) : !hasData ? (
          <div className="py-10 text-center text-sm text-muted">{t("compare.empty")}</div>
        ) : (
          <div className="h-96">
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={chartData} margin={{ top: 5, right: 10, bottom: 0, left: -10 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="var(--border-c)" />
                <XAxis
                  dataKey="ts"
                  tickFormatter={(v: number) => (period === "7d" ? fmtDate(v, { month: "short", day: "numeric" }) : fmtTime(v))}
                  tick={{ fontSize: 11, fill: "var(--muted)" }}
                />
                <YAxis
                  tick={{ fontSize: 11, fill: "var(--muted)" }}
                  tickFormatter={(v: number) => metric.unit(v)}
                  width={70}
                  domain={pctMetric ? [0, 100] : undefined}
                />
                <Tooltip
                  labelFormatter={(v: number) => fmtDateTime(v)}
                  formatter={(value, name) => [metric.unit(Number(value)), name]}
                  contentStyle={{ background: "var(--panel)", border: "1px solid var(--border-c)", borderRadius: 8 }}
                />
                <Legend wrapperStyle={{ fontSize: 12 }} />
                {data?.series.map((s: CompareSeries, i: number) => (
                  <Line
                    key={s.server_id}
                    type="monotone"
                    dataKey={String(s.server_id)}
                    name={s.server_name || `#${s.server_id}`}
                    stroke={SERVER_COLORS[i % SERVER_COLORS.length]}
                    strokeWidth={1.8}
                    dot={false}
                    connectNulls
                    isAnimationActive={false}
                  />
                ))}
              </LineChart>
            </ResponsiveContainer>
          </div>
        )}
      </div>
    </div>
  );
}
