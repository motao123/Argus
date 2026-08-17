// 月度 SLA/SLO 卡片（服务器详情页）：近 N 个月可用性（自动排除维护窗口）。
import { useQuery } from "@tanstack/react-query";
import { Gauge } from "lucide-react";
import { api } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { fmtAvailability, fmtMinutes, monthLabel } from "../lib/status";

export function SlaCard({ serverId }: { serverId: number }) {
  const { t } = useI18n();
  const { data } = useQuery({
    queryKey: ["sla", serverId],
    queryFn: () => api.serverSla(serverId, 6),
    refetchInterval: 300000,
  });
  const months = data?.months ?? [];
  if (months.length === 0) return null;

  return (
    <div className="mb-4 rounded-xl border border-border bg-panel p-4">
      <div className="mb-2 flex items-center gap-2 text-sm font-medium">
        <Gauge className="h-4 w-4 text-accent" />
        {t("sla.title")}
        {data && data.slo_target > 0 && (
          <span className="text-xs font-normal text-muted">{t("sla.target", { target: data.slo_target.toFixed(1) })}</span>
        )}
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="text-left text-xs text-muted">
              <th className="py-1.5 pr-3 font-normal">{t("sla.month")}</th>
              <th className="py-1.5 pr-3 font-normal">{t("sla.availability")}</th>
              <th className="py-1.5 pr-3 font-normal">{t("sla.uptime")}</th>
              <th className="py-1.5 pr-3 font-normal">{t("sla.maintenance")}</th>
              <th className="py-1.5 font-normal">{t("sla.slo")}</th>
            </tr>
          </thead>
          <tbody>
            {months.map((m) => {
              const disabled = data && data.slo_target <= 0;
              const met = disabled || m.slo_met === null ? null : m.slo_met;
              return (
                <tr key={m.month} className="border-t border-border">
                  <td className="py-2 pr-3 tabular">{monthLabel(m.month)}</td>
                  <td className={`py-2 pr-3 tabular font-medium ${m.availability === null ? "text-muted" : m.availability >= (m.slo_target > 0 ? m.slo_target : 99.9) ? "text-ok" : m.availability >= 95 ? "text-warn" : "text-err"}`}>
                    {fmtAvailability(m.availability)}
                  </td>
                  <td className="py-2 pr-3 tabular text-muted">{fmtMinutes(m.uptime_minutes)}</td>
                  <td className="py-2 pr-3 tabular text-muted">{fmtMinutes(m.maintenance_minutes)}</td>
                  <td className="py-2 tabular">
                    {m.availability === null ? (
                      <span className="text-muted">—</span>
                    ) : met === null ? (
                      <span className="text-muted">{t("sla.disabled")}</span>
                    ) : met ? (
                      <span className="text-ok">{t("sla.met")}</span>
                    ) : (
                      <span className="text-err">{t("sla.notMet")}</span>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <p className="mt-2 text-xs text-muted">{t("sla.subtitle")}</p>
    </div>
  );
}
