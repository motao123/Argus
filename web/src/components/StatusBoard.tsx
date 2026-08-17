// 公开状态板（里程碑9）：当前维护横幅 + 事故时间线。
// 数据来自公开接口，游客与登录用户均可查看。
import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { CalendarClock, TriangleAlert } from "lucide-react";
import { api, type Incident, type MaintenanceWindow } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { isWindowActive, severityRank, severityTone, windowState } from "../lib/status";

export function StatusBoard() {
  const { t, fmtDateTime } = useI18n();
  const { data: incData } = useQuery({ queryKey: ["incidents"], queryFn: api.incidents, refetchInterval: 60000 });
  const { data: mwData } = useQuery({ queryKey: ["maintenance-windows"], queryFn: api.maintenanceWindows, refetchInterval: 60000 });
  const incidents = incData?.incidents ?? [];
  const windows = mwData?.windows ?? [];

  // 当前生效的维护窗口
  const activeWindows = useMemo(() => windows.filter((w) => isWindowActive(w)), [windows]);

  // 时间线：进行中（按严重级别）在前，其后 14 天内已解决的事故
  const timeline = useMemo(() => {
    const cutoff = Date.now() - 14 * 24 * 3600 * 1000;
    const visible = incidents.filter((i) => i.status === "ongoing" || new Date(i.end_at ?? 0).getTime() >= cutoff);
    return [...visible].sort((a, b) => {
      if (a.status !== b.status) return a.status === "ongoing" ? -1 : 1;
      if (a.status === "ongoing") return severityRank(a.severity) - severityRank(b.severity);
      return new Date(b.start_at).getTime() - new Date(a.start_at).getTime();
    });
  }, [incidents]);

  if (activeWindows.length === 0 && timeline.length === 0) return null;

  const severityLabel: Record<Incident["severity"], string> = {
    minor: t("incidents.severityMinor"),
    major: t("incidents.severityMajor"),
    critical: t("incidents.severityCritical"),
  };

  return (
    <div className="mb-4 space-y-3">
      {/* 当前维护横幅 */}
      {activeWindows.length > 0 && (
        <div className="rounded-xl border border-warn/40 bg-warn/10 p-3">
          <div className="flex items-center gap-2 text-sm font-medium text-warn">
            <CalendarClock className="h-4 w-4" />
            {t("mw.activeBanner")}
          </div>
          <ul className="mt-1.5 space-y-1">
            {activeWindows.map((w: MaintenanceWindow) => (
              <li key={w.id} className="flex flex-wrap items-center gap-x-2 text-xs text-muted">
                <span className="font-medium text-fg">{w.title}</span>
                <span>{fmtDateTime(w.start_at)} — {fmtDateTime(w.end_at)}</span>
                {w.recurring && <span className="rounded-full bg-accent/10 px-2 py-0.5 text-accent">{t("mw.recurring")}</span>}
                {w.server_ids === "" && <span>{t("mw.allServers")}</span>}
              </li>
            ))}
          </ul>
        </div>
      )}

      {/* 事故时间线 */}
      {timeline.length > 0 && (
        <div className="rounded-xl border border-border bg-panel p-3">
          <div className="mb-2 flex items-center gap-2 text-sm font-medium">
            <TriangleAlert className="h-4 w-4 text-accent" />
            {t("incidents.publicTitle")}
          </div>
          <ul className="space-y-2">
            {timeline.map((inc) => {
              const tone = severityTone(inc.severity);
              return (
                <li key={inc.id} className="flex items-start gap-2.5">
                  <span className={`mt-1.5 h-2 w-2 shrink-0 rounded-full ${tone === "err" ? "bg-err" : tone === "warn" ? "bg-warn" : "bg-ok"}`} />
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="text-sm font-medium">{inc.title}</span>
                      <span className={`rounded-full px-2 py-0.5 text-[11px] ${tone === "err" ? "bg-err/10 text-err" : tone === "warn" ? "bg-warn/10 text-warn" : "bg-ok/10 text-ok"}`}>
                        {severityLabel[inc.severity]}
                      </span>
                      {inc.status === "ongoing" && (
                        <span className="rounded-full bg-err/10 px-2 py-0.5 text-[11px] text-err">{t("incidents.ongoing")}</span>
                      )}
                    </div>
                    <div className="text-xs text-muted">
                      {fmtDateTime(inc.start_at)} — {inc.end_at ? fmtDateTime(inc.end_at) : t("incidents.ongoing")}
                    </div>
                    {inc.notes && <p className="mt-0.5 break-all text-xs text-muted">{inc.notes}</p>}
                  </div>
                </li>
              );
            })}
          </ul>
        </div>
      )}
    </div>
  );
}

/** 维护窗口状态徽标（公开页/详情页复用）。 */
export function WindowStateBadge({ w }: { w: MaintenanceWindow }) {
  const { t } = useI18n();
  const st = windowState(w);
  return (
    <span
      className={`rounded-full px-2 py-0.5 text-[11px] ${
        st === "active" ? "bg-warn/10 text-warn" : st === "upcoming" ? "bg-accent/10 text-accent" : "bg-black/5 text-muted dark:bg-white/10"
      }`}
    >
      {st === "active" ? t("mw.active") : st === "upcoming" ? t("mw.upcoming") : t("mw.ended")}
    </span>
  );
}
