// dev-mock：同时启动 mock API 与 vite dev，任一退出则整体退出
import { spawn } from "node:child_process";

const isWindows = process.platform === "win32";
const pnpm = isWindows ? "pnpm.cmd" : "pnpm";

const children = [
  spawn(process.execPath, ["scripts/mock-server.mjs"], { stdio: "inherit", env: { ...process.env, MOCK_ARGUS_PORT: process.env.MOCK_ARGUS_PORT || "8008" } }),
  spawn(pnpm, ["dev"], { stdio: "inherit", env: { ...process.env, ARGUS_API: process.env.ARGUS_API || "http://127.0.0.1:8008" } }),
];

let shuttingDown = false;
function shutdown(code = 0) {
  if (shuttingDown) return;
  shuttingDown = true;
  for (const c of children) if (!c.killed) c.kill("SIGTERM");
  process.exit(code);
}

for (const c of children) {
  c.on("exit", (code, signal) => {
    if (shuttingDown) return;
    if (code === 0 || signal === "SIGTERM") return;
    shutdown(code ?? 1);
  });
}
process.on("SIGINT", () => shutdown(0));
process.on("SIGTERM", () => shutdown(0));
