import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

// 未开启 vitest globals 时手动注册 RTL 清理，避免用例间 DOM 残留
afterEach(() => cleanup());
