import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import Alerts from "./Alerts";
import { I18nProvider } from "../lib/i18n";

vi.mock("../lib/api", () => ({
  api: {
    alerts: vi.fn(),
    notifications: vi.fn(),
    crons: vi.fn(),
    notificationGroups: vi.fn(),
    saveAlert: vi.fn(),
    deleteAlert: vi.fn(),
    ackAlert: vi.fn(),
    unackAlert: vi.fn(),
    silenceAlert: vi.fn(),
    unsilenceAlert: vi.fn(),
    saveNotification: vi.fn(),
    deleteNotification: vi.fn(),
    testMessage: vi.fn(),
  },
}));

import { api } from "../lib/api";

const mockAlert = {
  id: 1,
  name: "CPU 过高",
  metric: "cpu",
  min: null as number | null,
  max: 90,
  duration: 30,
  notify: true,
  webhook_id: 1,
  group_id: 0,
  trigger_cron_id: 0,
  trigger_ratio: null as number | null,
  template: "",
  enabled: true,
  acked_at: null as string | null,
  acked_by: "",
  silence_from: null as string | null,
  silence_to: null as string | null,
};

const mockNotification = {
  id: 1,
  name: "hook",
  type: "webhook",
  url: "https://example.com/hook",
  method: "POST",
  headers: "{}",
  body: '{"title":{{title}},"content":{{content}}}',
  chat_id: "",
  extra: "",
};

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <I18nProvider>
      <QueryClientProvider client={qc}>
        <Alerts />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

beforeEach(() => {
  localStorage.setItem("argus-lang", "zh-CN");
  vi.mocked(api.alerts).mockResolvedValue({ alerts: [mockAlert] });
  vi.mocked(api.notifications).mockResolvedValue({ notifications: [mockNotification] });
  vi.mocked(api.crons).mockResolvedValue({ crons: [] });
  vi.mocked(api.notificationGroups).mockResolvedValue({ groups: [] });
  vi.mocked(api.saveAlert).mockResolvedValue({} as never);
  vi.mocked(api.saveNotification).mockResolvedValue({} as never);
});

describe("Alerts page — custom notification template", () => {
  it("renders alert rules with template column data intact", async () => {
    renderPage();
    expect(await screen.findByText("CPU 过高")).toBeInTheDocument();
  });

  it("new-rule form shows the custom template textarea and saves its value", async () => {
    renderPage();
    await screen.findByText("CPU 过高");
    fireEvent.click(screen.getByText("新建规则"));

    const textarea = screen.getByPlaceholderText(/首行为标题/);
    fireEvent.change(textarea, {
      target: { value: "{{server.name}} {{event}}\n{{rule}}: {{metric}} = {{value}} 阈值 {{threshold}}" },
    });
    fireEvent.click(screen.getByText("保存"));

    await waitFor(() => {
      expect(api.saveAlert).toHaveBeenCalledWith(
        expect.objectContaining({
          name: "",
          template: "{{server.name}} {{event}}\n{{rule}}: {{metric}} = {{value}} 阈值 {{threshold}}",
        }),
      );
    });
  });

  it("edit form pre-fills an existing rule's template", async () => {
    vi.mocked(api.alerts).mockResolvedValue({
      alerts: [{ ...mockAlert, template: "{{server.name}}|{{event}}" }],
    });
    renderPage();
    await screen.findByText("CPU 过高");
    const pencil = document.querySelector(".lucide-pencil")!;
    fireEvent.click(pencil.closest("button")!);

    const textarea = screen.getByPlaceholderText(/首行为标题/) as HTMLTextAreaElement;
    expect(textarea.value).toBe("{{server.name}}|{{event}}");
    // 编辑时清空模板 → 提交空串（回退默认格式）
    fireEvent.change(textarea, { target: { value: "" } });
    fireEvent.click(screen.getByText("保存"));
    await waitFor(() => {
      expect(api.saveAlert).toHaveBeenCalledWith(expect.objectContaining({ id: 1, template: "" }));
    });
  });

  it("channel form default body uses unquoted placeholders", async () => {
    renderPage();
    await screen.findByText("CPU 过高");
    fireEvent.click(screen.getByText("添加"));
    const body = screen.getByPlaceholderText(/Body 模板/) as HTMLTextAreaElement;
    expect(body.value).toBe('{"title":{{title}},"content":{{content}}}');
  });
});
