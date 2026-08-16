import { useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { Activity, Github, KeyRound, Lock, User } from "lucide-react";
import { api, setToken } from "../lib/api";
import { useI18n } from "../lib/i18n";

export default function Login() {
  const { t } = useI18n();
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const [twoFA, setTwoFA] = useState("");
  const [showTwoFA, setShowTwoFA] = useState(false);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [providers, setProviders] = useState<string[]>([]);
  const navigate = useNavigate();
  const [params] = useSearchParams();

  // OAuth 回调：消费一次性 code 换取会话（JWT 不进入 URL）
  useEffect(() => {
    const code = params.get("oauth_code");
    if (code) {
      api
        .consumeOAuthCode(code)
        .then((r) => {
          setToken(r.token);
          navigate("/admin/overview", { replace: true });
        })
        .catch((e) => setError(e.message));
    }
    api.oauthProviders().then((r) => setProviders(r.providers ?? [])).catch(() => {});
  }, [params, navigate]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading(true);
    setError("");
    try {
      const res = await api.login(username, password, showTwoFA ? twoFA : "");
      setToken(res.token);
      navigate(params.get("returnTo") || "/admin/overview", { replace: true });
    } catch (err) {
      const msg = (err as Error).message;
      setError(msg);
      // 启用 2FA 的账号缺码/错码时提示输入验证码
      if (msg.includes("2fa")) setShowTwoFA(true);
    } finally {
      setLoading(false);
    }
  };

  const oauthLogin = (name: string) => {
    window.location.href = `/api/v1/auth/oauth/${encodeURIComponent(name)}`;
  };

  return (
    <div className="flex min-h-screen items-center justify-center bg-bg">
      <div className="w-full max-w-sm rounded-2xl border border-border bg-panel p-8 shadow-xl">
        <div className="mb-6 flex items-center justify-center gap-2 text-2xl font-bold">
          <Activity className="h-7 w-7 text-accent" />
          Argus
        </div>
        <form onSubmit={submit} className="space-y-4">
          <div className="relative">
            <User className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted" />
            <input
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder={t("login.username")}
              autoComplete="username"
              className="w-full rounded-lg border border-border bg-bg py-2.5 pl-9 pr-3 text-sm outline-none focus:border-accent"
            />
          </div>
          <div className="relative">
            <Lock className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted" />
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder={t("login.password")}
              autoComplete="current-password"
              className="w-full rounded-lg border border-border bg-bg py-2.5 pl-9 pr-3 text-sm outline-none focus:border-accent"
            />
          </div>
          {showTwoFA && (
            <div className="relative">
              <KeyRound className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted" />
              <input
                value={twoFA}
                onChange={(e) => setTwoFA(e.target.value)}
                placeholder={t("login.twoFA")}
                inputMode="numeric"
                autoComplete="one-time-code"
                className="w-full rounded-lg border border-border bg-bg py-2.5 pl-9 pr-3 text-sm outline-none focus:border-accent"
              />
            </div>
          )}
          {error && <p className="text-sm text-err">{error}</p>}
          <button
            disabled={loading}
            className="w-full rounded-lg bg-accent py-2.5 text-sm font-medium text-white transition-opacity hover:opacity-90 disabled:opacity-50"
          >
            {loading ? t("login.loggingIn") : t("common.login")}
          </button>
        </form>
        {providers.length > 0 && (
          <div className="mt-4 space-y-2 border-t border-border pt-4">
            <p className="text-center text-xs text-muted">{t("login.orThirdParty")}</p>
            {providers.map((name) => (
              <button
                key={name}
                onClick={() => oauthLogin(name)}
                className="flex w-full items-center justify-center gap-2 rounded-lg border border-border py-2 text-sm hover:bg-black/5 dark:hover:bg-white/5"
              >
                <Github className="h-4 w-4" />
                {name}
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
