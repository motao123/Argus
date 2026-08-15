// 服务器实时状态：WebSocket 每 2s 推送快照，广播给页面
import { createContext, useContext, useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { getToken, wsUrl, type Server } from "../lib/api";

interface ServersContextValue {
  servers: Server[];
  online: number;
  total: number;
}

const ServersContext = createContext<ServersContextValue>({ servers: [], online: 0, total: 0 });

export function ServersProvider({ children }: { children: ReactNode }) {
  const [servers, setServers] = useState<Server[]>([]);
  const retries = useRef(0);

  useEffect(() => {
    let ws: WebSocket | null = null;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const connect = () => {
      if (!getToken()) return;
      ws = new WebSocket(wsUrl("/api/v1/ws"));
      ws.onmessage = (e) => {
        retries.current = 0;
        try {
          const msg = JSON.parse(e.data);
          if (msg.type === "snapshot") setServers(msg.servers);
        } catch {}
      };
      ws.onclose = () => {
        // 指数退避重连，最多 30s
        const delay = Math.min(30000, 1000 * 2 ** Math.min(retries.current, 5));
        retries.current += 1;
        timer = setTimeout(connect, delay);
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
    <ServersContext.Provider value={{ servers, online, total: servers.length }}>
      {children}
    </ServersContext.Provider>
  );
}

export function useServers() {
  return useContext(ServersContext);
}
