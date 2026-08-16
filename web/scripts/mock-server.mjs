// Argus Mock Server：无后端演示环境（借鉴 nezha-dash-v2 的 mock 设计）。
// 用法: pnpm mock:server  （配合 pnpm dev 使用，或 pnpm dev:mock 一键启动）
import http from "node:http";
import { WebSocketServer } from "ws";

const port = Number(process.env.MOCK_ARGUS_PORT || 8008);
const count = Number(process.env.MOCK_ARGUS_COUNT || 12);
const intervalMs = Number(process.env.MOCK_ARGUS_INTERVAL_MS || 2000);

const platforms = ["Ubuntu 24.04", "Debian 12", "CentOS 9", "AlmaLinux 9", "Arch Linux", "openSUSE", "FreeBSD 14"];
const groups = ["核心", "边缘", "数据库", "香港", "美西", "欧洲"];
const countries = ["CN", "US", "DE", "JP", "SG", "GB", "FR", "HK", "TW"];

function rand(min, max) {
  return Math.random() * (max - min) + min;
}

function sendJson(res, status, body) {
  res.writeHead(status, { "Content-Type": "application/json; charset=utf-8" });
  res.end(JSON.stringify(body));
}

function sendData(res, data) {
  sendJson(res, 200, { success: true, data });
}

const servers = Array.from({ length: count }, (_, i) => {
  const online = Math.random() > 0.12;
  return {
    id: i + 1,
    name: `node-${String(i + 1).padStart(2, "0")}-${platforms[i % platforms.length].split(" ")[0].toLowerCase()}`,
    group: groups[i % groups.length],
    note: i % 4 === 0 ? "示例服务器" : "",
    host: {
      hostname: `node-${i + 1}`,
      platform: platforms[i % platforms.length],
      platform_version: "x86_64",
      cpu_model: "Intel Xeon Platinum 8462Y+",
      cpu_cores: 4 + (i % 8) * 2,
      agent_version: "0.1.0",
      ip: `203.0.113.${(i % 254) + 1}`,
      country_code: countries[i % countries.length],
    },
    cpu: online ? rand(1, 70) : 0,
    mem_used: online ? rand(2, 28) * 1024 ** 3 : 0,
    mem_total: 32 * 1024 ** 3,
    disk_used: online ? rand(20, 300) * 1024 ** 3 : 0,
    disk_total: 512 * 1024 ** 3,
    net_in_speed: online ? rand(10, 800) * 1024 : 0,
    net_out_speed: online ? rand(10, 400) * 1024 : 0,
    load1: online ? rand(0.1, 4) : 0,
    uptime: online ? Math.floor(rand(3600, 86400 * 200)) : 0,
    online,
    last_seen: new Date().toISOString(),
  };
});

const server = http.createServer((req, res) => {
  const requestUrl = new URL(req.url || "/", `http://${req.headers.host || "localhost"}`);
  const path = requestUrl.pathname;

  // 服务器公开视图与真实后端一致，匿名请求也可以访问。
  if (req.method === "GET" && path === "/api/v1/servers") {
    sendData(res, { servers });
    return;
  }
  if (req.method === "GET" && path === "/api/v1/alerts") {
    sendData(res, { alerts: [] });
    return;
  }
  if (req.method === "GET" && path === "/api/v1/notifications") {
    sendData(res, { notifications: [] });
    return;
  }
  if (req.method === "GET" && path === "/api/v1/crons") {
    sendData(res, { crons: [] });
    return;
  }

  const metricsMatch = path.match(/^\/api\/v1\/servers\/(\d+)\/metrics$/);
  if (req.method === "GET" && metricsMatch) {
    const requestedPeriod = requestUrl.searchParams.get("period");
    const periodName = requestedPeriod === "24h" || requestedPeriod === "7d" ? requestedPeriod : "1h";
    const period = periodName === "24h" ? 24 * 3600 : periodName === "7d" ? 7 * 86400 : 3600;
    const step = periodName === "1h" ? 60 : periodName === "24h" ? 300 : 3600;
    const now = Math.floor(Date.now() / 1000);
    const points = [];
    for (let ts = now - period; ts <= now; ts += step) {
      points.push({
        ts,
        cpu: rand(2, 70),
        net_in: rand(1, 400) * 1024,
        net_out: rand(1, 200) * 1024,
        load1: rand(0.1, 3),
        mem_used: rand(2, 26) * 1024 ** 3,
        mem_total: 32 * 1024 ** 3,
        disk_used: rand(20, 300) * 1024 ** 3,
        disk_total: 512 * 1024 ** 3,
      });
    }
    sendData(res, { period: periodName, points });
    return;
  }

  sendJson(res, 404, { success: false, data: null, error: "not found" });
});

// WebSocket 实时推送保持原始消息协议，不套 HTTP 响应壳。
const wss = new WebSocketServer({ server });
wss.on("connection", (ws) => {
  const timer = setInterval(() => {
    for (const s of servers) {
      if (s.online) {
        s.cpu = rand(1, 70);
        s.net_in_speed = rand(10, 800) * 1024;
        s.net_out_speed = rand(10, 400) * 1024;
        s.load1 = rand(0.1, 4);
      }
    }
    if (ws.readyState === ws.OPEN) {
      ws.send(JSON.stringify({ type: "snapshot", servers }));
    }
  }, intervalMs);
  ws.on("close", () => clearInterval(timer));
});

server.listen(port, () => {
  console.log(`Argus Mock API listening on http://localhost:${port} with ${count} servers`);
  console.log(`REST: /api/v1/*   WS: /api/v1/ws`);
});
