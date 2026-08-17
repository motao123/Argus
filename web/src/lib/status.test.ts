import { describe, expect, it } from "vitest";
import { fmtAvailability, fmtMinutes, isWindowActive, monthLabel, severityRank, severityTone, windowState } from "./status";

describe("severityTone", () => {
  it("maps severity to tone", () => {
    expect(severityTone("critical")).toBe("err");
    expect(severityTone("major")).toBe("warn");
    expect(severityTone("minor")).toBe("ok");
  });
});

describe("severityRank", () => {
  it("orders critical < major < minor", () => {
    expect(severityRank("critical")).toBeLessThan(severityRank("major"));
    expect(severityRank("major")).toBeLessThan(severityRank("minor"));
  });
});

describe("windowState", () => {
  const now = Date.parse("2026-08-17T12:00:00Z");
  const oneOff = { start_at: "2026-08-17T10:00:00Z", end_at: "2026-08-17T14:00:00Z", recurring: false };

  it("classifies one-off windows", () => {
    expect(windowState(oneOff, Date.parse("2026-08-17T09:59:59Z"))).toBe("upcoming");
    expect(windowState(oneOff, Date.parse("2026-08-17T10:00:00Z"))).toBe("active");
    expect(windowState(oneOff, Date.parse("2026-08-17T13:59:59Z"))).toBe("active");
    expect(windowState(oneOff, Date.parse("2026-08-17T14:00:00Z"))).toBe("ended");
  });

  it("classifies recurring windows weekly (never 'ended')", () => {
    // 每周六 22:00 → 周日 02:00；2026-08-15 是周六
    const rec = { start_at: "2026-08-15T22:00:00Z", end_at: "2026-08-16T02:00:00Z", recurring: true };
    expect(windowState(rec, Date.parse("2026-08-15T23:00:00Z"))).toBe("active");
    expect(windowState(rec, Date.parse("2026-08-16T01:00:00Z"))).toBe("active");
    expect(windowState(rec, Date.parse("2026-08-16T12:00:00Z"))).toBe("upcoming");
    expect(windowState(rec, Date.parse("2026-08-22T23:00:00Z"))).toBe("active"); // 下周六
  });

  it("isWindowActive matches windowState", () => {
    expect(isWindowActive(oneOff, now)).toBe(true);
    expect(isWindowActive({ ...oneOff, end_at: "2026-08-17T11:00:00Z" }, now)).toBe(false);
  });
});

describe("fmtMinutes", () => {
  it("formats compact durations", () => {
    expect(fmtMinutes(0)).toBe("0m");
    expect(fmtMinutes(45)).toBe("45m");
    expect(fmtMinutes(90)).toBe("1h 30m");
    expect(fmtMinutes(1440)).toBe("1d");
    expect(fmtMinutes(1500)).toBe("1d 1h");
    expect(fmtMinutes(NaN)).toBe("0m");
  });
});

describe("fmtAvailability", () => {
  it("formats percentages and unknown", () => {
    expect(fmtAvailability(99.95)).toBe("99.95%");
    expect(fmtAvailability(99.9)).toBe("99.90%");
    expect(fmtAvailability(null)).toBe("—");
    expect(fmtAvailability(undefined)).toBe("—");
    expect(fmtAvailability(NaN)).toBe("—");
  });
});

describe("monthLabel", () => {
  it("keeps ISO month", () => {
    expect(monthLabel("2026-08")).toBe("2026-08");
  });
});
