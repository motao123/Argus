import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import Servers from "./Servers";
import { I18nProvider } from "../lib/i18n";

vi.mock("../lib/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../lib/api")>();
  return {
    ...actual,
    api: {
      servers: vi.fn(),
      groups: vi.fn(),
      createGroup: vi.fn(),
      deleteGroup: vi.fn(),
      createServer: vi.fn(),
      updateServer: vi.fn(),
      deleteServer: vi.fn(),
      batchDeleteServers: vi.fn(),
      batchMoveServers: vi.fn(),
      applyServerConfig: vi.fn(),
      serverInstallCommand: vi.fn(),
      exec: vi.fn(),
      ddns: vi.fn(),
      batchConfigServers: vi.fn(),
      batchDDNSServers: vi.fn(),
    },
  };
});

import { api, type DDNSProfile, type Server } from "../lib/api";

const server = (id: number, name: string, online: boolean): Server => ({
  id,
  name,
  group: "default",
  note: "",
  cpu: 0,
  mem_used: 0,
  mem_total: 0,
  disk_used: 0,
  disk_total: 0,
  net_in_speed: 0,
  net_out_speed: 0,
  load1: 0,
  temperature: 0,
  gpu_util: 0,
  gpu: { available: false },
  process_count: 0,
  tcp_established: 0,
  tcp_listen: 0,
  udp_count: 0,
  disk_read_speed: 0,
  disk_write_speed: 0,
  disk_read_iops: 0,
  disk_write_iops: 0,
  disk_io_availability: { available: false },
  socket_availability: { available: false },
  process_availability: { available: false },
  temperature_availability: { available: false },
  uptime: 0,
  online,
  last_seen: "",
  price: 0,
  cycle_days: 0,
  expire_at: null,
  auto_renew: false,
  tags: "",
  sort_order: 0,
  hidden: false,
  owner_id: 0,
  traffic_quota_bytes: 0,
  traffic_cycle_day: 1,
  traffic_timezone: "UTC",
  traffic_accounting: "sum",
});

const mockServers = [server(1, "srv-a", true), server(2, "srv-b", false)];
const mockProfile: DDNSProfile = {
  id: 10,
  owner_id: 0,
  server_id: 1,
  name: "home-ddns",
  provider: "webhook",
  record_type: "A",
  domains: "home.example",
  webhook_url: "https://example.com/hook",
  webhook_method: "GET",
  webhook_headers: "{}",
  webhook_body: "",
  last_ip: "",
  last_updated: "",
  enabled: true,
  created_at: "",
  records: [],
};

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <I18nProvider>
      <QueryClientProvider client={qc}>
        <MemoryRouter>
          <Servers />
        </MemoryRouter>
      </QueryClientProvider>
    </I18nProvider>,
  );
}

beforeEach(() => {
  localStorage.setItem("argus-lang", "zh-CN");
  vi.mocked(api.servers).mockResolvedValue({ servers: mockServers });
  vi.mocked(api.groups).mockResolvedValue({ groups: [] });
  vi.mocked(api.ddns).mockResolvedValue({ profiles: [mockProfile] });
  vi.mocked(api.batchConfigServers).mockResolvedValue({
    results: [
      { server_id: 1, server_name: "srv-a", status: "ok" },
      { server_id: 2, server_name: "srv-b", status: "offline" },
    ],
  } as never);
  vi.mocked(api.batchDDNSServers).mockResolvedValue({
    results: [
      { server_id: 1, server_name: "srv-a", status: "ok", profile_id: 11 },
      { server_id: 2, server_name: "srv-b", status: "no_ip", profile_id: 12 },
    ],
  } as never);
});

describe("Servers batch operations", () => {
  // 点击表格每行首列的多选框（thead 的全选按钮与工具按钮不在 tbody 内）。
  function selectRows(count: number) {
    const tbody = document.querySelector("tbody");
    expect(tbody).not.toBeNull();
    const rowToggles = Array.from(tbody!.querySelectorAll("tr td:first-child button"));
    rowToggles.slice(0, count).forEach((b) => fireEvent.click(b));
  }

  it("batch config pushes capabilities and returns per-server results", async () => {
    renderPage();
    await screen.findByText("srv-a");
    selectRows(2);

    fireEvent.click(screen.getByText("批量配置"));
    fireEvent.change(screen.getByPlaceholderText("上报间隔（秒）"), { target: { value: "5" } });
    fireEvent.click(screen.getByText("下发到全部"));

    await waitFor(() => {
      expect(api.batchConfigServers).toHaveBeenCalledWith(
        [1, 2],
        expect.objectContaining({ interval: 5, capabilities: expect.any(Object) }),
      );
    });
    // 结果展示：成功 + 离线
    expect(await screen.findByText("成功 1 · 失败 1")).toBeInTheDocument();
    expect(screen.getByText("离线（已跳过）")).toBeInTheDocument();
  });

  it("batch DDNS applies a selected profile and shows results", async () => {
    renderPage();
    await screen.findByText("srv-a");
    selectRows(2);

    fireEvent.click(screen.getByText("批量 DDNS"));
    // 等待 DDNS profile 从查询加载出来
    await screen.findByText(/home-ddns/);
    // 选择 profile（展示为 "home-ddns · srv-a · home.example"）；页面存在分组下拉，需定位到 DDNS 下拉。
    const ddnsSelect = screen
      .getAllByRole("combobox")
      .find((s) => Array.from(s.querySelectorAll("option")).some((o) => o.textContent === "选择 DDNS 配置…"));
    expect(ddnsSelect).toBeDefined();
    fireEvent.change(ddnsSelect!, { target: { value: "10" } });
    fireEvent.click(screen.getByText("应用"));

    await waitFor(() => {
      expect(api.batchDDNSServers).toHaveBeenCalledWith([1, 2], 10);
    });
    expect(await screen.findByText("成功 1 · 失败 1")).toBeInTheDocument();
    expect(screen.getByText("已创建，等待 Agent 上报 IP")).toBeInTheDocument();
  });
});
