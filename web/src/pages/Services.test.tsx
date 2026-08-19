import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import Services from "./Services";
import { I18nProvider } from "../lib/i18n";

vi.mock("../lib/api", () => ({
  api: {
    services: vi.fn(),
    saveService: vi.fn(),
    deleteService: vi.fn(),
    serviceHistory: vi.fn(),
    notifications: vi.fn(),
    notificationGroups: vi.fn(),
    crons: vi.fn(),
  },
}));

vi.mock("../context/servers", () => ({
  useServers: () => ({
    servers: [
      { id: 7, name: "node-1" },
      { id: 8, name: "node-2" },
    ],
    online: 2,
    total: 2,
    wsStatus: "connected" as const,
  }),
}));

import { api } from "../lib/api";

const mockService = {
  id: 1,
  owner_id: 0,
  server_id: 7,
  server_ids: [7],
  name: "api",
  type: "http",
  target: "https://example.com/health",
  interval: 60,
  enabled: true,
  hidden: false,
  notify: false,
  notify_webhook_id: 0,
  notification_group_id: 0,
  http_method: "POST",
  verify_tls: true,
  timeout: 10,
  expected_status_min: 200,
  expected_status_max: 399,
  expected_statuses: "",
  ping_count: 4,
  cert_warn: true,
  request_headers: '[{"key":"Authorization","value":"Bearer t"}]',
  request_body: '{"ping":1}',
  assert_contains: '"ok":true',
  failure_trigger_cron_id: 0,
  recovery_trigger_cron_id: 0,
  last_up: true,
  last_delay: 12,
  last_check_at: 0,
  today_up_rate: 100,
  availability: 100,
  min_delay: 5,
  avg_delay: 12,
  max_delay: 30,
  delay_p50: null as number | null,
  delay_p95: null as number | null,
  delay_p99: null as number | null,
  delay_stddev_ms: null as number | null,
  delay_jitter_ms: null as number | null,
  loss_rate: 0,
  status_code: 200,
  cert_days: 30,
  dns_ms: 1,
  connect_ms: 2,
  tls_ms: 3,
  ttfb_ms: 4,
};

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <I18nProvider>
      <QueryClientProvider client={qc}>
        <Services />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

beforeEach(() => {
  localStorage.setItem("argus-lang", "zh-CN");
  vi.mocked(api.services).mockResolvedValue({ services: [mockService] } as never);
  vi.mocked(api.saveService).mockResolvedValue({ ok: true } as never);
  vi.mocked(api.deleteService).mockResolvedValue({ ok: true } as never);
  vi.mocked(api.serviceHistory).mockResolvedValue({ period: "1d", points: [] } as never);
  vi.mocked(api.notifications).mockResolvedValue({ notifications: [] } as never);
  vi.mocked(api.notificationGroups).mockResolvedValue({ groups: [] } as never);
  vi.mocked(api.crons).mockResolvedValue({ crons: [] } as never);
});

