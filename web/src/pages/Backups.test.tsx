import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import Backups from "./Backups";
import { I18nProvider } from "../lib/i18n";

vi.mock("../lib/api", () => ({
  api: {
    backupSchedules: vi.fn(),
    backupRuns: vi.fn(),
    createBackupSchedule: vi.fn(),
    updateBackupSchedule: vi.fn(),
    deleteBackupSchedule: vi.fn(),
    runBackupSchedule: vi.fn(),
    backupDrill: vi.fn(),
    restoreEncryptedBackup: vi.fn(),
  },
}));

import { api } from "../lib/api";

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <I18nProvider>
      <QueryClientProvider client={client}>
        <Backups />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.setItem("argus-lang", "zh-CN");
  vi.stubGlobal("confirm", vi.fn(() => true));
  vi.mocked(api.backupSchedules).mockResolvedValue({
    schedules: [{
      id: 7,
      name: "nightly",
      enabled: true,
      cron: "0 3 * * *",
      target: "D:/backups",
      keep_count: 7,
      key_source: "env:ARGUS_BACKUP_KEY",
      key_id: "0123456789abcdef",
      last_run_at: null,
      last_status: "success",
      last_error: "",
      last_size: 1024,
      created_at: "2026-08-18T12:00:00Z",
      updated_at: "2026-08-18T12:00:00Z",
    }],
  } as never);
  vi.mocked(api.backupRuns).mockResolvedValue({ runs: [] } as never);
  vi.mocked(api.restoreEncryptedBackup).mockResolvedValue({
    ok: true,
    key_id: "0123456789abcdef",
    rollback_path: "D:/data/argus.db.pre-restore.20260818",
    status: "restart_required",
    restart_required: true,
    note: "restart",
  } as never);
});

describe("Encrypted backup restore", () => {
  it("requires confirmation, restores against the selected schedule and shows restart state", async () => {
    const { container } = renderPage();
    await screen.findByText("nightly");
    fireEvent.click(screen.getByTitle("立即备份").nextElementSibling as HTMLElement);

    const restoreButton = await screen.findByRole("button", { name: "恢复此加密备份" });
    fireEvent.click(restoreButton);
    const inputs = Array.from(container.querySelectorAll<HTMLInputElement>('input[type="file"][accept=".argusenc"]'));
    const restoreInput = inputs[inputs.length - 1];
    expect(restoreInput).toBeTruthy();
    const file = new File(["encrypted"], "nightly.argusenc", { type: "application/octet-stream" });
    fireEvent.change(restoreInput!, { target: { files: [file] } });

    expect(confirm).toHaveBeenCalledWith("确认用所选加密备份恢复计划「nightly」？当前数据库会被替换，成功后必须立即重启服务。");
    await waitFor(() => expect(api.restoreEncryptedBackup).toHaveBeenCalledWith(7, file));
    expect(await screen.findByText(/必须立即重启服务/)).toBeInTheDocument();
    expect(screen.getByText(/argus\.db\.pre-restore/)).toBeInTheDocument();
  });

  it("does not call restore when the operator cancels", async () => {
    vi.mocked(confirm).mockReturnValueOnce(false);
    const { container } = renderPage();
    await screen.findByText("nightly");
    fireEvent.click(screen.getByTitle("立即备份").nextElementSibling as HTMLElement);
    fireEvent.click(await screen.findByRole("button", { name: "恢复此加密备份" }));
    const inputs = Array.from(container.querySelectorAll<HTMLInputElement>('input[type="file"][accept=".argusenc"]'));
    fireEvent.change(inputs[inputs.length - 1], { target: { files: [new File(["x"], "x.argusenc")] } });

    await waitFor(() => expect(confirm).toHaveBeenCalled());
    expect(api.restoreEncryptedBackup).not.toHaveBeenCalled();
  });
});
