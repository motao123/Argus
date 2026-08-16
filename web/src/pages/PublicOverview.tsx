// 前台总览（公开，借鉴 komari 前台 + dash-v2 游客模式）
import { useMemo, useState } from "react";
import { Search, Server as ServerIcon, Wifi, WifiOff, ArrowUpDown } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import ServerCard from "../components/ServerCard";
import { useServers } from "../context/servers";
import { api } from "../lib/api";
import { fmtBytes } from "../lib/format";

type SortKey =
  | "default" | "name" | "cpu" | "mem" | "disk" | "load"
  | "net_in" | "net_out" | "uptime" | "platform";

const sortOptions: { key: SortKey; label: string }[] = [
  { key: "default", label: "默认" },
  { key: "name", label: "名称" },
  { key: "cpu", label: "CPU" },
  { key: "mem", label: "内存" },
  { key: "disk", label: "磁盘" },
  { key: "load", label: "负载" },
  { key: "net_in", label: "下行速率" },
  { key: "net_out", label: "上行速率" },
  { key: "uptime", label: "运行时间" },
  { key: "platform", label: "系统" },
];

// 服务监控状态条（公开展示，借鉴 dash-v2 ServiceTracker 简化版）
function ServiceStatusStrip() {
  const { data } = useQuery({ queryKey: ["services"], queryFn: api.services, refetchInterval: 15000 });
  const services = data?.services ?? [];
  if (services.length === 0) return null;
  return (
    <div className="mb-4 rounded-xl border border-border bg-panel p-3">
      <div className="mb-2 text-xs font-medium text-muted">服务监控状态</div>
      <div className="flex flex-wrap gap-2">
        {services.map((s) => (
          <span
            key={s.id}
            className={`flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs ${
              s.last_up ? "bg-ok/10 text-ok" : "bg-err/10 text-err"
            }`}
            title={`${s.target} · 今日可用率 ${s.today_up_rate.toFixed(1)}%`}
          >
            <span className={`h-1.5 w-1.5 rounded-full ${s.last_up ? "bg-ok" : "bg-err"}`} />
            {s.name}
            <span className="tabular opacity-70">{s.today_up_rate.toFixed(0)}%</span>
          </span>
        ))}
      </div>
    </div>
  );
}

