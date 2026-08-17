import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, KeyRound, Plus, ShieldCheck, Trash2 } from "lucide-react";
import { api, type ApiToken, type User } from "../lib/api";
import { useI18n, type TKey } from "../lib/i18n";
const allScopes: { key: string; label: TKey }[] = [
  { key: "argus:server:read", label: "access.scopeServerRead" },
  { key: "argus:server:write", label: "access.scopeServerWrite" },
  { key: "argus:server:delete", label: "access.scopeServerDelete" },
  { key: "argus:server:exec", label: "access.scopeServerExec" },
  { key: "argus:service:read", label: "access.scopeServiceRead" },
  { key: "argus:service:write", label: "access.scopeServiceWrite" },
  { key: "argus:alert:read", label: "access.scopeAlertRead" },
  { key: "argus:alert:write", label: "access.scopeAlertWrite" },
  { key: "argus:cron:read", label: "access.scopeCronRead" },
  { key: "argus:cron:write", label: "access.scopeCronWrite" },
  { key: "argus:notification:read", label: "access.scopeNotifRead" },
  { key: "argus:notification:write", label: "access.scopeNotifWrite" },
];

const roleBadgeClass: Record<string, string> = {
  admin: "bg-accent/15 text-accent",
  readonly: "bg-warn/15 text-warn",
  user: "bg-muted/20 text-muted",
};

