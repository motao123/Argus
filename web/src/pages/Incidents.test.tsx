import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import Incidents from "./Incidents";
import { I18nProvider } from "../lib/i18n";

vi.mock("../lib/api", () => ({
  api: {
    incidents: vi.fn(),
    maintenanceWindows: vi.fn(),
    servers: vi.fn(),
    saveIncident: vi.fn(),
    saveMaintenanceWindow: vi.fn(),
    resolveIncident: vi.fn(),
    deleteIncident: vi.fn(),
    deleteMaintenanceWindow: vi.fn(),
  },
}));

import { api } from "../lib/api";

const mockIncident = {
  id: 1,
  owner_id: 0,
  title: "DB 故障",
  severity: "critical" as const,
  status: "ongoing" as const,
  server_ids: "",
  notes: "排查中",
  start_at: "2026-08-16T10:00:00Z",
  end_at: null as string | null,
  created_at: "2026-08-16T10:00:00Z",
  updated_at: "2026-08-16T10:00:00Z",
};

const mockWindow = {
  id: 2,
  owner_id: 0,
  title: "每周维护",
  server_ids: "1",
  start_at: "2026-08-15T22:00:00Z",
  end_at: "2026-08-16T02:00:00Z",
  recurring: true,
  created_at: "2026-08-10T00:00:00Z",
  updated_at: "2026-08-10T00:00:00Z",
};

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <I18nProvider>
      <QueryClientProvider client={qc}>
        <Incidents />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

beforeEach(() => {
  localStorage.setItem("argus-lang", "zh-CN");
  vi.mocked(api.incidents).mockResolvedValue({ incidents: [mockIncident] });
  vi.mocked(api.maintenanceWindows).mockResolvedValue({ windows: [mockWindow] });
  vi.mocked(api.servers).mockResolvedValue({ servers: [] });
  vi.mocked(api.saveIncident).mockResolvedValue({} as never);
  vi.mocked(api.saveMaintenanceWindow).mockResolvedValue({} as never);
});

describe("Incidents admin page", () => {
  it("renders incident timeline and maintenance windows", async () => {
    renderPage();
    expect(await screen.findByText("DB 故障")).toBeInTheDocument();
    expect(screen.getByText("每周维护")).toBeInTheDocument();
    expect(screen.getByText("处理中")).toBeInTheDocument();
    expect(screen.getByText("每周重复")).toBeInTheDocument();
  });

  it("creates an incident with converted ISO timestamps", async () => {
    renderPage();
    await screen.findByText("DB 故障");
    fireEvent.click(screen.getByText("新建事故"));
    fireEvent.change(screen.getByPlaceholderText("标题"), { target: { value: "网络抖动" } });
    fireEvent.change(screen.getByLabelText("开始时间"), { target: { value: "2026-08-17T10:00" } });
    fireEvent.click(screen.getByText("保存"));

    await waitFor(() => {
      expect(api.saveIncident).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "网络抖动",
          start_at: new Date("2026-08-17T10:00").toISOString(),
        }),
      );
    });
  });

  it("creates a maintenance window and toggles recurring", async () => {
    renderPage();
    await screen.findByText("每周维护");
    fireEvent.click(screen.getByText("新建维护窗口"));
    fireEvent.change(screen.getByPlaceholderText("标题"), { target: { value: "紧急维护" } });
    fireEvent.change(screen.getByLabelText("开始时间"), { target: { value: "2026-08-18T02:00" } });
    fireEvent.change(screen.getByLabelText("结束时间"), { target: { value: "2026-08-18T04:00" } });
    fireEvent.click(screen.getByText("保存"));

    await waitFor(() => {
      expect(api.saveMaintenanceWindow).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "紧急维护",
          start_at: new Date("2026-08-18T02:00").toISOString(),
          end_at: new Date("2026-08-18T04:00").toISOString(),
          recurring: false,
        }),
      );
    });
  });

  it("resolves an ongoing incident", async () => {
    renderPage();
    await screen.findByText("DB 故障");
    const resolveBtn = screen.getByTitle("结案");
    fireEvent.click(resolveBtn);
    await waitFor(() => {
      expect(api.resolveIncident).toHaveBeenCalledWith(1);
    });
  });
});
