import { useEffect, useRef, useState } from "react";
import { useParams, Link } from "react-router-dom";
import { ArrowLeft } from "lucide-react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { getToken, wsUrl } from "../lib/api";

export default function TerminalPage() {
  const { id } = useParams();
  const ref = useRef<HTMLDivElement>(null);
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!ref.current || !getToken()) return;

    const term = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: 'Menlo, Consolas, "Courier New", monospace',
      theme: {
        background: "#0d1117",
        foreground: "#e6edf3",
      },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(ref.current);
    fit.fit();

    const ws = new WebSocket(wsUrl(`/api/v1/terminal/${id}`));
    ws.binaryType = "arraybuffer";

    ws.onopen = () => {
      setConnected(true);
      term.write("\x1b[32m已连接服务器终端...\x1b[0m\r\n");
    };
    ws.onmessage = (e) => {
      const data = typeof e.data === "string" ? e.data : new Uint8Array(e.data);
      term.write(data as Uint8Array);
    };
    ws.onclose = () => {
      setConnected(false);
      term.write("\r\n\x1b[31m连接已断开\x1b[0m\r\n");
    };
    ws.onerror = () => setError("终端连接失败");

    const onData = (data: string) => {
      if (ws.readyState === WebSocket.OPEN) ws.send(data);
    };
    // 注意：不要把 resize 信息作为终端输入发送 —— Agent 侧没有窗口尺寸协议，
    // JSON 会被 shell 当成命令吞掉。尺寸变化只做本地 fit。
    const onResize = () => {
      fit.fit();
    };
    term.onData(onData);
    term.onResize(onResize);
    const ro = new ResizeObserver(onResize);
    ro.observe(ref.current);

    return () => {
      ro.disconnect();
      ws.close();
      term.dispose();
    };
  }, [id]);

  return (
    <div className="flex h-[calc(100vh-3rem)] flex-col">
      <div className="mb-3 flex items-center gap-3">
        <Link to={`/server/${id}`} className="rounded-lg p-2 hover:bg-black/5 dark:hover:bg-white/5">
          <ArrowLeft className="h-4 w-4" />
        </Link>
        <h1 className="text-lg font-semibold">服务器 #{id} 终端</h1>
        <span
          className={`rounded-full px-2 py-0.5 text-xs ${
            connected ? "bg-ok/15 text-ok" : error ? "bg-err/15 text-err" : "bg-muted/20 text-muted"
          }`}
        >
          {error ? "连接失败" : connected ? "已连接" : "连接中…"}
        </span>
      </div>
      <div ref={ref} className="min-h-0 flex-1 overflow-hidden rounded-xl border border-border" />
    </div>
  );
}
