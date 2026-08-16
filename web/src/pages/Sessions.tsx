import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { LogOut, MonitorSmartphone, ShieldOff } from "lucide-react";
import { api, type Session } from "../lib/api";
import { useI18n } from "../lib/i18n";

export default function Sessions() {
  const { t, fmtDateTime } = useI18n();
  const qc = useQueryClient();
  const { data } = useQuery({ queryKey: ["sessions"], queryFn: api.sessions, refetchInterval: 10000 });
  const sessions = data?.sessions ?? [];

  const kick = useMutation({
    mutationFn: api.kickSession,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["sessions"] }),
  });
  const kickAll = useMutation({
    mutationFn: () => api.kickAllSessions(),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["sessions"] }),
  });

  return (
    <div>
      <h1 className="mb-1 text-xl font-semibold">{t("sessions.title")}</h1>
      <p className="mb-5 text-sm text-muted">{t("sessions.subtitle")}</p>

      <div className="mb-3 flex justify-end">
        <button
          onClick={() => sessions.length > 1 && confirm(t("sessions.confirmKickAll")) && kickAll.mutate()}
          disabled={sessions.length <= 1 || kickAll.isPending}
          className="flex items-center gap-1.5 rounded border border-err/30 px-2.5 py-1.5 text-sm text-err hover:bg-err/10 disabled:cursor-not-allowed disabled:opacity-40"
          title={t("sessions.kickAllTitle")}
        >
          <ShieldOff className="h-4 w-4" />
          {t("sessions.kickAll")}
        </button>
      </div>

      <div className="overflow-hidden rounded-xl border border-border bg-panel">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted">
              <th className="px-4 py-2.5 font-normal">{t("common.id")}</th>
              <th className="px-4 py-2.5 font-normal">{t("common.user")}</th>
              <th className="px-4 py-2.5 font-normal">{t("common.ip")}</th>
              <th className="px-4 py-2.5 font-normal">{t("sessions.device")}</th>
              <th className="px-4 py-2.5 font-normal">{t("sessions.loginAt")}</th>
              <th className="px-4 py-2.5 font-normal">{t("sessions.expiresAt")}</th>
              <th className="px-4 py-2.5 text-right font-normal">{t("common.actions")}</th>
            </tr>
          </thead>
          <tbody>
            {sessions.map((s: Session) => (
              <tr key={s.id} className="border-b border-border last:border-0">
                <td className="px-4 py-2.5 tabular text-muted">#{s.id}</td>
                <td className="px-4 py-2.5 font-medium">#{s.user_id}</td>
                <td className="px-4 py-2.5 tabular text-xs">{s.ip}</td>
                <td className="max-w-[220px] truncate px-4 py-2.5 text-xs text-muted" title={s.user_agent}>
                  {s.user_agent || "—"}
                </td>
                <td className="px-4 py-2.5 text-xs text-muted">{fmtDateTime(s.created_at)}</td>
                <td className="px-4 py-2.5 text-xs text-muted">{fmtDateTime(s.expires_at)}</td>
                <td className="px-4 py-2.5 text-right">
                  <button
                    onClick={() => confirm(t("sessions.confirmKick", { id: s.id })) && kick.mutate(s.id)}
                    className="flex items-center gap-1 rounded p-1.5 text-err hover:bg-err/10"
                    title={t("sessions.kickTitle")}
                  >
                    <LogOut className="h-4 w-4" />
                  </button>
                </td>
              </tr>
            ))}
            {sessions.length === 0 && (
              <tr>
                <td colSpan={7} className="px-4 py-8 text-center text-muted">
                  <MonitorSmartphone className="mx-auto mb-2 h-6 w-6 opacity-40" />
                  {t("sessions.none")}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
