// 里程碑8 主题框架测试：明暗模式统一状态、服务端主题注入、降级默认。
import { act, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ThemeProvider, applyThemeCss, detectMode, themeCssUrl, useTheme } from "./theme";

function Probe() {
  const { mode, toggleMode, active } = useTheme();
  return (
    <div>
      <span data-testid="mode">{mode}</span>
      <span data-testid="active">{active.name}</span>
      <button onClick={toggleMode} data-testid="toggle">
        toggle
      </button>
    </div>
  );
}

function renderProbe() {
  return render(
    <ThemeProvider>
      <Probe />
    </ThemeProvider>,
  );
}

/** 模拟 /api/v1/public/settings 返回。 */
function mockPublicSettings(activeTheme: string, entry = "css/theme.css") {
  return vi.stubGlobal("fetch", vi.fn().mockResolvedValue({
    json: async () => ({ data: { active_theme: activeTheme, active_theme_entry: entry } }),
  }));
}

describe("theme framework", () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.classList.remove("dark");
    document.getElementById("argus-theme-css")?.remove();
    vi.unstubAllGlobals();
  });

  it("detectMode: localStorage 优先，其次 <html> class，最后 light", () => {
    expect(detectMode()).toBe("light");
    document.documentElement.classList.add("dark");
    expect(detectMode()).toBe("dark");
    localStorage.setItem("argus-theme", "light");
    expect(detectMode()).toBe("light");
    localStorage.setItem("argus-theme", "dark");
    expect(detectMode()).toBe("dark");
    localStorage.setItem("argus-theme", "invalid");
    expect(detectMode()).toBe("dark"); // class 回退仍生效
  });

  it("themeCssUrl: 名称与路径分段编码", () => {
    expect(themeCssUrl("midnight", "css/theme.css")).toBe("/theme-assets/midnight/css/theme.css");
    expect(themeCssUrl("a b", "css/theme.css")).toBe("/theme-assets/a%20b/css/theme.css");
    expect(themeCssUrl("midnight", "css/theme%20x.css")).toBe("/theme-assets/midnight/css/theme%2520x.css");
  });

  it("applyThemeCss: 非默认主题注入 link，default 移除 link", () => {
    applyThemeCss({ name: "midnight", entry: "css/theme.css" });
    const link = document.getElementById("argus-theme-css") as HTMLLinkElement | null;
    expect(link).not.toBeNull();
    expect(link?.getAttribute("href")).toBe("/theme-assets/midnight/css/theme.css");
    expect(link?.getAttribute("rel")).toBe("stylesheet");

    // 替换：切换主题只保留一个 link
    applyThemeCss({ name: "ocean", entry: "ocean.css" });
    const links = document.querySelectorAll("#argus-theme-css");
    expect(links.length).toBe(1);
    expect((links[0] as HTMLLinkElement).href).toContain("/theme-assets/ocean/ocean.css");

    // 回退默认 → 移除
    applyThemeCss({ name: "default", entry: "" });
    expect(document.getElementById("argus-theme-css")).toBeNull();
    applyThemeCss(null);
    expect(document.getElementById("argus-theme-css")).toBeNull();
  });

  it("Provider: 挂载后拉取服务端主题并注入入口 CSS", async () => {
    mockPublicSettings("midnight");
    renderProbe();
    await waitFor(() => expect(screen.getByTestId("active").textContent).toBe("midnight"));
    const link = document.getElementById("argus-theme-css") as HTMLLinkElement | null;
    expect(link?.getAttribute("href")).toBe("/theme-assets/midnight/css/theme.css");
  });

  it("Provider: 服务端为 default 时不注入 CSS", async () => {
    mockPublicSettings("default", "");
    renderProbe();
    await waitFor(() => expect(screen.getByTestId("active").textContent).toBe("default"));
    expect(document.getElementById("argus-theme-css")).toBeNull();
  });

  it("Provider: 拉取失败（Mock 模式）回退默认主题，不注入 CSS", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("network down")));
    renderProbe();
    await waitFor(() => expect(screen.getByTestId("active").textContent).toBe("default"));
    expect(document.getElementById("argus-theme-css")).toBeNull();
  });

  it("Provider: toggleMode 三态循环 light → dark → system 并持久化 localStorage + <html> class", () => {
    mockPublicSettings("default");
    renderProbe();
    expect(screen.getByTestId("mode").textContent).toBe("light");
    act(() => screen.getByTestId("toggle").click());
    expect(screen.getByTestId("mode").textContent).toBe("dark");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    expect(localStorage.getItem("argus-theme")).toBe("dark");
    act(() => screen.getByTestId("toggle").click());
    expect(screen.getByTestId("mode").textContent).toBe("system");
    expect(localStorage.getItem("argus-theme")).toBe("system");
    act(() => screen.getByTestId("toggle").click());
    expect(screen.getByTestId("mode").textContent).toBe("light");
    expect(document.documentElement.classList.contains("dark")).toBe(false);
    expect(localStorage.getItem("argus-theme")).toBe("light");
  });
});
