import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import Overview from "./Overview";
import { I18nProvider } from "../lib/i18n";

vi.mock("../lib/api", () => ({
  api: { top: vi.fn() },
  countryFlag: () => "",
}));

vi.mock("../context/servers", () => ({
  useServers: () => ({
    servers: [makeServer()],
    online: 1,
    total: 1,
    wsStatus: "connected",
  }),
}));

import { api, type Server, type TopMetric, type TopServerEntry } from "../lib/api";

// 资源排行 mock 数据：每指标返回一台服务器
const topByMetric: Record<TopMetric, TopServerEntry[]> = {
  cpu: [{ server_id: 1, server_name: "alice-srv", value: 88.2 }],
  mem: [{ server_id: 1, server_name: "alice-srv", value: 75, used: 3221225472, total: 4294967296 }],
  disk: [{ server_id: 1, server_name: "alice-srv", value: 90, used: 52428800, total: 104857600 }],
  net_in: [],
  net_out: [],
  latency: [{ server_id: 1, server_name: "alice-srv", value: 45 }],
};

function makeServer(): Server {
  return {
    id: 1,
    name: "alice-srv",
    group: "",
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
    latency_ms: 0,
    online: true,
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
    traffic_cycle_day: 0,
    traffic_timezone: "UTC",
    traffic_accounting: "sum",
  };
}

function renderPage() {
  return render(
    <I18nProvider>
      <MemoryRouter>
        <Overview />
      </MemoryRouter>
    </I18nProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.setItem("argus-lang", "zh-CN");
  vi.mocked(api.top).mockImplementation(async (metric) => ({
    metric,
    limit: 5,
    servers: topByMetric[metric],
  }));
});

describe("Overview — resource ranking panel", () => {
  it("renders four top lists with server name, value and empty state", async () => {
    renderPage();
    // 面板标题与四个榜单（CPU/内存/磁盘同时出现在下方排序下拉，榜单内用 within 定位）
    expect(screen.getByText("资源排行")).toBeInTheDocument();
    const panel = screen.getByText("资源排行").closest("div")!;
    expect(within(panel).getByText("CPU")).toBeInTheDocument();
    expect(within(panel).getByText("内存")).toBeInTheDocument();
    expect(within(panel).getByText("磁盘")).toBeInTheDocument();
    expect(within(panel).getByText("延迟")).toBeInTheDocument();

    // 数值：CPU/内存/磁盘为百分比，延迟为毫秒
    expect(await screen.findByText("88.2%")).toBeInTheDocument();
    expect(screen.getByText("75.0%")).toBeInTheDocument();
    expect(screen.getByText("90.0%")).toBeInTheDocument();
    expect(screen.getByText("45ms")).toBeInTheDocument();

    // 服务器名出现（榜单行 + 下方卡片）
    expect((await screen.findAllByText("alice-srv")).length).toBeGreaterThan(0);

    // 每行是跳转详情的链接
    const rows = screen.getAllByRole("link");
    expect(rows.some((r) => r.getAttribute("href") === "/server/1")).toBe(true);
  });

  it("fetches each metric with limit 5 and refreshes every 10s", async () => {
    const intervalSpy = vi.spyOn(globalThis, "setInterval");
    try {
      renderPage();
      // 初始并行拉取四个指标（各 limit=5）
      await waitFor(() => expect(api.top).toHaveBeenCalledTimes(4));
      for (const metric of ["cpu", "mem", "disk", "latency"] as TopMetric[]) {
        expect(api.top).toHaveBeenCalledWith(metric, 5);
      }
      // 手动触发 10s 轮询回调，验证定时刷新
      const intervalCall = intervalSpy.mock.calls.find((c) => c[1] === 10_000);
      expect(intervalCall).toBeDefined();
      await act(async () => {
        (intervalCall![0] as () => void)();
      });
      await waitFor(() => expect(api.top).toHaveBeenCalledTimes(8));
    } finally {
      intervalSpy.mockRestore();
    }
  });

  it("shows empty state when a list has no data", async () => {
    // 磁盘榜置空后再渲染，验证占位文案
    vi.mocked(api.top).mockImplementation(async (metric) => ({
      metric,
      limit: 5,
      servers: metric === "disk" ? [] : topByMetric[metric],
    }));
    renderPage();
    expect(await screen.findByText("暂无数据")).toBeInTheDocument();
  });
});
