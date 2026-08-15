import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, KeyRound, Plus, ShieldCheck, Trash2 } from "lucide-react";
import { api, type ApiToken, type User } from "../lib/api";
import { fmtDateTime } from "../lib/format";

const allScopes = [
  { key: "argus:server:read", label: "服务器读取" },
  { key: "argus:server:write", label: "服务器写入" },
  { key: "argus:server:delete", label: "服务器删除" },
  { key: "argus:server:exec", label: "远程执行" },
  { key: "argus:service:read", label: "服务监控读取" },
  { key: "argus:service:write", label: "服务监控写入" },
  { key: "argus:alert:read", label: "报警读取" },
  { key: "argus:alert:write", label: "报警写入" },
  { key: "argus:cron:read", label: "任务读取" },
  { key: "argus:cron:write", label: "任务写入" },
  { key: "argus:notification:read", label: "通知读取" },
  { key: "argus:notification:write", label: "通知写入" },
];

export default function Access() {
  const qc = useQueryClient();
  const { data: tokenData } = useQuery({ queryKey: ["tokens"], queryFn: api.tokens });
  const { data: userData } = useQuery({ queryKey: ["users"], queryFn: api.users });
  const tokens = tokenData?.tokens ?? [];
  const users = userData?.users ?? [];

  const [tkForm, setTkForm] = useState<{ name: string; scopes: string[]; expires_in: number } | null>(null);
  const [created, setCreated] = useState<string>("");
  const [userForm, setUserForm] = useState<{ username: string; password: string; role: string } | null>(null);
  const [createdUser, setCreatedUser] = useState<string>("");

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ["tokens"] });
    qc.invalidateQueries({ queryKey: ["users"] });
  };

  const createToken = useMutation({
    mutationFn: (t: { name: string; scopes: string[]; expires_in: number }) => api.createToken(t),
    onSuccess: (r) => {
      setCreated(r.token);
      setTkForm(null);
      invalidate();
    },
  });
  const revoke = useMutation({ mutationFn: api.revokeToken, onSuccess: invalidate });

  const createUser = useMutation({
    mutationFn: (u: { username: string; password: string; role: string }) => api.createUser(u),
    onSuccess: (r) => {
      setCreatedUser(r.agent_secret);
      setUserForm(null);
      invalidate();
    },
  });
  const delUser = useMutation({ mutationFn: api.deleteUser, onSuccess: invalidate });

  return (
    <div>
      <h1 className="mb-1 text-xl font-semibold">访问控制</h1>
      <p className="mb-5 text-sm text-muted">个人访问令牌（PAT）与用户管理</p>

      {/* PAT */}
      <div className="mb-3 flex items-center justify-between">
        <h2 className="flex items-center gap-2 text-lg font-semibold">
          <KeyRound className="h-4 w-4 text-accent" /> 访问令牌
        </h2>
        <button
          onClick={() => setTkForm({ name: "", scopes: ["argus:server:read"], expires_in: 30 })}
          className="flex items-center gap-2 rounded-lg bg-accent px-3 py-1.5 text-sm text-white hover:opacity-90"
        >
          <Plus className="h-4 w-4" />
          创建令牌
        </button>
      </div>

      {created && (
        <div className="mb-4 flex items-center gap-2 rounded-lg bg-ok/10 p-3 text-sm">
          <ShieldCheck className="h-4 w-4 shrink-0 text-ok" />
          <span className="break-all font-mono text-xs">{created}</span>
          <button onClick={() => navigator.clipboard?.writeText(created)} className="ml-auto shrink-0 text-muted hover:text-fg">
            <Copy className="h-4 w-4" />
          </button>
          <button onClick={() => setCreated("")} className="shrink-0 text-muted hover:text-fg">
            ✕
          </button>
        </div>
      )}

      {tkForm && (
        <div className="mb-4 rounded-xl border border-border bg-panel p-4">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <input
              placeholder="令牌名称"
              value={tkForm.name}
              onChange={(e) => setTkForm({ ...tkForm, name: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            />
            <input
              type="number"
              placeholder="有效期（天，0=永久）"
              value={tkForm.expires_in}
              onChange={(e) => setTkForm({ ...tkForm, expires_in: Number(e.target.value) })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            />
          </div>
          <div className="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-4">
            {allScopes.map((sc) => (
              <label key={sc.key} className="flex items-center gap-2 text-xs">
                <input
                  type="checkbox"
                  checked={tkForm.scopes.includes(sc.key)}
                  onChange={(e) =>
                    setTkForm({
                      ...tkForm,
                      scopes: e.target.checked
                        ? [...tkForm.scopes, sc.key]
                        : tkForm.scopes.filter((k) => k !== sc.key),
                    })
                  }
                />
                {sc.label}
              </label>
            ))}
          </div>
          <div className="mt-3 flex gap-2">
            <button
              onClick={() => createToken.mutate(tkForm)}
              disabled={!tkForm.name || tkForm.scopes.length === 0}
              className="rounded-lg bg-accent px-4 py-1.5 text-sm text-white hover:opacity-90 disabled:opacity-40"
            >
              创建（明文仅显示一次）
            </button>
            <button onClick={() => setTkForm(null)} className="rounded-lg border border-border px-4 py-1.5 text-sm text-muted">
              取消
            </button>
          </div>
        </div>
      )}

      <div className="mb-8 overflow-hidden rounded-xl border border-border bg-panel">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted">
              <th className="px-4 py-2.5 font-normal">名称</th>
              <th className="px-4 py-2.5 font-normal">权限</th>
              <th className="px-4 py-2.5 font-normal">过期</th>
              <th className="px-4 py-2.5 font-normal">状态</th>
              <th className="px-4 py-2.5 text-right font-normal">操作</th>
            </tr>
          </thead>
          <tbody>
            {tokens.map((t: ApiToken) => (
              <tr key={t.id} className="border-b border-border last:border-0">
                <td className="px-4 py-2.5 font-medium">{t.name}</td>
                <td className="max-w-xs truncate px-4 py-2.5 text-xs text-muted">{t.scopes}</td>
                <td className="px-4 py-2.5 tabular text-xs text-muted">{t.expires_at ? fmtDateTime(t.expires_at) : "永久"}</td>
                <td className="px-4 py-2.5">
                  <span className={`rounded-full px-2 py-0.5 text-xs ${t.revoked ? "bg-err/15 text-err" : "bg-ok/15 text-ok"}`}>
                    {t.revoked ? "已吊销" : "有效"}
                  </span>
                </td>
                <td className="px-4 py-2.5 text-right">
                  {!t.revoked && (
                    <button
                      onClick={() => confirm(`吊销令牌「${t.name}」？`) && revoke.mutate(t.id)}
                      className="rounded p-1.5 text-err hover:bg-err/10"
                    >
                      <Trash2 className="h-4 w-4" />
                    </button>
                  )}
                </td>
              </tr>
            ))}
            {tokens.length === 0 && (
              <tr>
                <td colSpan={5} className="px-4 py-8 text-center text-muted">
                  暂无令牌
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {/* 用户管理（admin） */}
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-lg font-semibold">用户管理</h2>
        <button
          onClick={() => setUserForm({ username: "", password: "", role: "user" })}
          className="flex items-center gap-2 rounded-lg bg-accent px-3 py-1.5 text-sm text-white hover:opacity-90"
        >
          <Plus className="h-4 w-4" />
          创建用户
        </button>
      </div>

      {createdUser && (
        <div className="mb-4 flex items-center gap-2 rounded-lg bg-ok/10 p-3 text-sm">
          <ShieldCheck className="h-4 w-4 shrink-0 text-ok" />
          <span className="text-xs">用户 Agent 密钥：</span>
          <span className="break-all font-mono text-xs">{createdUser}</span>
          <button onClick={() => setCreatedUser("")} className="ml-auto shrink-0 text-muted hover:text-fg">
            ✕
          </button>
        </div>
      )}

      {userForm && (
        <div className="mb-4 rounded-xl border border-border bg-panel p-4">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-4">
            <input
              placeholder="用户名"
              value={userForm.username}
              onChange={(e) => setUserForm({ ...userForm, username: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            />
            <input
              type="password"
              placeholder="密码（≥6位）"
              value={userForm.password}
              onChange={(e) => setUserForm({ ...userForm, password: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            />
            <select
              value={userForm.role}
              onChange={(e) => setUserForm({ ...userForm, role: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            >
              <option value="user">普通用户</option>
              <option value="admin">管理员</option>
            </select>
            <button
              onClick={() => createUser.mutate(userForm)}
              disabled={!userForm.username || userForm.password.length < 6}
              className="rounded-lg bg-accent px-4 py-1.5 text-sm text-white hover:opacity-90 disabled:opacity-40"
            >
              创建
            </button>
          </div>
        </div>
      )}

      <div className="overflow-hidden rounded-xl border border-border bg-panel">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted">
              <th className="px-4 py-2.5 font-normal">ID</th>
              <th className="px-4 py-2.5 font-normal">用户名</th>
              <th className="px-4 py-2.5 font-normal">角色</th>
              <th className="px-4 py-2.5 font-normal">创建时间</th>
              <th className="px-4 py-2.5 text-right font-normal">操作</th>
            </tr>
          </thead>
          <tbody>
            {users.map((u: User) => (
              <tr key={u.id} className="border-b border-border last:border-0">
                <td className="px-4 py-2.5 tabular text-muted">#{u.id}</td>
                <td className="px-4 py-2.5 font-medium">{u.username}</td>
                <td className="px-4 py-2.5">
                  <span className={`rounded-full px-2 py-0.5 text-xs ${u.role === "admin" ? "bg-accent/15 text-accent" : "bg-muted/20 text-muted"}`}>
                    {u.role === "admin" ? "管理员" : "普通用户"}
                  </span>
                </td>
                <td className="px-4 py-2.5 text-xs text-muted">{fmtDateTime(u.created_at)}</td>
                <td className="px-4 py-2.5 text-right">
                  <button
                    onClick={() => confirm(`删除用户「${u.username}」？其名下服务器将一并删除！`) && delUser.mutate(u.id)}
                    className="rounded p-1.5 text-err hover:bg-err/10"
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                </td>
              </tr>
            ))}
            {users.length === 0 && (
              <tr>
                <td colSpan={5} className="px-4 py-8 text-center text-muted">
                  暂无用户
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
