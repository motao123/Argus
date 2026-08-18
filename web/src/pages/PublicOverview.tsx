// 前台总览（公开，借鉴 komari 前台 + dash-v2 游客模式）
import { useMemo, useState } from "react";
import { Search, Server as ServerIcon, Wifi, WifiOff, ArrowUpDown } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import ServerCard from "../components/ServerCard";
import { StatusBoard } from "../components/StatusBoard";
import WorldMap from "../components/WorldMap";
import { useServers } from "../context/servers";
import { api } from "../lib/api";
import { fmtBytes } from "../lib/format";
import { useI18n, type TKey } from "../lib/i18n";
import { useScrollRestore } from "../lib/scroll-restore";

type SortKey =
  | "default" | "name" | "cpu" | "mem" | "disk" | "load"
  | "net_in" | "net_out" | "uptime" | "platform";

const sortOptions: { key: SortKey; label: TKey }[] = [
  { key: "default", label: "overview.sortDefault" },
  { key: "name", label: "overview.sortName" },
  { key: "cpu", label: "overview.sortCpu" },
  { key: "mem", label: "overview.sortMem" },
  { key: "disk", label: "overview.sortDisk" },
  { key: "load", label: "overview.sortLoad" },
  { key: "net_in", label: "overview.sortNetIn" },
  { key: "net_out", label: "overview.sortNetOut" },
  { key: "uptime", label: "overview.sortUptime" },
  { key: "platform", label: "overview.sortPlatform" },
];

// 服务监控状态条（公开展示，借鉴 dash-v2 ServiceTracker 简化版）
function ServiceStatusStrip() {
  const { t } = useI18n();
  const { data } = useQuery({ queryKey: ["services"], queryFn: api.services, refetchInterval: 15000 });
  const services = data?.services ?? [];
  if (services.length === 0) return null;
  return (
    <div className="mb-4 rounded-xl border border-border bg-panel p-3">
      <div className="mb-2 text-xs font-medium text-muted">{t("services.statusTitle")}</div>
      <div className="flex flex-wrap gap-2">
        {services.map((s) => (
          <span
            key={s.id}
            className={`flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs ${
              s.last_up === null ? "bg-black/5 text-muted" : s.last_up ? "bg-ok/10 text-ok" : "bg-err/10 text-err"
            }`}
            title={`${s.target} · ${t("services.todayRate")} ${s.today_up_rate === null ? t("common.unknown") : `${s.today_up_rate.toFixed(1)}%`}`}
          >
            <span className={`h-1.5 w-1.5 rounded-full ${s.last_up === null ? "bg-muted" : s.last_up ? "bg-ok" : "bg-err"}`} />
            {s.name}
            <span className="tabular opacity-70">{s.today_up_rate === null ? "—" : `${s.today_up_rate.toFixed(0)}%`}</span>
          </span>
        ))}
      </div>
    </div>
  );
}