describe("Services page", () => {
  it("creates a service with custom method, headers, body and assertion", async () => {
    renderPage();
    await screen.findByText("api");
    fireEvent.click(screen.getByText("新建服务"));

    // 方法下拉支持 POST/PUT/PATCH/DELETE
    const methodSelect = screen.getByDisplayValue("GET") as HTMLSelectElement;
    fireEvent.change(methodSelect, { target: { value: "POST" } });
    expect((screen.getByDisplayValue("POST") as HTMLSelectElement).value).toBe("POST");
    for (const m of ["GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"]) {
      expect(screen.getByRole("option", { name: m })).toBeInTheDocument();
    }

    fireEvent.change(screen.getByPlaceholderText("名称"), { target: { value: "post-svc" } });
    fireEvent.change(screen.getByPlaceholderText("目标（URL / host:port / host）"), { target: { value: "https://example.com" } });
    fireEvent.change(screen.getByPlaceholderText("Authorization: token 123"), { target: { value: "X-Api-Key: secret\nHost: api.example.com" } });
    fireEvent.change(screen.getByPlaceholderText("仅 POST/PUT/PATCH 时发送"), { target: { value: "hello" } });
    fireEvent.change(screen.getByPlaceholderText("响应体中须包含的关键字（留空=不启用）"), { target: { value: "ok" } });

    fireEvent.click(screen.getByText("保存"));
    await waitFor(() => {
      expect(api.saveService).toHaveBeenCalledWith(expect.objectContaining({
        http_method: "POST",
        request_headers: '[{"key":"X-Api-Key","value":"secret"},{"key":"Host","value":"api.example.com"}]',
        request_body: "hello",
        assert_contains: "ok",
      }));
    });
  });

  it("edits an existing service and pre-fills header lines from stored JSON", async () => {
    renderPage();
    await screen.findByText("api");
    fireEvent.click(screen.getByTitle("编辑"));

    expect((screen.getByDisplayValue("POST") as HTMLSelectElement).value).toBe("POST");
    const headersArea = screen.getByPlaceholderText("Authorization: token 123") as HTMLTextAreaElement;
    expect(headersArea.value).toBe("Authorization: Bearer t");
    expect((screen.getByPlaceholderText("仅 POST/PUT/PATCH 时发送") as HTMLTextAreaElement).value).toBe('{"ping":1}');
    expect((screen.getByPlaceholderText("响应体中须包含的关键字（留空=不启用）") as HTMLInputElement).value).toBe('"ok":true');

    fireEvent.click(screen.getByText("保存"));
    await waitFor(() => {
      expect(api.saveService).toHaveBeenCalledWith(expect.objectContaining({
        id: 1,
        http_method: "POST",
        request_headers: '[{"key":"Authorization","value":"Bearer t"}]',
      }));
    });
  });

  it("creates a service with multiple probe servers and submits the default server", async () => {
    renderPage();
    await screen.findByText("api");
    fireEvent.click(screen.getByText("新建服务"));

    const node1 = screen.getByRole("checkbox", { name: "node-1" }) as HTMLInputElement;
    const node2 = screen.getByRole("checkbox", { name: "node-2" }) as HTMLInputElement;
    expect(node1.checked).toBe(true);
    expect(node2.checked).toBe(false);

    fireEvent.click(node2);
    fireEvent.change(screen.getByPlaceholderText("名称"), { target: { value: "multi-probe" } });
    fireEvent.change(screen.getByPlaceholderText("目标（URL / host:port / host）"), { target: { value: "https://example.com" } });
    fireEvent.click(screen.getByText("保存"));

    await waitFor(() => {
      expect(api.saveService).toHaveBeenCalledWith(expect.objectContaining({
        server_id: 7,
        server_ids: [7, 8],
      }));
    });
  });

  it("pre-fills all probe servers and updates the default after removing one", async () => {
    vi.mocked(api.services).mockResolvedValue({
      services: [{ ...mockService, server_ids: [7, 8] }],
    } as never);
    renderPage();
    await screen.findByText("api");
    fireEvent.click(screen.getByTitle("编辑"));

    const node1 = screen.getByRole("checkbox", { name: "node-1" }) as HTMLInputElement;
    const node2 = screen.getByRole("checkbox", { name: "node-2" }) as HTMLInputElement;
    expect(node1.checked).toBe(true);
    expect(node2.checked).toBe(true);

    fireEvent.click(node1);
    fireEvent.click(screen.getByText("保存"));
    await waitFor(() => {
      expect(api.saveService).toHaveBeenCalledWith(expect.objectContaining({
        id: 1,
        server_id: 8,
        server_ids: [8],
      }));
    });
  });

  it("disables the body input for GET/HEAD methods", async () => {
    renderPage();
    await screen.findByText("api");
    fireEvent.click(screen.getByText("新建服务"));
    const bodyArea = screen.getByPlaceholderText("仅 POST/PUT/PATCH 时发送") as HTMLTextAreaElement;
    expect(bodyArea.disabled).toBe(true);
    fireEvent.change(screen.getByDisplayValue("GET"), { target: { value: "PUT" } });
    expect((screen.getByDisplayValue("PUT") as HTMLSelectElement).value).toBe("PUT");
    expect((screen.getByPlaceholderText("仅 POST/PUT/PATCH 时发送") as HTMLTextAreaElement).disabled).toBe(false);
  });

  it("submits the expected statuses list (blank = range mode)", async () => {
    renderPage();
    await screen.findByText("api");
    fireEvent.click(screen.getByText("新建服务"));

    const listInput = screen.getByTitle("期望状态码（逗号分隔，留空=区间）") as HTMLInputElement;
    expect(listInput.value).toBe("");
    fireEvent.change(listInput, { target: { value: "200, 301, 404" } });
    fireEvent.change(screen.getByPlaceholderText("名称"), { target: { value: "multi-status" } });
    fireEvent.change(screen.getByPlaceholderText("目标（URL / host:port / host）"), { target: { value: "https://example.com" } });

    fireEvent.click(screen.getByText("保存"));
    await waitFor(() => {
      expect(api.saveService).toHaveBeenCalledWith(expect.objectContaining({
        expected_statuses: "200, 301, 404",
        expected_status_min: 200,
        expected_status_max: 399,
      }));
    });
  });

  it("pre-fills the expected statuses list when editing", async () => {
    vi.mocked(api.services).mockResolvedValue({
      services: [{ ...mockService, expected_statuses: "301,404" }],
    } as never);
    renderPage();
    await screen.findByText("api");
    fireEvent.click(screen.getByTitle("编辑"));
    expect((screen.getByTitle("期望状态码（逗号分隔，留空=区间）") as HTMLInputElement).value).toBe("301,404");
  });
});

// ---- P1：延迟分位数统计展示（滑动窗口快照；缺样本为 null → 显示 —） ----
describe("Services page — delay quantile stats", () => {
  it("shows p50/p95/p99/stddev/jitter when window samples are sufficient", async () => {
    vi.mocked(api.services).mockResolvedValue({
      services: [{ ...mockService, delay_p50: 50, delay_p95: 95, delay_p99: 99, delay_stddev_ms: 10, delay_jitter_ms: 8 }],
    } as never);
    renderPage();
    fireEvent.click(await screen.findByText("api"));
    expect(await screen.findByText("P50 50ms")).toBeInTheDocument();
    expect(screen.getByText("P95 95ms")).toBeInTheDocument();
    expect(screen.getByText("P99 99ms")).toBeInTheDocument();
    expect(screen.getByText("标准差 10ms")).toBeInTheDocument();
    expect(screen.getByText("抖动 8ms")).toBeInTheDocument();
  });

  it("shows — for all quantile stats when samples are missing (null)", async () => {
    vi.mocked(api.services).mockResolvedValue({ services: [mockService] } as never);
    renderPage();
    fireEvent.click(await screen.findByText("api"));
    expect(await screen.findByText("P50 —")).toBeInTheDocument();
    expect(screen.getByText("P95 —")).toBeInTheDocument();
    expect(screen.getByText("P99 —")).toBeInTheDocument();
    expect(screen.getByText("标准差 —")).toBeInTheDocument();
    expect(screen.getByText("抖动 —")).toBeInTheDocument();
  });
});
