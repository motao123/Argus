import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import Clipboard from "./Clipboard";
import { I18nProvider } from "../lib/i18n";

vi.mock("../lib/api", () => ({
  api: {
    clipboard: vi.fn(),
    createClipboard: vi.fn(),
    updateClipboard: vi.fn(),
    deleteClipboard: vi.fn(),
  },
}));

import { api } from "../lib/api";

const mockItems = [
  { id: 1, user_id: 0, title: "SSH 命令", content: "ssh root@example.com", created_at: "2026-08-17T10:00:00Z" },
  { id: 2, user_id: 0, title: "", content: "docker compose up -d", created_at: "2026-08-17T11:00:00Z" },
];

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <I18nProvider>
      <QueryClientProvider client={qc}>
        <Clipboard />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

beforeEach(() => {
  localStorage.setItem("argus-lang", "zh-CN");
  vi.mocked(api.clipboard).mockResolvedValue({ items: mockItems });
  vi.mocked(api.createClipboard).mockResolvedValue({ id: 3, user_id: 0, title: "", content: "new", created_at: "" } as never);
  vi.mocked(api.updateClipboard).mockResolvedValue({} as never);
  vi.mocked(api.deleteClipboard).mockResolvedValue({ ok: true } as never);
  // jsdom 无 navigator.clipboard，测试里提供 stub
  Object.defineProperty(navigator, "clipboard", {
    value: { writeText: vi.fn().mockResolvedValue(undefined) },
    configurable: true,
  });
});

describe("Clipboard page", () => {
  it("renders items with content and untitled fallback", async () => {
    renderPage();
    expect(await screen.findByText("SSH 命令")).toBeInTheDocument();
    expect(screen.getByText("ssh root@example.com")).toBeInTheDocument();
    expect(screen.getByText("docker compose up -d")).toBeInTheDocument();
    expect(screen.getByText("无标题")).toBeInTheDocument();
  });

  it("creates a new entry with content", async () => {
    renderPage();
    await screen.findByText("SSH 命令");
    fireEvent.click(screen.getByText("新建条目"));
    fireEvent.change(screen.getByPlaceholderText("粘贴或输入要暂存的内容…"), { target: { value: "curl -fsSL https://example.com" } });
    fireEvent.click(screen.getByText("保存"));
    await waitFor(() => {
      expect(api.createClipboard).toHaveBeenCalledWith({ title: undefined, content: "curl -fsSL https://example.com" });
    });
  });

  it("copies item content to clipboard", async () => {
    renderPage();
    await screen.findByText("SSH 命令");
    fireEvent.click(screen.getAllByTitle("复制")[0]);
    await waitFor(() => {
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith("ssh root@example.com");
    });
  });

  it("edits an item title", async () => {
    renderPage();
    await screen.findByText("SSH 命令");
    fireEvent.click(screen.getAllByTitle("编辑")[0]);
    fireEvent.change(screen.getByPlaceholderText("标题（可选）"), { target: { value: "改名" } });
    fireEvent.click(screen.getByText("保存"));
    await waitFor(() => {
      expect(api.updateClipboard).toHaveBeenCalledWith(1, { title: "改名", content: "ssh root@example.com" });
    });
  });

  it("deletes an item after confirm", async () => {
    renderPage();
    await screen.findByText("SSH 命令");
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);
    fireEvent.click(screen.getAllByTitle("删除")[0]);
    await waitFor(() => {
      expect(vi.mocked(api.deleteClipboard).mock.calls[0]?.[0]).toBe(1);
    });
    confirmSpy.mockRestore();
  });
});
