import { useEffect, useRef, useState } from "react";
import { useParams, Link } from "react-router-dom";
import { ArrowLeft } from "lucide-react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { getToken, wsUrl } from "../lib/api";
import { useI18n } from "../lib/i18n";

export default function TerminalPage() {
  const { t } = useI18n();
  const { id } = useParams();
  const ref = useRef<HTMLDivElement>(null);
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState("");
  const [compatMode, setCompatMode] = useState(false);
  // 敏感操作 2FA：已启用 2FA 的用户需要先输入 TOTP 码再连接
  const [needs2FA, setNeeds2FA] = useState(false);
  const [twofa, setTwofa] = useState("");
  const [started, setStarted] = useState(false);

  useEffect(() => {
    // 探测当前用户是否启用 2FA（决定是否显示验证码输入）
    fetch("/api/v1/auth/me", { headers: { Authorization: `Bearer ${getToken()}` } })
      .then((r) => r.json())
      .then((d) => {
        if (d.data?.two_fa_enabled) setNeeds2FA(true);
      })
      .catch(() => {});
  }, []);

  useEffect(() => {
    if (!started || !ref.current || !getToken()) return;

    let fontSize = 13;
    let termTheme = "dark";
    fetch("/api/v1/public/term-settings")
      .then((r) => r.json())
      .then((d) => {
        if (d.data?.font_size) fontSize = Number(d.data.font_size) || 13;
        if (d.data?.theme) termTheme = d.data.theme;
        term.options.fontSize = fontSize;
        if (termTheme === "light") {
          term.options.theme = { background: "#ffffff", foreground: "#111827" };
        }
      })
      .catch(() => {});

    const term = new Terminal({
      cursorBlink: true,
      fontSize,
      fontFamily: 'Menlo, Consolas, "Courier New", monospace',
      theme: {
        background: termTheme === "light" ? "#ffffff" : "#0d1117",
        foreground: termTheme === "light" ? "#111827" : "#e6edf3",
      },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(ref.current);
    fit.fit();

    const sep = id?.includes("?") ? "&" : "?";
    const ws = new WebSocket(wsUrl(`/api/v1/terminal/${id}${sep}twofa=${encodeURIComponent(twofa)}`));
    ws.binaryType = "arraybuffer";

    ws.onopen = () => {
      setConnected(true);
      setCompatMode(false);
      term.write(`\x1b[32m${t("terminal.banner")}\x1b[0m\r\n`);
      sendResize();
    };
    ws.onmessage = (e) => {
      const data = typeof e.data === "string" ? e.data : new Uint8Array(e.data);
      term.write(data as Uint8Array);
    };
    ws.onclose = () => {
      setConnected(false);
      term.write(`\r\n\x1b[31m${t("terminal.closed")}\x1b[0m\r\n`);
    };
    ws.onerror = () => setError(t("terminal.failed"));

    const onData = (data: string) => {
      if (ws.readyState === WebSocket.OPEN) ws.send(data);
    };
    const sendResize = () => {
      fit.fit();
      if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }));
    };
    const onResize = () => sendResize();
    term.onData(onData);
    term.onResize(onResize);
    const ro = new ResizeObserver(onResize);
    ro.observe(ref.current);

    return () => {
      ro.disconnect();
      ws.close();
      term.dispose();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, t, started, twofa]);

  const connect = () => setStarted(true);

  return (
    <div className="flex h-[calc(100vh-3rem)] flex-col">
      <div className="mb-3 flex items-center gap-3">
        <Link to={`/server/${id}`} className="rounded-lg p-2 hover:bg-black/5 dark:hover:bg-white/5">
          <ArrowLeft className="h-4 w-4" />
        </Link>
        <h1 className="text-lg font-semibold">{t("terminal.title", { id: id ?? "" })}</h1>
        <span
          className={`rounded-full px-2 py-0.5 text-xs ${
            connected ? "bg-ok/15 text-ok" : error ? "bg-err/15 text-err" : "bg-muted/20 text-muted"
          }`}
        >
          {error ? t("terminal.failed") : compatMode ? t("terminal.compat") : connected ? t("terminal.connected") : t("terminal.connecting")}
        </span>
      </div>
      {needs2FA && !started ? (
        <div className="flex flex-1 items-center justify-center">
          <div className="w-full max-w-sm rounded-xl border border-border bg-panel p-5">
            <p className="mb-3 text-sm font-medium">{t("terminal.twoFARequired")}</p>
            <input
              value={twofa}
              onChange={(e) => setTwofa(e.target.value)}
              placeholder={t("terminal.twoFACodePlaceholder")}
              className="mb-3 w-full rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none"
            />
            <button
              onClick={connect}
              disabled={!twofa}
              className="w-full rounded-lg bg-accent px-4 py-2 text-sm text-white hover:opacity-90 disabled:opacity-40"
            >
              {t("terminal.connect")}
            </button>
          </div>
        </div>
      ) : (
        <div ref={ref} className="min-h-0 flex-1 overflow-hidden rounded-xl border border-border" />
      )}
    </div>
  );
}