export default function PublicOverview() {
  const { servers: liveServers, wsStatus } = useServers();
  // 合并 REST 列表（含离线）与 WS 实时值：以 REST 为底，WS 覆盖在线服务器实时字段
  const { data: restData } = useQuery({ queryKey: ["servers-public"], queryFn: api.servers, refetchInterval: 30000 });
  const restServers = restData?.servers ?? [];
  const servers = useMemo(() => {
    const liveById = new Map(liveServers.map((s) => [s.id, s]));
    return restServers.map((s) => (liveById.has(s.id) ? { ...s, ...liveById.get(s.id) } : s));
  }, [restServers, liveServers]);
  const online = servers.filter((s) => s.online).length;
  const total = servers.length;
  const [query, setQuery] = useState("");
  const [group, setGroup] = useState("全部");
  const [sortKey, setSortKey] = useState<SortKey>("default");
  const [sortDesc, setSortDesc] = useState(false);
  const [status, setStatus] = useState<"all" | "online" | "offline">("all");

  const groups = useMemo(() => {
    const set = new Set<string>(servers.map((s) => s.group || "默认").filter(Boolean));
    return ["全部", ...Array.from(set)];
  }, [servers]);

  const totalNet = useMemo(() => {
    return servers.reduce(
      (acc, s) => ({ in: acc.in + s.net_in_speed, out: acc.out + s.net_out_speed }),
      { in: 0, out: 0 },
    );
  }, [servers]);

  const filtered = useMemo(() => {
    let list = servers.filter((s) => {
      if (group !== "全部" && (s.group || "默认") !== group) return false;
      if (status === "online" && !s.online) return false;
      if (status === "offline" && s.online) return false;
      if (query && !s.name.toLowerCase().includes(query.toLowerCase())) return false;
      return true;
    });
    if (sortKey !== "default") {
      const dir = sortDesc ? -1 : 1;
      list = [...list].sort((a, b) => {
        let va: number | string = 0;
        let vb: number | string = 0;
        switch (sortKey) {
          case "name": va = a.name; vb = b.name; break;
          case "cpu": va = a.cpu; vb = b.cpu; break;
          case "mem": va = a.mem_total ? a.mem_used / a.mem_total : 0; vb = b.mem_total ? b.mem_used / b.mem_total : 0; break;
          case "disk": va = a.disk_total ? a.disk_used / a.disk_total : 0; vb = b.disk_total ? b.disk_used / b.disk_total : 0; break;
          case "load": va = a.load1; vb = b.load1; break;
          case "net_in": va = a.net_in_speed; vb = b.net_in_speed; break;
          case "net_out": va = a.net_out_speed; vb = b.net_out_speed; break;
          case "uptime": va = a.uptime; vb = b.uptime; break;
          case "platform": va = a.host?.platform ?? ""; vb = b.host?.platform ?? ""; break;
        }
        if (typeof va === "string") return va.localeCompare(vb as string) * dir;
        return ((va as number) - (vb as number)) * dir;
      });
      list.sort((a, b) => Number(b.online) - Number(a.online)); // 在线优先、离线沉底
    } else {
      list = [...list].sort((a, b) => Number(b.online) - Number(a.online));
    }
    return list;
  }, [servers, query, group, sortKey, sortDesc, status]);

  return (
    <div>
      {wsStatus === "reconnecting" && (
        <div className="mb-3 rounded-lg border border-warn/30 bg-warn/10 px-3 py-2 text-xs text-warn">
          实时连接中断，正在重连…
        </div>
      )}
      <div className="mb-5">
        <h1 className="text-xl font-semibold">服务器总览</h1>
        <p className="text-sm text-muted">
          实时监控 <span className="text-ok font-medium">{online}</span> 台在线 / 共 {total} 台
        </p>
      </div>

      {/* 统计卡 */}
      <div className="mb-4 grid grid-cols-2 gap-3 lg:grid-cols-4">
        <div className="rounded-xl border border-border bg-panel p-4">
          <div className="flex items-center gap-2 text-xs text-muted">
            <ServerIcon className="h-4 w-4" /> 总服务器
          </div>
          <div className="tabular mt-1 text-2xl font-semibold">{total}</div>
        </div>
        <button
          onClick={() => setStatus(status === "online" ? "all" : "online")}
          className={`rounded-xl border p-4 text-left ${status === "online" ? "border-ok/40 bg-ok/5" : "border-border bg-panel"} hover:bg-black/2 dark:hover:bg-white/2`}
        >
          <div className="flex items-center gap-2 text-xs text-muted">
            <Wifi className="h-4 w-4 text-ok" /> 在线
          </div>
          <div className="tabular mt-1 text-2xl font-semibold">{online}</div>
        </button>
        <button
          onClick={() => setStatus(status === "offline" ? "all" : "offline")}
          className={`rounded-xl border p-4 text-left ${status === "offline" ? "border-err/40 bg-err/5" : "border-border bg-panel"} hover:bg-black/2 dark:hover:bg-white/2`}
        >
          <div className="flex items-center gap-2 text-xs text-muted">
            <WifiOff className="h-4 w-4 text-err" /> 离线
          </div>
          <div className="tabular mt-1 text-2xl font-semibold">{total - online}</div>
        </button>
        <div className="rounded-xl border border-border bg-panel p-4">
          <div className="flex items-center gap-2 text-xs text-muted">
            <ArrowUpDown className="h-4 w-4" /> 实时流量
          </div>
          <div className="tabular mt-1 text-lg font-semibold">
            ↓ {fmtBytes(totalNet.in)}/s <span className="text-muted">·</span> ↑ {fmtBytes(totalNet.out)}/s
          </div>
        </div>
      </div>

      {/* 服务监控状态条（公开） */}
      <ServiceStatusStrip />

      {/* 工具条 */}
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <div className="flex overflow-hidden rounded-lg border border-border">
          {(["all", "online", "offline"] as const).map((k) => (
            <button
              key={k}
              onClick={() => setStatus(k)}
              className={`px-3 py-1.5 text-sm ${status === k ? "bg-accent text-white" : "bg-panel text-muted hover:text-fg"}`}
            >
              {k === "all" ? "全部" : k === "online" ? "在线" : "离线"}
            </button>
          ))}
        </div>
        <select
          value={group}
          onChange={(e) => setGroup(e.target.value)}
          className="rounded-lg border border-border bg-panel px-3 py-1.5 text-sm outline-none"
        >
          {groups.map((g) => (
            <option key={g}>{g}</option>
          ))}
        </select>
        <select
          value={sortKey}
          onChange={(e) => setSortKey(e.target.value as SortKey)}
          className="rounded-lg border border-border bg-panel px-3 py-1.5 text-sm outline-none"
        >
          {sortOptions.map((o) => (
            <option key={o.key} value={o.key}>
              {o.label}
            </option>
          ))}
        </select>
        <button
          onClick={() => setSortDesc(!sortDesc)}
          className={`rounded-lg border px-3 py-1.5 text-sm ${sortDesc ? "border-accent text-accent" : "border-border text-muted hover:text-fg"}`}
        >
          {sortDesc ? "降序 ↓" : "升序 ↑"}
        </button>
        <div className="relative ml-auto">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="搜索服务器…"
            className="w-44 rounded-lg border border-border bg-panel py-2 pl-9 pr-3 text-sm outline-none focus:border-accent"
          />
        </div>
      </div>

      {filtered.length === 0 ? (
        <div className="rounded-xl border border-dashed border-border py-20 text-center text-sm text-muted">
          {servers.length === 0 ? "暂无服务器数据" : "没有匹配的服务器"}
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
          {filtered.map((s) => (
            <ServerCard key={s.id} server={s} />
          ))}
        </div>
      )}
    </div>
  );
}
