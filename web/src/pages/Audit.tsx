import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ScrollText } from "lucide-react";
import { api, type AuditLog } from "../lib/api";
import { fmtDateTime } from "../lib/format";

export default function Audit() {
  const [offset, setOffset] = useState(0);
  const { data, isFetching } = useQuery({
    queryKey: ["audit", offset],
    queryFn: () => api.auditLogs(offset, 50),
  });
  const logs = data?.logs ?? [];
  const total = data?.pagination?.total ?? 0;
  const page = Math.floor(offset / 50);

  return (
    <div>
      <h1 className="mb-1 flex items-center gap-2 text-xl font-semibold">
        <ScrollText className="h-5 w-5 text-accent" /> 审计日志
      </h1>
      <p className="mb-4 text-sm text-muted">管理操作记录（admin），共 {total} 条</p>
      <div className="overflow-x-auto rounded-xl border border-border bg-panel">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted">
              <th className="px-4 py-2.5">时间</th>
              <th className="px-4 py-2.5">用户</th>
              <th className="px-4 py-2.5">动作</th>
              <th className="px-4 py-2.5">详情</th>
              <th className="px-4 py-2.5">IP</th>
            </tr>
          </thead>
          <tbody>
            {logs.map((l: AuditLog) => (
              <tr key={l.id} className="border-b border-border last:border-0">
                <td className="whitespace-nowrap px-4 py-2.5 text-xs text-muted">{fmtDateTime(l.created_at)}</td>
                <td className="px-4 py-2.5">{l.username}</td>
                <td className="whitespace-nowrap px-4 py-2.5 font-mono text-xs text-accent">{l.action}</td>
                <td className="max-w-md truncate px-4 py-2.5 text-muted" title={l.detail}>{l.detail || "—"}</td>
                <td className="whitespace-nowrap px-4 py-2.5 font-mono text-xs text-muted">{l.ip}</td>
              </tr>
            ))}
            {logs.length === 0 && <tr><td colSpan={5} className="px-4 py-8 text-center text-muted">暂无审计记录</td></tr>}
          </tbody>
        </table>
      </div>
      <div className="mt-3 flex items-center gap-3 text-sm">
        <button
          disabled={offset === 0}
          onClick={() => setOffset(Math.max(0, offset - 50))}
          className="rounded-lg border border-border px-3 py-1.5 disabled:opacity-40"
        >
          上一页
        </button>
        <span className="text-muted">第 {page + 1} 页</span>
        <button
          disabled={offset + 50 >= total}
          onClick={() => setOffset(offset + 50)}
          className="rounded-lg border border-border px-3 py-1.5 disabled:opacity-40"
        >
          下一页
        </button>
        {isFetching && <span className="text-xs text-muted">加载中…</span>}
      </div>
    </div>
  );
}
