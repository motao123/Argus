import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ScrollText } from "lucide-react";
import { api, type AuditLog } from "../lib/api";
import { useI18n } from "../lib/i18n";

export default function Audit() {
  const { t, fmtDateTime } = useI18n();
  const [offset, setOffset] = useState(0);
  const { data, isFetching } = useQuery({
    queryKey: ["audit", offset],
    queryFn: () => api.auditLogs(offset, 50),
  });
  const logs = data?.logs ?? [];
  const total = data?.pagination?.total ?? 0;
  const page = Math.floor(offset / 50);

  return (
    <div>
      <h1 className="mb-1 flex items-center gap-2 text-xl font-semibold">
        <ScrollText className="h-5 w-5 text-accent" /> {t("audit.title")}
      </h1>
      <p className="mb-4 text-sm text-muted">{t("audit.subtitle", { total })}</p>
      <div className="overflow-x-auto rounded-xl border border-border bg-panel">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted">
              <th className="px-4 py-2.5">{t("audit.time")}</th>
              <th className="px-4 py-2.5">{t("common.user")}</th>
              <th className="px-4 py-2.5">{t("audit.action")}</th>
              <th className="px-4 py-2.5">{t("audit.detail")}</th>
              <th className="px-4 py-2.5">{t("common.ip")}</th>
            </tr>
          </thead>
          <tbody>
            {logs.map((l: AuditLog) => (
              <tr key={l.id} className="border-b border-border last:border-0">
                <td className="whitespace-nowrap px-4 py-2.5 text-xs text-muted">{fmtDateTime(l.created_at)}</td>
                <td className="px-4 py-2.5">{l.username}</td>
                <td className="whitespace-nowrap px-4 py-2.5 font-mono text-xs text-accent">{l.action}</td>
                <td className="max-w-md truncate px-4 py-2.5 text-muted" title={l.detail}>{l.detail || "—"}</td>
                <td className="whitespace-nowrap px-4 py-2.5 font-mono text-xs text-muted">{l.ip}</td>
              </tr>
            ))}
            {logs.length === 0 && <tr><td colSpan={5} className="px-4 py-8 text-center text-muted">{t("audit.none")}</td></tr>}
          </tbody>
        </table>
      </div>
      <div className="mt-3 flex items-center gap-3 text-sm">
        <button
          disabled={offset === 0}
          onClick={() => setOffset(Math.max(0, offset - 50))}
          className="rounded-lg border border-border px-3 py-1.5 disabled:opacity-40"
        >
          {t("audit.prev")}
        </button>
        <span className="text-muted">{t("audit.page", { page: page + 1 })}</span>
        <button
          disabled={offset + 50 >= total}
          onClick={() => setOffset(offset + 50)}
          className="rounded-lg border border-border px-3 py-1.5 disabled:opacity-40"
        >
          {t("audit.next")}
        </button>
        {isFetching && <span className="text-xs text-muted">{t("audit.loading")}</span>}
      </div>
    </div>
  );
}
