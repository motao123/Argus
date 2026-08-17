import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import Compare from "./Compare";
import { I18nProvider } from "../lib/i18n";

vi.mock("../lib/api", () => ({
  api: {
    servers: vi.fn(),
    metricsCompare: vi.fn(),
  },
}));

import { api, type MetricPoint, type Server } from "../lib/api";

// jsdom 无 ResizeObserver，recharts 的 ResponsiveContainer 依赖它出图；
// observe 时立即回调一个固定尺寸，让图表在测试中真实渲染。
class ResizeObserverStub {
  private cb: ResizeObserverCallback;
  constructor(cb: ResizeObserverCallback) {
    this.cb = cb;
  }
  observe() {
    this.cb([{ contentRect: { width: 800, height: 400 } }] as unknown as ResizeObserverEntry[], this as unknown as ResizeObserver);
  }
  unobserve() {}
  disconnect() {}
}
globalThis.ResizeObserver ??= ResizeObserverStub as unknown as typeof ResizeObserver;

const baseServer = {
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
  gpu: { available: false } as Server["gpu"],
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
  traffic_accounting: "sum" as const,
};

function makeServer(id: number, name: string, online = true): Server {
  return { ...baseServer, id, name, online };
}

function makePoint(ts: number, cpu: number): MetricPoint {
  return {
    ts,
    cpu,
    net_in: 0,
    net_out: 0,
    load1: 0,
    mem_used: 512,
    mem_total: 1024,
    disk_used: 0,
    disk_total: 0,
    process_count: 0,
    tcp_established: 0,
    tcp_listen: 0,
    udp_count: 0,
    disk_read_speed: 0,
    disk_write_speed: 0,
    disk_read_iops: 0,
    disk_write_iops: 0,
  };
}

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <I18nProvider>
      <QueryClientProvider client={qc}>
        <Compare />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.setItem("argus-lang", "zh-CN");
  vi.mocked(api.servers).mockResolvedValue({ servers: [makeServer(1, "Server 1"), makeServer(2, "Server 2")] });
  vi.mocked(api.metricsCompare).mockImplementation(async (ids, period) => ({
    period,
    series: ids.map((id) => ({
      server_id: id,
      server_name: `Server ${id}`,
      points: [makePoint(1700000000 + id, 10 + id)],
    })),
  }));
});

describe("Compare page — multi-server metric comparison", () => {
  it("preselects servers, renders controls and overlays one line per server", async () => {
    renderPage();
    // 默认勾选前两台并请求 24h 对比数据
    await waitFor(() => expect(api.metricsCompare).toHaveBeenCalledWith([1, 2], "24h"));
    // 图例按服务器名区分（列表与图例各出现一次）
    expect((await screen.findAllByText("Server 1")).length).toBeGreaterThan(0);
    expect((await screen.findAllByText("Server 2")).length).toBeGreaterThan(0);
    // 指标与时间范围控件（CPU 同时出现在指标按钮与图表标题，用 role 区分）
    expect(screen.getByRole("button", { name: /^CPU$/ })).toBeInTheDocument();
    expect(screen.getByText("内存")).toBeInTheDocument();
    expect(screen.getByText("1 小时")).toBeInTheDocument();
    expect(screen.getByText("24 小时")).toBeInTheDocument();
    expect(screen.getByText("7 天")).toBeInTheDocument();
    expect(screen.getByText("已选 2/10 台")).toBeInTheDocument();
  });

  it("switching period refetches with the new range", async () => {
    renderPage();
    await waitFor(() => expect(api.metricsCompare).toHaveBeenCalledWith([1, 2], "24h"));
    fireEvent.click(screen.getByText("7 天"));
    await waitFor(() => expect(api.metricsCompare).toHaveBeenCalledWith([1, 2], "7d"));
  });

  it("switching metric is client-side and does not refetch", async () => {
    renderPage();
    await waitFor(() => expect(api.metricsCompare).toHaveBeenCalledWith([1, 2], "24h"));
    fireEvent.click(screen.getByText("内存"));
    await new Promise((r) => setTimeout(r, 100));
    expect(api.metricsCompare).toHaveBeenCalledTimes(1);
  });

  it("unchecking a server removes it from the compare request", async () => {
    renderPage();
    await waitFor(() => expect(api.metricsCompare).toHaveBeenCalledWith([1, 2], "24h"));
    fireEvent.click(screen.getByRole("checkbox", { name: /Server 2/ }));
    await waitFor(() => expect(api.metricsCompare).toHaveBeenCalledWith([1], "24h"));
    expect(screen.getByText("已选 1/10 台")).toBeInTheDocument();
  });

  it("caps selection at 10 servers with a hint", async () => {
    const many = Array.from({ length: 12 }, (_, i) => makeServer(i + 1, `Srv ${i + 1}`));
    vi.mocked(api.servers).mockResolvedValue({ servers: many });
    renderPage();
    await waitFor(() => expect(api.metricsCompare).toHaveBeenCalledWith([1, 2, 3, 4, 5, 6, 7, 8, 9, 10], "24h"));
    // 第 11 台不可勾选，出现上限提示
    fireEvent.click(screen.getByRole("checkbox", { name: /^Srv 11$/ }));
    expect(screen.getByText("最多同时对比 10 台服务器")).toBeInTheDocument();
    expect(api.metricsCompare).not.toHaveBeenCalledWith(expect.arrayContaining([11]), expect.anything());
    // 取消一台后可继续勾选
    fireEvent.click(screen.getByRole("checkbox", { name: /^Srv 1$/ }));
    fireEvent.click(screen.getByRole("checkbox", { name: /^Srv 11$/ }));
    await waitFor(() => expect(api.metricsCompare).toHaveBeenCalledWith([2, 3, 4, 5, 6, 7, 8, 9, 10, 11], "24h"));
  });

  it("shows empty state when there are no servers", async () => {
    vi.mocked(api.servers).mockResolvedValue({ servers: [] });
    renderPage();
    expect(await screen.findByText("暂无可用服务器")).toBeInTheDocument();
    expect(api.metricsCompare).not.toHaveBeenCalled();
  });
});
