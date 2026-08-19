import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import Audit from "./Audit";
import { I18nProvider } from "../lib/i18n";

vi.mock("../lib/api", () => ({
  api: {
    auditLogs: vi.fn(),
    auditExport: vi.fn(),
    mcpAuditLogs: vi.fn(),
  },
}));

import { api } from "../lib/api";

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <I18nProvider>
      <QueryClientProvider client={client}>
        <Audit />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.setItem("argus-lang", "zh-CN");
  vi.mocked(api.auditLogs).mockResolvedValue({
    logs: [{
      id: 2,
      user_id: 1,
      username: "admin",
      action: "backup_schedule.restore",
      resource_type: "backup_schedule",
      resource_id: "7",
      outcome: "failure",
      error_code: "backup.key_mismatch",
      duration_ms: 18,
      request_id: "request-123",
      detail: "schedule_id=7 name=nightly",
      ip: "127.0.0.1",
      created_at: "2026-08-18T12:00:00Z",
    }],
    pagination: { total: 1 },
  } as never);
  vi.mocked(api.mcpAuditLogs).mockResolvedValue({
    logs: [{
      id: 1,
      user_id: 1,
      tool: "server_exec",
      server_id: 7,
      args_hash: "hash",
      args_peek: '{"server_id":7}',
      outcome: "success",
      error_msg: "",
      duration_ms: 12,
      ip: "127.0.0.1",
      created_at: "2026-08-18T12:00:00Z",
    }],
    pagination: { total: 1 },
  } as never);
  vi.mocked(api.auditExport).mockResolvedValue({ blob: new Blob(["csv"]), filename: "argus-audit.csv" } as never);
  vi.stubGlobal("URL", {
    ...URL,
    createObjectURL: vi.fn(() => "blob:audit"),
    revokeObjectURL: vi.fn(),
  });
  vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
});

describe("Audit page", () => {
  it("shows structured admin records and applies exact filters", async () => {
    renderPage();
    expect(await screen.findByText("backup_schedule.restore")).toBeInTheDocument();
    expect(screen.getByText("backup.key_mismatch")).toBeInTheDocument();
    expect(screen.getByText("request-123")).toBeInTheDocument();

    fireEvent.change(screen.getByPlaceholderText("按资源类型精确筛选"), { target: { value: "backup_schedule" } });
    fireEvent.change(screen.getByRole("combobox", { name: "管理操作结果筛选" }), { target: { value: "failure" } });

    await waitFor(() => expect(api.auditLogs).toHaveBeenLastCalledWith(0, 50, "backup_schedule", "failure"));
  });

  it("shows MCP audit records and applies exact filters", async () => {
    renderPage();
    await screen.findByText("backup_schedule.restore");
    fireEvent.click(screen.getByRole("button", { name: "MCP 调用" }));

    expect(await screen.findByText("server_exec")).toBeInTheDocument();
    fireEvent.change(screen.getByPlaceholderText("按工具精确筛选"), { target: { value: "server_exec" } });
    fireEvent.change(screen.getByRole("combobox", { name: "MCP 调用结果筛选" }), { target: { value: "success" } });

    await waitFor(() => expect(api.mcpAuditLogs).toHaveBeenLastCalledWith(0, 50, "server_exec", "success"));
  });

  it("downloads the filtered CSV export with the server-provided filename", async () => {
    renderPage();
    await screen.findByText("backup_schedule.restore");
    fireEvent.change(screen.getByPlaceholderText("按资源类型精确筛选"), { target: { value: "backup_schedule" } });
    fireEvent.change(screen.getByRole("combobox", { name: "管理操作结果筛选" }), { target: { value: "failure" } });
    fireEvent.click(screen.getByRole("button", { name: "导出 CSV" }));

    await waitFor(() => expect(api.auditExport).toHaveBeenCalledWith("csv", 30, "backup_schedule", "failure"));
    expect(URL.createObjectURL).toHaveBeenCalled();
    expect(HTMLAnchorElement.prototype.click).toHaveBeenCalled();
    expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:audit");
  });
});
