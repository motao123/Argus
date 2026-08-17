import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import Waf from "./Waf";
import { I18nProvider } from "../lib/i18n";

vi.mock("../lib/api", () => ({
  api: {
    onlineUsers: vi.fn(),
    wafBans: vi.fn(),
    banIP: vi.fn(),
    unbanIP: vi.fn(),
  },
}));

import { api, type OnlineUser, type WafBan } from "../lib/api";

const online: OnlineUser[] = [
  { ip: "1.2.3.4", username: "admin", auth_method: "jwt", last_active_at: "2026-08-17T10:00:00Z", connections: 2 },
  { ip: "5.6.7.8", username: "", auth_method: "guest", last_active_at: "2026-08-17T09:00:00Z", connections: 0 },
];

const bans: WafBan[] = [
  {
    id: 1,
    ip: "9.9.9.9",
    reason: "abuse",
    count: 2,
    source: "rate",
    banned_at: "2026-08-17T08:00:00Z",
    expire_at: "2026-08-17T20:00:00Z",
    created_at: "2026-08-17T08:00:00Z",
  },
];

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <I18nProvider>
      <QueryClientProvider client={qc}>
        <Waf />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

beforeEach(() => {
  localStorage.setItem("argus-lang", "zh-CN");
  vi.mocked(api.onlineUsers).mockClear().mockResolvedValue({ online });
  vi.mocked(api.wafBans).mockClear().mockResolvedValue({ bans, pagination: { total: 1 } });
  vi.mocked(api.banIP).mockClear().mockResolvedValue({ ban: bans[0] } as never);
  vi.mocked(api.unbanIP).mockClear().mockResolvedValue({ ok: true } as never);
});

describe("Waf page", () => {
  it("renders online users and ban records", async () => {
    renderPage();
    expect(await screen.findByText("1.2.3.4")).toBeInTheDocument();
    expect(screen.getByText("admin")).toBeInTheDocument();
    expect(screen.getByText("游客")).toBeInTheDocument();
    expect(screen.getByText("9.9.9.9")).toBeInTheDocument();
    expect(screen.getByText("速率超限")).toBeInTheDocument();
    expect(screen.getByText("abuse")).toBeInTheDocument();
  });

  it("bans a single online user after confirm", async () => {
    renderPage();
    await screen.findByText("1.2.3.4");
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);
    fireEvent.click(screen.getAllByTitle("封禁 1.2.3.4")[0]);
    await waitFor(() => {
      expect(vi.mocked(api.banIP).mock.calls[0]?.[0]).toBe("1.2.3.4");
    });
    confirmSpy.mockRestore();
  });

  it("bans selected users in batch", async () => {
    renderPage();
    await screen.findByText("1.2.3.4");
    // 勾选两个在线用户
    fireEvent.click(screen.getAllByRole("checkbox")[1]); // 1.2.3.4
    fireEvent.click(screen.getAllByRole("checkbox")[2]); // 5.6.7.8
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);
    fireEvent.click(screen.getByText("封禁选中"));
    await waitFor(() => {
      expect(vi.mocked(api.banIP).mock.calls.length).toBe(2);
    });
    expect(vi.mocked(api.banIP).mock.calls[0]?.[0]).toBe("1.2.3.4");
    expect(vi.mocked(api.banIP).mock.calls[1]?.[0]).toBe("5.6.7.8");
    confirmSpy.mockRestore();
  });

  it("bans an IP from the manual form", async () => {
    renderPage();
    await screen.findByText("1.2.3.4");
    fireEvent.change(screen.getByPlaceholderText("IP 地址，如 1.2.3.4"), { target: { value: "10.0.0.1" } });
    fireEvent.change(screen.getByPlaceholderText("原因（可选）"), { target: { value: "spam" } });
    fireEvent.click(screen.getByText("封禁"));
    await waitFor(() => {
      expect(vi.mocked(api.banIP).mock.calls[0]?.[0]).toBe("10.0.0.1");
      expect(vi.mocked(api.banIP).mock.calls[0]?.[1]).toBe("spam");
      expect(vi.mocked(api.banIP).mock.calls[0]?.[2]).toBe(24);
    });
  });

  it("unbans an IP after confirm", async () => {
    renderPage();
    await screen.findByText("9.9.9.9");
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);
    fireEvent.click(screen.getAllByTitle("解封")[0]);
    await waitFor(() => {
      expect(vi.mocked(api.unbanIP).mock.calls[0]?.[0]).toBe("9.9.9.9");
    });
    confirmSpy.mockRestore();
  });
});