export default function Access() {
  const { t, fmtDateTime } = useI18n();
  const qc = useQueryClient();
  const { data: tokenData } = useQuery({ queryKey: ["tokens"], queryFn: api.tokens });
  const { data: userData } = useQuery({ queryKey: ["users"], queryFn: api.users });
  const tokens = tokenData?.tokens ?? [];
  const users = userData?.users ?? [];

  const [tkForm, setTkForm] = useState<{ name: string; scopes: string[]; expires_in: number } | null>(null);
  const [created, setCreated] = useState<string>("");
  const [userForm, setUserForm] = useState<{ username: string; password: string; role: string } | null>(null);
  const [createdUser, setCreatedUser] = useState<string>("");

  const roleLabel = (role: string) =>
    role === "admin" ? t("access.roleAdmin") : role === "readonly" ? t("access.roleReadonly") : t("access.roleUser");

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
  const [viewedSecret, setViewedSecret] = useState<{ username: string; secret: string } | null>(null);
  const viewSecret = useMutation({
    mutationFn: (u: User) => api.userSecret(u.id).then((r) => ({ username: u.username, secret: r.agent_secret })),
    onSuccess: setViewedSecret,
  });

  return (
    <div>
      <h1 className="mb-1 text-xl font-semibold">{t("access.title")}</h1>
      <p className="mb-5 text-sm text-muted">{t("access.subtitle")}</p>

      {/* PAT */}
      <div className="mb-3 flex items-center justify-between">
        <h2 className="flex items-center gap-2 text-lg font-semibold">
          <KeyRound className="h-4 w-4 text-accent" /> {t("access.tokens")}
        </h2>
        <button
          onClick={() => setTkForm({ name: "", scopes: ["argus:server:read"], expires_in: 30 })}
          className="flex items-center gap-2 rounded-lg bg-accent px-3 py-1.5 text-sm text-white hover:opacity-90"
        >
          <Plus className="h-4 w-4" />
          {t("access.createToken")}
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
              placeholder={t("access.tokenName")}
              value={tkForm.name}
              onChange={(e) => setTkForm({ ...tkForm, name: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            />
            <input
              type="number"
              placeholder={t("access.expiresIn")}
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
                {t(sc.label)}
              </label>
            ))}
          </div>
          <div className="mt-3 flex gap-2">
            <button
              onClick={() => createToken.mutate(tkForm)}
              disabled={!tkForm.name || tkForm.scopes.length === 0}
              className="rounded-lg bg-accent px-4 py-1.5 text-sm text-white hover:opacity-90 disabled:opacity-40"
            >
              {t("access.createOnce")}
            </button>
            <button onClick={() => setTkForm(null)} className="rounded-lg border border-border px-4 py-1.5 text-sm text-muted">
              {t("common.cancel")}
            </button>
          </div>
        </div>
      )}

      <div className="mb-8 overflow-hidden rounded-xl border border-border bg-panel">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted">
              <th className="px-4 py-2.5 font-normal">{t("common.name")}</th>
              <th className="px-4 py-2.5 font-normal">{t("access.scopes")}</th>
              <th className="px-4 py-2.5 font-normal">{t("access.expires")}</th>
              <th className="px-4 py-2.5 font-normal">{t("common.status")}</th>
              <th className="px-4 py-2.5 text-right font-normal">{t("common.actions")}</th>
            </tr>
          </thead>
          <tbody>
            {tokens.map((tk: ApiToken) => (
              <tr key={tk.id} className="border-b border-border last:border-0">
                <td className="px-4 py-2.5 font-medium">{tk.name}</td>
                <td className="max-w-xs truncate px-4 py-2.5 text-xs text-muted">{tk.scopes}</td>
                <td className="px-4 py-2.5 tabular text-xs text-muted">{tk.expires_at ? fmtDateTime(tk.expires_at) : t("access.forever")}</td>
                <td className="px-4 py-2.5">
                  <span className={`rounded-full px-2 py-0.5 text-xs ${tk.revoked ? "bg-err/15 text-err" : "bg-ok/15 text-ok"}`}>
                    {tk.revoked ? t("access.revoked") : t("access.active")}
                  </span>
                </td>
                <td className="px-4 py-2.5 text-right">
                  {!tk.revoked && (
                    <button
                      onClick={() => confirm(t("access.confirmRevoke", { name: tk.name })) && revoke.mutate(tk.id)}
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
                  {t("access.noTokens")}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {/* 用户管理（admin） */}
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-lg font-semibold">{t("access.users")}</h2>
        <button
          onClick={() => setUserForm({ username: "", password: "", role: "user" })}
          className="flex items-center gap-2 rounded-lg bg-accent px-3 py-1.5 text-sm text-white hover:opacity-90"
        >
          <Plus className="h-4 w-4" />
          {t("access.createUser")}
        </button>
      </div>

      {createdUser && (
        <div className="mb-4 flex items-center gap-2 rounded-lg bg-ok/10 p-3 text-sm">
          <ShieldCheck className="h-4 w-4 shrink-0 text-ok" />
          <span className="text-xs">{t("access.userSecret")}</span>
          <span className="break-all font-mono text-xs">{createdUser}</span>
          <button onClick={() => setCreatedUser("")} className="ml-auto shrink-0 text-muted hover:text-fg">
            ✕
          </button>
        </div>
      )}

      {viewedSecret && (
        <div className="mb-4 flex items-center gap-2 rounded-lg bg-ok/10 p-3 text-sm">
          <ShieldCheck className="h-4 w-4 shrink-0 text-ok" />
          <span className="text-xs">{t("access.secretOf", { name: viewedSecret.username })}</span>
          <span className="break-all font-mono text-xs">{viewedSecret.secret}</span>
          <button onClick={() => navigator.clipboard?.writeText(viewedSecret.secret)} className="ml-auto shrink-0 text-muted hover:text-fg">
            <Copy className="h-4 w-4" />
          </button>
          <button onClick={() => setViewedSecret(null)} className="shrink-0 text-muted hover:text-fg">
            ✕
          </button>
        </div>
      )}

      {userForm && (
        <div className="mb-4 rounded-xl border border-border bg-panel p-4">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-4">
            <input
              placeholder={t("common.username")}
              value={userForm.username}
              onChange={(e) => setUserForm({ ...userForm, username: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            />
            <input
              type="password"
              placeholder={t("access.passwordMin")}
              value={userForm.password}
              onChange={(e) => setUserForm({ ...userForm, password: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            />
            <select
              value={userForm.role}
              onChange={(e) => setUserForm({ ...userForm, role: e.target.value })}
              className="rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            >
              <option value="user">{t("access.roleUser")}</option>
              <option value="readonly">{t("access.roleReadonly")}</option>
              <option value="admin">{t("access.roleAdmin")}</option>
            </select>
            <button
              onClick={() => createUser.mutate(userForm)}
              disabled={!userForm.username || userForm.password.length < 6}
              className="rounded-lg bg-accent px-4 py-1.5 text-sm text-white hover:opacity-90 disabled:opacity-40"
            >
              {t("common.create")}
            </button>
          </div>
          <p className="mt-2 text-xs text-muted">{t("access.roleReadonlyHelp")}</p>
        </div>
      )}

      <div className="overflow-hidden rounded-xl border border-border bg-panel">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted">
              <th className="px-4 py-2.5 font-normal">{t("common.id")}</th>
              <th className="px-4 py-2.5 font-normal">{t("common.username")}</th>
              <th className="px-4 py-2.5 font-normal">{t("access.role")}</th>
              <th className="px-4 py-2.5 font-normal">{t("access.createdAt")}</th>
              <th className="px-4 py-2.5 text-right font-normal">{t("common.actions")}</th>
            </tr>
          </thead>
          <tbody>
            {users.map((u: User) => (
              <tr key={u.id} className="border-b border-border last:border-0">
                <td className="px-4 py-2.5 tabular text-muted">#{u.id}</td>
                <td className="px-4 py-2.5 font-medium">{u.username}</td>
                <td className="px-4 py-2.5">
                  <span className={`rounded-full px-2 py-0.5 text-xs ${roleBadgeClass[u.role] ?? "bg-muted/20 text-muted"}`}>
                    {roleLabel(u.role)}
                  </span>
                </td>
                <td className="px-4 py-2.5 text-xs text-muted">{fmtDateTime(u.created_at)}</td>
                <td className="px-4 py-2.5 text-right">
                  <button
                    onClick={() => viewSecret.mutate(u)}
                    title={t("access.viewSecret")}
                    className="mr-1 rounded p-1.5 text-muted hover:bg-accent/10 hover:text-accent"
                  >
                    <KeyRound className="h-4 w-4" />
                  </button>
                  <button
                    onClick={() => confirm(t("access.confirmDeleteUser", { name: u.username })) && delUser.mutate(u.id)}
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
                  {t("access.noUsers")}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
