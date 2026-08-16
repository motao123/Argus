import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { LogOut, MonitorSmartphone } from "lucide-react";
import { api, type Session } from "../lib/api";
import { fmtDateTime } from "../lib/format";

export default function Sessions() {
  const qc = useQueryClient();
  const { data } = useQuery({ queryKey: ["sessions"], queryFn: api.sessions, refetchInterval: 10000 });
  const sessions = data?.sessions ?? [];

  const kick = useMutation({
    mutationFn: api.kickSession,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["sessions"] }),
  });

  return (
    <div>
      <h1 className="mb-1 text-xl font-semibold">在线会话</h1>
      <p className="mb-5 text-sm text-muted">当前登录会话（10s 自动刷新），可强制踢出（借鉴 nezha 在线用户管理）</p>

      <div className="overflow-hidden rounded-xl border border-border bg-panel">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted">
              <th className="px-4 py-2.5 font-normal">ID</th>
              <th className="px-4 py-2.5 font-normal">用户</th>
              <th className="px-4 py-2.5 font-normal">IP</th>
              <th className="px-4 py-2.5 font-normal">设备</th>
              <th className="px-4 py-2.5 font-normal">登录时间</th>
              <th className="px-4 py-2.5 font-normal">过期时间</th>
              <th className="px-4 py-2.5 text-right font-normal">操作</th>
            </tr>
          </thead>
          <tbody>
            {sessions.map((s: Session) => (
              <tr key={s.id} className="border-b border-border last:border-0">
                <td className="px-4 py-2.5 tabular text-muted">#{s.id}</td>
                <td className="px-4 py-2.5 font-medium">#{s.user_id}</td>
                <td className="px-4 py-2.5 tabular text-xs">{s.ip}</td>
                <td className="max-w-[220px] truncate px-4 py-2.5 text-xs text-muted" title={s.user_agent}>
                  {s.user_agent || "—"}
                </td>
                <td className="px-4 py-2.5 text-xs text-muted">{fmtDateTime(s.created_at)}</td>
                <td className="px-4 py-2.5 text-xs text-muted">{fmtDateTime(s.expires_at)}</td>
                <td className="px-4 py-2.5 text-right">
                  <button
                    onClick={() => confirm(`踢出会话 #${s.id}？` ) && kick.mutate(s.id)}
                    className="flex items-center gap-1 rounded p-1.5 text-err hover:bg-err/10"
                    title="踢出会话"
                  >
                    <LogOut className="h-4 w-4" />
                  </button>
                </td>
              </tr>
            ))}
            {sessions.length === 0 && (
              <tr>
                <td colSpan={7} className="px-4 py-8 text-center text-muted">
                  <MonitorSmartphone className="mx-auto mb-2 h-6 w-6 opacity-40" />
                  暂无会话
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
