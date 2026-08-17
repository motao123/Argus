import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import ServerCard from "./ServerCard";
import { I18nProvider } from "../lib/i18n";
import type { Server } from "../lib/api";

const base: Server = {
  id: 1,
  name: "node-1",
  group: "default",
  note: "",
  cpu: 10,
  mem_used: 512,
  mem_total: 1024,
  disk_used: 20,
  disk_total: 100,
  net_in_speed: 1024,
  net_out_speed: 2048,
  load1: 0.5,
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
  uptime: 3600,
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
  traffic_cycle_day: 1,
  traffic_timezone: "UTC",
  traffic_accounting: "sum",
};

function renderCard(server: Server) {
  return render(
    <I18nProvider>
      <MemoryRouter>
        <ServerCard server={server} />
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe("ServerCard latency display", () => {
  it("shows latency in ms when measured", () => {
    renderCard({ ...base, latency_ms: 42 });
    expect(screen.getByText("Latency 42ms")).toBeInTheDocument();
  });

  it("shows em dash when no measurement", () => {
    renderCard({ ...base, latency_ms: 0 });
    expect(screen.getByText("Latency —")).toBeInTheDocument();
  });
});
