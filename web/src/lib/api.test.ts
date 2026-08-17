import { describe, expect, it } from "vitest";
import { DEFAULT_CAPABILITIES, notificationUpdatePayload, parseCommaList } from "./api";

describe("notificationUpdatePayload", () => {
  it("does not write redacted or blank sensitive values back", () => {
    expect(notificationUpdatePayload({ id: 1, name: "renamed", url: "https://example.com/***", headers: "", body: "" })).toEqual({ id: 1, name: "renamed" });
  });

  it("supports explicit replace and clear operations", () => {
    expect(notificationUpdatePayload({ id: 1, url: "https://new.example/hook", clear_headers: true, body: "new body" })).toEqual({
      id: 1,
      url: "https://new.example/hook",
      clear_headers: true,
      body: "new body",
    });
  });

  it("writes preset channel extra only when configured and supports clearing it", () => {
    expect(notificationUpdatePayload({ id: 1, extra: '{"access_token":"tok"}' })).toEqual({
      id: 1,
      extra: '{"access_token":"tok"}',
    });
    expect(notificationUpdatePayload({ id: 1, extra: "", clear_extra: true })).toEqual({ id: 1, clear_extra: true });
    // 空 extra（读取已脱敏）不提交，避免覆盖原值
    expect(notificationUpdatePayload({ id: 1, extra: "" })).toEqual({ id: 1 });
  });
});

describe("parseCommaList", () => {
  it("splits comma-separated globs, trims whitespace and drops empty entries", () => {
    expect(parseCommaList("eth0, eth1 ,, docker* ")).toEqual(["eth0", "eth1", "docker*"]);
    expect(parseCommaList("single")).toEqual(["single"]);
    expect(parseCommaList("")).toEqual([]);
    expect(parseCommaList("  , , ")).toEqual([]);
    expect(parseCommaList("/data/*,/logs")).toEqual(["/data/*", "/logs"]);
  });
});

describe("DEFAULT_CAPABILITIES", () => {
  it("enables all seven capabilities by default", () => {
    expect(Object.values(DEFAULT_CAPABILITIES)).toEqual([true, true, true, true, true, true, true]);
    expect(Object.keys(DEFAULT_CAPABILITIES).sort()).toEqual(["command", "files", "metrics", "nat", "probe", "terminal", "upgrade"]);
  });
});