export default function PublicOverview() {
  const { servers: liveServers, wsStatus } = useServers();
  const { t, fmtNumber } = useI18n();
  useScrollRestore();
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
  const [group, setGroup] = useState("");
  const [sortKey, setSortKey] = useState<SortKey>("default");
  const [sortDesc, setSortDesc] = useState(false);
  const [status, setStatus] = useState<"all" | "online" | "offline">("all");

  const allLabel = t("common.all");
  const defaultGroupLabel = t("common.default");
  const groups = useMemo(() => {
    const set = new Set<string>(servers.map((s) => s.group || defaultGroupLabel).filter(Boolean));
    return Array.from(set);
  }, [servers, defaultGroupLabel]);

  const totalNet = useMemo(() => {
    return servers.reduce(
      (acc, s) => ({ in: acc.in + s.net_in_speed, out: acc.out + s.net_out_speed }),
      { in: 0, out: 0 },
    );
  }, [servers]);

  const filtered = useMemo(() => {
    let list = servers.filter((s) => {
      if (group !== "" && (s.group || defaultGroupLabel) !== group) return false;
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
  }, [servers, query, group, sortKey, sortDesc, status, defaultGroupLabel]);

  return (
    <div>
      <WorldMap servers={servers} />
      {wsStatus === "reconnecting" && (
        <div className="mb-3 rounded-lg border border-warn/30 bg-warn/10 px-3 py-2 text-xs text-warn">
          {t("overview.wsReconnecting")}
        </div>
      )}
      <div className="mb-5">
        <h1 className="text-xl font-semibold">{t("overview.title")}</h1>
        <p className="text-sm text-muted">
          {t("overview.publicSubtitle", { online: fmtNumber(online), total: fmtNumber(total) })}
        </p>
      </div>

      {/* 统计卡 */}
      <div className="mb-4 grid grid-cols-2 gap-3 lg:grid-cols-4">
        <div className="rounded-xl border border-border bg-panel p-4">
          <div className="flex items-center gap-2 text-xs text-muted">
            <ServerIcon className="h-4 w-4" /> {t("overview.statTotal")}
          </div>
          <div className="tabular mt-1 text-2xl font-semibold">{fmtNumber(total)}</div>
        </div>
        <button
          onClick={() => setStatus(status === "online" ? "all" : "online")}
          className={`rounded-xl border p-4 text-left ${status === "online" ? "border-ok/40 bg-ok/5" : "border-border bg-panel"} hover:bg-black/2 dark:hover:bg-white/2`}
        >
          <div className="flex items-center gap-2 text-xs text-muted">
            <Wifi className="h-4 w-4 text-ok" /> {t("common.online")}
          </div>
          <div className="tabular mt-1 text-2xl font-semibold">{fmtNumber(online)}</div>
        </button>
        <button
          onClick={() => setStatus(status === "offline" ? "all" : "offline")}
          className={`rounded-xl border p-4 text-left ${status === "offline" ? "border-err/40 bg-err/5" : "border-border bg-panel"} hover:bg-black/2 dark:hover:bg-white/2`}
        >
          <div className="flex items-center gap-2 text-xs text-muted">
            <WifiOff className="h-4 w-4 text-err" /> {t("common.offline")}
          </div>
          <div className="tabular mt-1 text-2xl font-semibold">{fmtNumber(total - online)}</div>
        </button>
        <div className="rounded-xl border border-border bg-panel p-4">
          <div className="flex items-center gap-2 text-xs text-muted">
            <ArrowUpDown className="h-4 w-4" /> {t("overview.statTraffic")}
          </div>
          <div className="tabular mt-1 text-lg font-semibold">
            ↓ {fmtBytes(totalNet.in)}/s <span className="text-muted">·</span> ↑ {fmtBytes(totalNet.out)}/s
          </div>
        </div>
      </div>

      {/* 服务监控状态条（公开） */}
      <ServiceStatusStrip />

      {/* 状态板：当前维护横幅 + 事故时间线（公开） */}
      <StatusBoard />

      {/* 工具条 */}
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <div className="flex overflow-hidden rounded-lg border border-border">
          {(["all", "online", "offline"] as const).map((k) => (
            <button
              key={k}
              onClick={() => setStatus(k)}
              className={`px-3 py-1.5 text-sm ${status === k ? "bg-accent text-white" : "bg-panel text-muted hover:text-fg"}`}
            >
              {k === "all" ? t("common.all") : k === "online" ? t("common.online") : t("common.offline")}
            </button>
          ))}
        </div>
        <select
          value={group}
          onChange={(e) => setGroup(e.target.value)}
          className="rounded-lg border border-border bg-panel px-3 py-1.5 text-sm outline-none"
        >
          <option value="">{allLabel}</option>
          {groups.map((g) => (
            <option key={g} value={g}>{g}</option>
          ))}
        </select>
        <select
          value={sortKey}
          onChange={(e) => setSortKey(e.target.value as SortKey)}
          className="rounded-lg border border-border bg-panel px-3 py-1.5 text-sm outline-none"
        >
          {sortOptions.map((o) => (
            <option key={o.key} value={o.key}>
              {t(o.label)}
            </option>
          ))}
        </select>
        <button
          onClick={() => setSortDesc(!sortDesc)}
          className={`rounded-lg border px-3 py-1.5 text-sm ${sortDesc ? "border-accent text-accent" : "border-border text-muted hover:text-fg"}`}
        >
          {sortDesc ? t("overview.sortDesc") : t("overview.sortAsc")}
        </button>
        <div className="relative ml-auto">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t("common.search")}
            className="w-44 rounded-lg border border-border bg-panel py-2 pl-9 pr-3 text-sm outline-none focus:border-accent"
          />
        </div>
      </div>

      {filtered.length === 0 ? (
        <div className="rounded-xl border border-dashed border-border py-20 text-center text-sm text-muted">
          {servers.length === 0 ? t("overview.emptyPublic") : t("common.noMatch")}
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
