// 服务器实时状态：WebSocket 每 2s 推送快照，广播给页面
import { createContext, useContext, useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { getToken, wsUrl, type Server } from "../lib/api";

type WSStatus = "connecting" | "connected" | "reconnecting";

interface ServersContextValue {
  servers: Server[];
  online: number;
  total: number;
  wsStatus: WSStatus;
}

const ServersContext = createContext<ServersContextValue>({ servers: [], online: 0, total: 0, wsStatus: "connecting" });

export function ServersProvider({ children }: { children: ReactNode }) {
  const [servers, setServers] = useState<Server[]>([]);
  const [wsStatus, setWsStatus] = useState<WSStatus>("connecting");
  const retries = useRef(0);

  useEffect(() => {
    let ws: WebSocket | null = null;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const connect = () => {
      setWsStatus(retries.current === 0 ? "connecting" : "reconnecting");
      // 游客也可连接（后端 WS 公开快照）；有 token 时带 token 看完整视图
      ws = new WebSocket(wsUrl("/api/v1/ws"));
      ws.onopen = () => {
        retries.current = 0;
        setWsStatus("connected");
      };
      ws.onmessage = (e) => {
        try {
          const msg = JSON.parse(e.data);
          if (msg.type === "snapshot") setServers(msg.servers);
        } catch {}
      };
      ws.onclose = () => {
        setWsStatus("reconnecting");
        // 指数退避重连，最多 30s
        const delay = Math.min(30000, 1000 * 2 ** Math.min(retries.current, 5));
        retries.current += 1;
        timer = setTimeout(connect, delay);
      };
      ws.onerror = () => {
        // onclose 会随后触发并进入重连
      };
    };
    connect();
    return () => {
      if (timer) clearTimeout(timer);
      ws?.close();
    };
  }, []);

  const online = servers.filter((s) => s.online).length;
  return (
    <ServersContext.Provider value={{ servers, online, total: servers.length, wsStatus }}>
      {children}
    </ServersContext.Provider>
  );
}

export function useServers() {
  return useContext(ServersContext);
}
