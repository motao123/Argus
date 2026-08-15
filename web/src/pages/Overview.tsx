import { useMemo, useState } from "react";
import { Search, Server as ServerIcon, Wifi, WifiOff, ArrowUpDown } from "lucide-react";
import ServerCard from "../components/ServerCard";
import { useServers } from "../context/servers";
import { fmtBytes } from "../lib/format";

type SortKey =
  | "default" | "name" | "cpu" | "mem" | "disk" | "load"
  | "net_in" | "net_out" | "uptime" | "platform";
type StatusFilter = "all" | "online" | "offline";

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

export default function Overview() {
  const { servers, online, total } = useServers();
  const [query, setQuery] = useState("");
  const [group, setGroup] = useState("全部");
  const [sortKey, setSortKey] = useState<SortKey>("default");
  const [sortDesc, setSortDesc] = useState(false);
  const [status, setStatus] = useState<StatusFilter>("all");

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
      // 离线永远沉底（借鉴 dash-v2）
      list.sort((a, b) => Number(a.online) - Number(b.online));
    }
    return list;
  }, [servers, query, group, sortKey, sortDesc, status]);

  const offline = total - online;

  return (
    <div>
      <div className="mb-5 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">服务器总览</h1>
          <p className="text-sm text-muted">
            在线 <span className="text-ok font-medium">{online}</span> / 共 {total} 台
          </p>
        </div>
        <div className="flex items-center gap-2">
          <select
            value={group}
            onChange={(e) => setGroup(e.target.value)}
            className="rounded-lg border border-border bg-panel px-3 py-2 text-sm outline-none"
          >
            {groups.map((g) => (
              <option key={g}>{g}</option>
            ))}
          </select>
          <div className="relative">
            <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="搜索服务器…"
              className="w-40 rounded-lg border border-border bg-panel py-2 pl-9 pr-3 text-sm outline-none focus:border-accent"
            />
          </div>
        </div>
      </div>

      {/* 统计卡（借鉴 dash-v2 ServerOverview） */}
      <div className="mb-4 grid grid-cols-2 gap-3 lg:grid-cols-4">
        <button
          onClick={() => setStatus(status === "all" ? "all" : "all")}
          className="rounded-xl border border-border bg-panel p-4 text-left hover:bg-black/2 dark:hover:bg-white/2"
        >
          <div className="flex items-center gap-2 text-xs text-muted">
            <ServerIcon className="h-4 w-4" /> 总服务器
          </div>
          <div className="tabular mt-1 text-2xl font-semibold">{total}</div>
        </button>
        <button
          onClick={() => setStatus(status === "online" ? "all" : "online")}
          className={`rounded-xl border p-4 text-left ${
            status === "online" ? "border-ok/40 bg-ok/5" : "border-border bg-panel"
          } hover:bg-black/2 dark:hover:bg-white/2`}
        >
          <div className="flex items-center gap-2 text-xs text-muted">
            <Wifi className="h-4 w-4 text-ok" /> 在线
          </div>
          <div className="tabular mt-1 text-2xl font-semibold">{online}</div>
        </button>
        <button
          onClick={() => setStatus(status === "offline" ? "all" : "offline")}
          className={`rounded-xl border p-4 text-left ${
            status === "offline" ? "border-err/40 bg-err/5" : "border-border bg-panel"
          } hover:bg-black/2 dark:hover:bg-white/2`}
        >
          <div className="flex items-center gap-2 text-xs text-muted">
            <WifiOff className="h-4 w-4 text-err" /> 离线
          </div>
          <div className="tabular mt-1 text-2xl font-semibold">{offline}</div>
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

      {/* 排序栏 */}
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <div className="flex overflow-hidden rounded-lg border border-border">
          {(["all", "online", "offline"] as StatusFilter[]).map((k) => (
            <button
              key={k}
              onClick={() => setStatus(k)}
              className={`px-3 py-1.5 text-sm transition-colors ${
                status === k ? "bg-accent text-white" : "bg-panel text-muted hover:text-fg"
              }`}
            >
              {k === "all" ? "全部" : k === "online" ? "在线" : "离线"}
            </button>
          ))}
        </div>
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
          className={`rounded-lg border px-3 py-1.5 text-sm ${
            sortDesc ? "border-accent text-accent" : "border-border text-muted hover:text-fg"
          }`}
        >
          {sortDesc ? "降序 ↓" : "升序 ↑"}
        </button>
      </div>

      {filtered.length === 0 ? (
        <div className="rounded-xl border border-dashed border-border py-20 text-center text-sm text-muted">
          {servers.length === 0
            ? "还没有服务器 —— 在「服务器」页创建，或直接部署 agent 自动注册"
            : "没有匹配的服务器"}
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
