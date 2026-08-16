import { useEffect, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { KeyRound, Plus, ShieldCheck, Trash2 } from "lucide-react";
import { api, type OAuthConfig } from "../lib/api";

export default function Security() {
  const qc = useQueryClient();
  const { data: me } = useQuery({ queryKey: ["me"], queryFn: api.me });
  const { data: oauthData } = useQuery({ queryKey: ["oauth-configs"], queryFn: api.oauthConfigs });
  const providers = oauthData?.providers ?? [];

  const [secret, setSecret] = useState("");
  const [qrUrl, setQrUrl] = useState("");
  const [setupCode, setSetupCode] = useState("");
  const [disableCode, setDisableCode] = useState("");
  const [msg, setMsg] = useState("");
  const [oauthForm, setOAuthForm] = useState<(Partial<OAuthConfig> & { name: string; client_secret?: string }) | null>(null);

  const twoFAEnabled = !!me?.two_fa_enabled;

  const startSetup = useMutation({
    mutationFn: async () => {
      const s = await api.twoFASetup();
      setSecret(s.secret);
      const blob = await api.twoFAQRCode();
      setQrUrl(URL.createObjectURL(blob));
      return s;
    },
    onError: (e) => setMsg((e as Error).message),
  });
  const enable = useMutation({
    mutationFn: (code: string) => api.twoFAEnable(code),
    onSuccess: () => {
      setMsg("2FA 已启用");
      setQrUrl("");
      setSecret("");
      setSetupCode("");
      qc.invalidateQueries({ queryKey: ["me"] });
    },
    onError: (e) => setMsg((e as Error).message),
  });
  const disable = useMutation({
    mutationFn: (code: string) => api.twoFADisable(code),
    onSuccess: () => {
      setMsg("2FA 已关闭");
      setDisableCode("");
      qc.invalidateQueries({ queryKey: ["me"] });
    },
    onError: (e) => setMsg((e as Error).message),
  });
  const saveProvider = useMutation({
    mutationFn: (cfg: Partial<OAuthConfig> & { name: string; client_secret?: string }) => api.saveOAuthConfig(cfg),
    onSuccess: () => {
      setOAuthForm(null);
      qc.invalidateQueries({ queryKey: ["oauth-configs"] });
    },
    onError: (e) => setMsg((e as Error).message),
  });
  const delProvider = useMutation({
    mutationFn: api.deleteOAuthConfig,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["oauth-configs"] }),
  });

  return (
    <div className="space-y-8">
      <section>
        <h1 className="mb-1 flex items-center gap-2 text-xl font-semibold">
          <ShieldCheck className="h-5 w-5 text-accent" /> 账户安全
        </h1>
        <p className="mb-4 text-sm text-muted">两步验证（TOTP）与 OAuth 登录配置</p>
        {msg && <p className="mb-3 text-sm text-ok">{msg}</p>}

        <div className="max-w-md rounded-xl border border-border bg-panel p-5">
          <div className="mb-3 flex items-center justify-between">
            <span className="text-sm font-medium">两步验证（TOTP）</span>
            <span className={`rounded-full px-2 py-0.5 text-xs ${twoFAEnabled ? "bg-ok/15 text-ok" : "bg-muted/20 text-muted"}`}>
              {twoFAEnabled ? "已启用" : "未启用"}
            </span>
          </div>
          {!twoFAEnabled && !secret && (
            <button
              onClick={() => startSetup.mutate()}
              className="rounded-lg bg-accent px-4 py-2 text-sm text-white hover:opacity-90"
            >
              开启 2FA
            </button>
          )}
          {secret && (
            <div className="space-y-3">
              {qrUrl && <img src={qrUrl} alt="TOTP 二维码" className="h-40 w-40 rounded-lg border border-border" />}
              <div className="break-all rounded-lg bg-bg p-2 font-mono text-xs">{secret}</div>
              <p className="text-xs text-muted">使用 Authenticator 扫码或手动输入密钥，然后输入 6 位验证码启用</p>
              <div className="flex gap-2">
                <input
                  value={setupCode}
                  onChange={(e) => setSetupCode(e.target.value)}
                  placeholder="6 位验证码"
                  inputMode="numeric"
                  className="w-40 rounded-lg border border-border bg-bg px-3 py-2 text-sm"
                />
                <button
                  onClick={() => setupCode && enable.mutate(setupCode)}
                  className="rounded-lg bg-accent px-4 py-2 text-sm text-white disabled:opacity-40"
                >
                  启用
                </button>
              </div>
            </div>
          )}
          {twoFAEnabled && (
            <div className="flex gap-2">
              <input
                value={disableCode}
                onChange={(e) => setDisableCode(e.target.value)}
                placeholder="输入当前验证码以关闭"
                inputMode="numeric"
                className="w-52 rounded-lg border border-border bg-bg px-3 py-2 text-sm"
              />
              <button
                onClick={() => disableCode && disable.mutate(disableCode)}
                className="rounded-lg border border-err/40 px-4 py-2 text-sm text-err hover:bg-err/10"
              >
                关闭 2FA
              </button>
            </div>
          )}
        </div>
      </section>

      <section>
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-lg font-semibold">OAuth 登录（管理员）</h2>
          <button
            onClick={() => setOAuthForm({ name: "", client_id: "", client_secret: "", auth_url: "", token_url: "", user_info_url: "", username_field: "login", admin_logins: "", enabled: true })}
            className="flex items-center gap-2 rounded-lg bg-accent px-3 py-1.5 text-sm text-white"
          >
            <Plus className="h-4 w-4" /> 新增 Provider
          </button>
        </div>

        {oauthForm && (
          <div className="mb-4 grid grid-cols-1 gap-3 rounded-xl border border-border bg-panel p-4 md:grid-cols-2">
            <input aria-label="名称" placeholder="名称（github/gitee/custom）" value={oauthForm.name ?? ""} onChange={(e) => setOAuthForm({ ...oauthForm, name: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input aria-label="Client ID" placeholder="Client ID" value={oauthForm.client_id ?? ""} onChange={(e) => setOAuthForm({ ...oauthForm, client_id: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input aria-label="Client Secret" type="password" placeholder="Client Secret（留空保留原值）" value={oauthForm.client_secret ?? ""} onChange={(e) => setOAuthForm({ ...oauthForm, client_secret: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input aria-label="Auth URL" placeholder="授权端点 URL" value={oauthForm.auth_url ?? ""} onChange={(e) => setOAuthForm({ ...oauthForm, auth_url: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input aria-label="Token URL" placeholder="Token 端点 URL" value={oauthForm.token_url ?? ""} onChange={(e) => setOAuthForm({ ...oauthForm, token_url: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input aria-label="UserInfo URL" placeholder="用户信息端点 URL" value={oauthForm.user_info_url ?? ""} onChange={(e) => setOAuthForm({ ...oauthForm, user_info_url: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input aria-label="用户名字段" placeholder="用户名字段（默认 login）" value={oauthForm.username_field ?? "login"} onChange={(e) => setOAuthForm({ ...oauthForm, username_field: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <input aria-label="管理员登录名" placeholder="管理员登录名（逗号分隔）" value={oauthForm.admin_logins ?? ""} onChange={(e) => setOAuthForm({ ...oauthForm, admin_logins: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={!!oauthForm.enabled} onChange={(e) => setOAuthForm({ ...oauthForm, enabled: e.target.checked })} />
              启用
            </label>
            <div className="flex gap-2">
              <button disabled={!oauthForm.name || !oauthForm.client_id || !oauthForm.auth_url} onClick={() => saveProvider.mutate(oauthForm)} className="rounded-lg bg-accent px-4 py-2 text-sm text-white disabled:opacity-40">保存</button>
              <button onClick={() => setOAuthForm(null)} className="rounded-lg border border-border px-4 py-2 text-sm">取消</button>
            </div>
          </div>
        )}

        <div className="overflow-hidden rounded-xl border border-border bg-panel">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-muted">
                <th className="px-4 py-2.5">名称</th><th className="px-4 py-2.5">状态</th><th className="px-4 py-2.5 text-right">操作</th>
              </tr>
            </thead>
            <tbody>
              {providers.map((p: OAuthConfig) => (
                <tr key={p.id} className="border-b border-border last:border-0">
                  <td className="px-4 py-2.5 font-medium">{p.name}</td>
                  <td className="px-4 py-2.5"><span className={p.enabled ? "text-ok" : "text-muted"}>{p.enabled ? "启用" : "停用"}</span></td>
                  <td className="px-4 py-2.5 text-right">
                    <button
                      title="编辑"
                      onClick={() => setOAuthForm({ id: p.id, name: p.name, client_id: p.client_id, auth_url: p.auth_url, token_url: p.token_url, user_info_url: p.user_info_url, username_field: p.username_field, admin_logins: p.admin_logins, enabled: p.enabled })}
                      className="mr-1 rounded p-1.5 text-muted hover:bg-accent/10"
                    >
                      <KeyRound className="h-4 w-4" />
                    </button>
                    <button title="删除" onClick={() => confirm(`删除 OAuth provider「${p.name}」？`) && delProvider.mutate(p.name)} className="rounded p-1.5 text-err hover:bg-err/10">
                      <Trash2 className="h-4 w-4" />
                    </button>
                  </td>
                </tr>
              ))}
              {providers.length === 0 && <tr><td colSpan={3} className="px-4 py-8 text-center text-muted">暂无 OAuth provider 配置</td></tr>}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}
