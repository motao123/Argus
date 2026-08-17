import { act, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { I18nProvider, detectLang, translate, translateError, useI18n } from "./i18n";
import { flattenMessages, locales, messages } from "../locales";

function Probe() {
  const { lang, setLang, t, fmtDate, fmtTime, fmtDateTime, fmtNumber, fmtRelativeTime, fmtDuration } = useI18n();
  return (
    <div>
      <span data-testid="lang">{lang}</span>
      <span data-testid="greeting">{t("common.login")}</span>
      <span data-testid="interp">{t("common.onlineOf", { online: 3, total: 5 })}</span>
      <span data-testid="missing">{t(`nope.${"nope"}` as never)}</span>
      <span data-testid="date">{fmtDate("2026-08-16T10:30:00Z")}</span>
      <span data-testid="time">{fmtTime(1755000000)}</span>
      <span data-testid="datetime">{fmtDateTime("2026-08-16T10:30:00Z")}</span>
      <span data-testid="number">{fmtNumber(12345.6)}</span>
      <span data-testid="relative">{fmtRelativeTime(3600)}</span>
      <span data-testid="duration">{fmtDuration(90061)}</span>
      <button onClick={() => setLang(lang === "zh-CN" ? "en" : "zh-CN")} data-testid="toggle">
        toggle
      </button>
    </div>
  );
}

function renderProbe() {
  return render(
    <I18nProvider>
      <Probe />
    </I18nProvider>,
  );
}

function ZeroProbe() {
  const { fmtDate, fmtDateTime, fmtDuration, fmtRelativeTime } = useI18n();
  return (
    <div>
      <span data-testid="zero-date">{fmtDate("0001-01-01T00:00:00Z")}</span>
      <span data-testid="zero-datetime">{fmtDateTime(0)}</span>
      <span data-testid="zero-duration">{fmtDuration(0)}</span>
      <span data-testid="zero-relative">{fmtRelativeTime(0)}</span>
    </div>
  );
}

describe("i18n framework", () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.lang = "";
  });

  it("detectLang: localStorage 优先，其次浏览器语言回退，最后 zh-CN", () => {
    // jsdom 的 navigator.language 为 en-US → 无存储时回退 en
    expect(detectLang()).toBe("en");
    localStorage.setItem("argus-lang", "zh-CN");
    expect(detectLang()).toBe("zh-CN");
    localStorage.setItem("argus-lang", "invalid");
    expect(detectLang()).toBe("en");
  });

  it("translate: 插值 + 缺失 key 原样返回", () => {
    expect(translate("zh-CN", "common.onlineOf", { online: 1, total: 2 })).toBe("在线 1/2");
    expect(translate("en", "common.onlineOf", { online: 1, total: 2 })).toBe("Online 1/2");
    // 未提供的占位符保留原样
    expect(translate("en", "common.onlineOf", { online: 1 })).toBe("Online 1/{total}");
    // 缺失 key
    expect(translate("en", "definitely.missing", { a: 1 })).toBe("definitely.missing");
  });

  it("语言包 key 完全一致且占位符一致（与 check-i18n.mjs 同一保证）", () => {
    const zh = flattenMessages(locales["zh-CN"]);
    const en = flattenMessages(locales.en);
    expect(Object.keys(zh).sort()).toEqual(Object.keys(en).sort());
    for (const key of Object.keys(zh)) {
      const placeholders = (template: string) => [...template.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();
      expect(placeholders(zh[key]), `占位符不一致: ${key}`).toEqual(placeholders(en[key]));
    }
  });

  it("默认语言（浏览器 en 回退）与切换：持久化 + html lang 同步 + 无刷新重渲染", () => {
    renderProbe();
    // jsdom navigator.language = en-US → 默认 en
    expect(screen.getByTestId("lang").textContent).toBe("en");
    expect(screen.getByTestId("greeting").textContent).toBe("Login");
    expect(document.documentElement.lang).toBe("en");
    expect(localStorage.getItem("argus-lang")).toBe("en");

    // 切换到 zh-CN：状态更新（无刷新），持久化并同步 <html lang>
    act(() => {
      screen.getByTestId("toggle").click();
    });
    expect(screen.getByTestId("lang").textContent).toBe("zh-CN");
    expect(screen.getByTestId("greeting").textContent).toBe("登录");
    expect(screen.getByTestId("interp").textContent).toBe("在线 3/5");
    expect(document.documentElement.lang).toBe("zh-CN");
    expect(localStorage.getItem("argus-lang")).toBe("zh-CN");
  });

  it("Intl 格式化：zh-CN 与 en 输出随语言切换", () => {
    renderProbe();
    // 初始 en
    expect(screen.getByTestId("date").textContent).toBe("08/16/2026");
    expect(screen.getByTestId("number").textContent).toBe("12,345.6");
    expect(screen.getByTestId("duration").textContent).toBe("1d 1h");
    act(() => {
      screen.getByTestId("toggle").click();
    });
    expect(screen.getByTestId("date").textContent).toBe("2026/08/16");
    expect(screen.getByTestId("number").textContent).toBe("12,345.6");
    expect(screen.getByTestId("duration").textContent).toBe("1天 1时");
  });

  it("fmtTime / fmtDateTime / fmtRelativeTime 输出合法且不抛错", () => {
    renderProbe();
    expect(screen.getByTestId("time").textContent).toMatch(/^\d{2}:\d{2}$/);
    expect(screen.getByTestId("datetime").textContent).not.toBe("");
    expect(screen.getByTestId("relative").textContent).not.toBe("");
  });

  it("translateError: 按后端稳定 code 翻译，未知 code / 无 code 回退原文", () => {
    expect(translateError("zh-CN", "server.offline", "server offline")).toBe("服务器离线");
    expect(translateError("en", "server.offline", "server offline")).toBe("Server offline");
    expect(translateError("zh-CN", "auth.invalid_credentials", "invalid credentials")).toBe("用户名或密码错误");
    // 被禁能力稳定码 → 友好提示
    expect(translateError("zh-CN", "capability.disabled", "capability disabled")).toContain("该能力已被禁用");
    expect(translateError("en", "capability.disabled", "capability disabled")).toContain("This capability is disabled");
    // 未知 code → 回退后端原文
    expect(translateError("en", "some.new_code", "raw backend message")).toBe("raw backend message");
    // 无 code → 回退原文
    expect(translateError("zh-CN", undefined, "raw backend message")).toBe("raw backend message");
  });

  it("空/零时间戳输出占位符 —", () => {
    render(
      <I18nProvider>
        <ZeroProbe />
      </I18nProvider>,
    );
    expect(screen.getByTestId("zero-date").textContent).toBe("—");
    expect(screen.getByTestId("zero-datetime").textContent).toBe("—");
    expect(screen.getByTestId("zero-duration").textContent).toBe("—");
    expect(screen.getByTestId("zero-relative").textContent).toBe("—");
  });
});
