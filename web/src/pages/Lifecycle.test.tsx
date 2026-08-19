import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import Lifecycle from "./Lifecycle";
import { I18nProvider } from "../lib/i18n";

vi.mock("../lib/api", () => ({
  api: {
    servers: vi.fn(),
    users: vi.fn(),
    transfers: vi.fn(),
    upgradeJobs: vi.fn(),
    createTransfer: vi.fn(),
    cancelTransfer: vi.fn(),
    retryTransfer: vi.fn(),
    createUpgradeJob: vi.fn(),
  },
}));

import { api } from "../lib/api";

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <I18nProvider>
      <QueryClientProvider client={client}>
        <Lifecycle />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

beforeEach(() => {
  localStorage.setItem("argus-lang", "zh-CN");
  vi.stubGlobal("confirm", vi.fn(() => true));
  vi.mocked(api.servers).mockResolvedValue({ servers: [{ id: 7, name: "node-1" }] } as never);
  vi.mocked(api.users).mockResolvedValue({ users: [{ id: 2, username: "bob" }] } as never);
  vi.mocked(api.transfers).mockResolvedValue({
    transfers: [{
      id: 11,
      server_id: 7,
      server_name: "node-1",
      to_username: "bob",
      status: "failed",
      created_at: "2026-08-18T12:00:00Z",
    }],
  } as never);
  vi.mocked(api.upgradeJobs).mockResolvedValue({ jobs: [] } as never);
  vi.mocked(api.retryTransfer).mockResolvedValue({
    transfer: {
      id: 12,
      server_id: 7,
      server_name: "node-1",
      to_username: "bob",
      status: "pending",
      created_at: "2026-08-18T12:10:00Z",
    },
    new_secret: "new-transfer-secret",
    note: "",
  } as never);
});

describe("Lifecycle transfer retry", () => {
  it("retries a failed transfer, displays the one-time secret and refreshes the list", async () => {
    renderPage();

    const retry = await screen.findByRole("button", { name: "重试" });
    fireEvent.click(retry);

    await waitFor(() => expect(api.retryTransfer).toHaveBeenCalledWith(11, expect.anything()));
    expect(confirm).toHaveBeenCalledWith("重试过户 #11？将生成新的临时密钥");
    expect(await screen.findByText(/new-transfer-secret/)).toBeInTheDocument();
    await waitFor(() => expect(api.transfers).toHaveBeenCalledTimes(2));
  });
});
