import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { Activity, Lock, User } from "lucide-react";
import { api, setToken } from "../lib/api";

export default function Login() {
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const navigate = useNavigate();

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading(true);
    setError("");
    try {
      const res = await api.login(username, password);
      setToken(res.token);
      navigate("/");
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
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
              placeholder="用户名"
              className="w-full rounded-lg border border-border bg-bg py-2.5 pl-9 pr-3 text-sm outline-none focus:border-accent"
            />
          </div>
          <div className="relative">
            <Lock className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted" />
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="密码"
              className="w-full rounded-lg border border-border bg-bg py-2.5 pl-9 pr-3 text-sm outline-none focus:border-accent"
            />
          </div>
          {error && <p className="text-sm text-err">{error}</p>}
          <button
            disabled={loading}
            className="w-full rounded-lg bg-accent py-2.5 text-sm font-medium text-white transition-opacity hover:opacity-90 disabled:opacity-50"
          >
            {loading ? "登录中…" : "登录"}
          </button>
        </form>
      </div>
    </div>
  );
}
