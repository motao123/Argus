import { useMemo, useState } from "react";
import { Search } from "lucide-react";
import ServerCard from "../components/ServerCard";
import { useServers } from "../context/servers";

export default function Overview() {
  const { servers, online, total } = useServers();
  const [query, setQuery] = useState("");
  const [group, setGroup] = useState("全部");

  const groups = useMemo(() => {
    const set = new Set<string>(servers.map((s) => s.group || "默认").filter(Boolean));
    return ["全部", ...Array.from(set)];
  }, [servers]);

  const filtered = useMemo(() => {
    return servers.filter((s) => {
      if (group !== "全部" && (s.group || "默认") !== group) return false;
      if (query && !s.name.toLowerCase().includes(query.toLowerCase())) return false;
      return true;
    });
  }, [servers, query, group]);

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
              className="w-48 rounded-lg border border-border bg-panel py-2 pl-9 pr-3 text-sm outline-none focus:border-accent"
            />
          </div>
        </div>
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
