import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach, vi } from "vitest";

// jsdom 未实现滚动 API；路由切换会调用它，测试中使用无副作用替身。
Object.defineProperty(window, "scrollTo", { value: vi.fn(), writable: true });

// Node 25's experimental --localstorage-file may expose a shell object without
// Storage methods; keep tests independent from that host-level flag.
if (typeof globalThis.localStorage?.getItem !== "function") {
  const values = new Map<string, string>();
  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: {
      getItem: (key: string) => values.get(key) ?? null,
      setItem: (key: string, value: string) => values.set(key, String(value)),
      removeItem: (key: string) => values.delete(key),
      clear: () => values.clear(),
    },
  });
}

// 未开启 vitest globals 时手动注册 RTL 清理，避免用例间 DOM 残留
afterEach(() => cleanup());
