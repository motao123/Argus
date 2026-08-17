// 进度条与服务器卡片
import { Link } from "react-router-dom";
import { Cpu, HardDrive, MemoryStick, Network } from "lucide-react";
import { countryFlag, type Server } from "../lib/api";
import { fmtPercent, fmtSpeed } from "../lib/format";
import { useI18n } from "../lib/i18n";

export function Bar({ value, color = "bg-accent" }: { value: number; color?: string }) {
  const v = Math.max(0, Math.min(100, value));
  return (
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-black/10 dark:bg-white/10">
      <div className={`h-full rounded-full ${color} transition-all duration-700`} style={{ width: `${v}%` }} />
    </div>
  );
}

function Row({ icon, label, value, pct, color }: { icon: React.ReactNode; label: string; value: string; pct?: number; color?: string }) {
  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between text-xs">
        <span className="flex items-center gap-1.5 text-muted">
          {icon}
          {label}
        </span>
        <span className="tabular">{value}</span>
      </div>
      {pct !== undefined && <Bar value={pct} color={color} />}
    </div>
  );
}

export default function ServerCard({ server }: { server: Server }) {
  const { t, fmtDuration } = useI18n();
  const memPct = server.mem_total ? (server.mem_used / server.mem_total) * 100 : 0;
  const diskPct = server.disk_total ? (server.disk_used / server.disk_total) * 100 : 0;

  return (
    <Link
      to={`/server/${server.id}`}
      className="block rounded-xl border border-border bg-panel p-4 transition-shadow hover:shadow-lg hover:shadow-black/5"
    >
      <div className="mb-3 flex items-start justify-between">
        <div className="min-w-0">
          <div className="truncate font-medium">{server.name}</div>
          <div className="text-xs text-muted">
            {server.host?.platform ?? t("serverCard.unreported")}
            {server.host?.cpu_cores ? ` · ${t("serverCard.cores", { count: server.host.cpu_cores })}` : ""}
          </div>
          <div className="mt-0.5 text-xs text-muted">
            {server.host?.country_code ? <span title={server.host.ip}>{countryFlag(server.host.country_code)} {server.host.country_code}</span> : null}
          </div>
        </div>
        <span
          className={`mt-0.5 flex h-2.5 w-2.5 shrink-0 rounded-full ${
            server.online ? "bg-ok shadow-[0_0_6px] shadow-ok" : "bg-err"
          }`}
          title={server.online ? t("common.online") : t("common.offline")}
        />
      </div>

      <div className="space-y-2.5">
        <Row icon={<Cpu className="h-3.5 w-3.5" />} label="CPU" value={`${server.cpu.toFixed(1)}%`} pct={server.cpu} />
        <Row
          icon={<MemoryStick className="h-3.5 w-3.5" />}
          label={t("serverCard.mem")}
          value={fmtPercent(server.mem_used, server.mem_total)}
          pct={memPct}
          color={memPct > 85 ? "bg-err" : memPct > 70 ? "bg-warn" : "bg-accent"}
        />
        <Row
          icon={<HardDrive className="h-3.5 w-3.5" />}
          label={t("serverCard.disk")}
          value={fmtPercent(server.disk_used, server.disk_total)}
          pct={diskPct}
          color={diskPct > 85 ? "bg-err" : diskPct > 70 ? "bg-warn" : "bg-accent"}
        />
        <Row icon={<Network className="h-3.5 w-3.5" />} label={t("serverCard.net")} value={`↓ ${fmtSpeed(server.net_in_speed)}  ↑ ${fmtSpeed(server.net_out_speed)}`} />
      </div>

      <div className="mt-3 flex items-center justify-between border-t border-border pt-2 text-xs text-muted">
        <span>{t("serverCard.load", { load: server.load1.toFixed(2) })}</span>
        <span>{t("serverCard.latency", { ms: server.latency_ms > 0 ? `${server.latency_ms}ms` : "—" })}</span>
        <span>{fmtDuration(server.uptime)}</span>
      </div>
    </Link>
  );
}
