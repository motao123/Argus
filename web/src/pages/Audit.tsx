import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Download, ScrollText } from "lucide-react";
import { api, type AuditLog, type MCPAuditLog } from "../lib/api";
import { useI18n } from "../lib/i18n";

const pageSize = 50;

type AuditTab = "admin" | "mcp";

function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  anchor.click();
  URL.revokeObjectURL(url);
}

export default function Audit() {
  const { t, fmtDateTime } = useI18n();
  const [tab, setTab] = useState<AuditTab>("admin");
  const [offset, setOffset] = useState(0);
  const [resourceType, setResourceType] = useState("");
  const [adminOutcome, setAdminOutcome] = useState("");
  const [tool, setTool] = useState("");
  const [mcpOutcome, setMcpOutcome] = useState("");
  const [downloadError, setDownloadError] = useState("");
  const [downloading, setDownloading] = useState(false);

  const adminQuery = useQuery({
    queryKey: ["audit", offset, resourceType, adminOutcome],
    queryFn: () => api.auditLogs(offset, pageSize, resourceType, adminOutcome),
    enabled: tab === "admin",
  });
  const mcpQuery = useQuery({
    queryKey: ["audit-mcp", offset, tool, mcpOutcome],
    queryFn: () => api.mcpAuditLogs(offset, pageSize, tool, mcpOutcome),
    enabled: tab === "mcp",
  });

  const data = tab === "admin" ? adminQuery.data : mcpQuery.data;
  const total = data?.pagination?.total ?? 0;
  const page = Math.floor(offset / pageSize);
  const isFetching = tab === "admin" ? adminQuery.isFetching : mcpQuery.isFetching;

  const switchTab = (next: AuditTab) => {
    setTab(next);
    setOffset(0);
  };

  const exportLogs = async (format: "csv" | "json") => {
    setDownloading(true);
    setDownloadError("");
    try {
      const result = await api.auditExport(format, 30, resourceType, adminOutcome);
      downloadBlob(result.blob, result.filename);
    } catch (error) {
      setDownloadError((error as Error).message);
    } finally {
      setDownloading(false);
    }
  };

  return (
    <div>
      <div className="mb-4 flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="mb-1 flex items-center gap-2 text-xl font-semibold">
            <ScrollText className="h-5 w-5 text-accent" /> {t("audit.title")}
          </h1>
          <p className="text-sm text-muted">{t("audit.subtitle", { total })}</p>
        </div>
        {tab === "admin" && (
          <div className="flex gap-2">
            <button disabled={downloading} onClick={() => exportLogs("csv")} className="flex items-center gap-2 rounded-lg border border-border px-3 py-2 text-sm disabled:opacity-40">
              <Download className="h-4 w-4" /> {t("audit.exportCsv")}
            </button>
            <button disabled={downloading} onClick={() => exportLogs("json")} className="flex items-center gap-2 rounded-lg border border-border px-3 py-2 text-sm disabled:opacity-40">
              <Download className="h-4 w-4" /> {t("audit.exportJson")}
            </button>
          </div>
        )}
      </div>

      <div className="mb-3 flex flex-wrap items-center gap-2">
        <button onClick={() => switchTab("admin")} className={`rounded-lg px-3 py-1.5 text-sm ${tab === "admin" ? "bg-accent text-white" : "border border-border text-muted"}`}>
          {t("audit.tabAdmin")}
        </button>
        <button onClick={() => switchTab("mcp")} className={`rounded-lg px-3 py-1.5 text-sm ${tab === "mcp" ? "bg-accent text-white" : "border border-border text-muted"}`}>
          {t("audit.tabMcp")}
        </button>
        {tab === "admin" ? (
          <>
            <input value={resourceType} onChange={(event) => { setResourceType(event.target.value); setOffset(0); }} placeholder={t("audit.resourceFilter")} className="min-w-0 rounded-lg border border-border bg-bg px-3 py-1.5 text-sm" />
            <select aria-label={t("audit.outcomeFilter")} value={adminOutcome} onChange={(event) => { setAdminOutcome(event.target.value); setOffset(0); }} className="rounded-lg border border-border bg-bg px-3 py-1.5 text-sm">
              <option value="">{t("audit.allOutcomes")}</option>
              <option value="success">success</option>
              <option value="failure">failure</option>
            </select>
          </>
        ) : (
          <>
            <input value={tool} onChange={(event) => { setTool(event.target.value); setOffset(0); }} placeholder={t("audit.toolFilter")} className="min-w-0 rounded-lg border border-border bg-bg px-3 py-1.5 text-sm" />
            <select aria-label={t("audit.mcpOutcomeFilter")} value={mcpOutcome} onChange={(event) => { setMcpOutcome(event.target.value); setOffset(0); }} className="rounded-lg border border-border bg-bg px-3 py-1.5 text-sm">
              <option value="">{t("audit.allOutcomes")}</option>
              {[
                "success", "tool_error", "tool_not_found", "invalid_request", "unauthorized", "scope_denied",
              ].map((value) => <option key={value} value={value}>{value}</option>)}
            </select>
          </>
        )}
      </div>

      {downloadError && <p className="mb-3 text-sm text-err">{t("audit.exportFailed", { error: downloadError })}</p>}

      <div className="overflow-x-auto rounded-xl border border-border bg-panel">
        {tab === "admin" ? (
          <table className="min-w-[1320px] w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-muted">
                <th className="px-4 py-2.5">{t("audit.time")}</th>
                <th className="px-4 py-2.5">{t("common.user")}</th>
                <th className="px-4 py-2.5">{t("audit.action")}</th>
                <th className="px-4 py-2.5">{t("audit.resource")}</th>
                <th className="px-4 py-2.5">{t("audit.outcome")}</th>
                <th className="px-4 py-2.5">{t("audit.duration")}</th>
                <th className="px-4 py-2.5">{t("audit.requestId")}</th>
                <th className="px-4 py-2.5">{t("audit.detail")}</th>
                <th className="px-4 py-2.5">{t("common.ip")}</th>
              </tr>
            </thead>
            <tbody>
              {(adminQuery.data?.logs ?? []).map((log: AuditLog) => (
                <tr key={log.id} className="border-b border-border last:border-0">
                  <td className="whitespace-nowrap px-4 py-2.5 text-xs text-muted">{fmtDateTime(log.created_at)}</td>
                  <td className="px-4 py-2.5">{log.username}</td>
                  <td className="whitespace-nowrap px-4 py-2.5 font-mono text-xs text-accent">{log.action}</td>
                  <td className="whitespace-nowrap px-4 py-2.5 font-mono text-xs text-muted">{log.resource_type || "—"}{log.resource_id ? ` #${log.resource_id}` : ""}</td>
                  <td className="whitespace-nowrap px-4 py-2.5 font-mono text-xs">
                    <span className={log.outcome === "failure" ? "text-err" : "text-ok"}>{log.outcome || "success"}</span>
                    {log.error_code && <span className="ml-2 text-err" title={log.error_code}>{log.error_code}</span>}
                  </td>
                  <td className="whitespace-nowrap px-4 py-2.5 tabular text-muted">{log.duration_ms}ms</td>
                  <td className="max-w-44 truncate px-4 py-2.5 font-mono text-xs text-muted" title={log.request_id}>{log.request_id || "—"}</td>
                  <td className="max-w-md truncate px-4 py-2.5 text-muted" title={log.detail}>{log.detail || "—"}</td>
                  <td className="whitespace-nowrap px-4 py-2.5 font-mono text-xs text-muted">{log.ip}</td>
                </tr>
              ))}
              {(adminQuery.data?.logs ?? []).length === 0 && <tr><td colSpan={9} className="px-4 py-8 text-center text-muted">{t("audit.none")}</td></tr>}
            </tbody>
          </table>
        ) : (
          <table className="min-w-[960px] w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-muted">
                <th className="px-4 py-2.5">{t("audit.time")}</th>
                <th className="px-4 py-2.5">{t("audit.tool")}</th>
                <th className="px-4 py-2.5">{t("audit.outcome")}</th>
                <th className="px-4 py-2.5">{t("audit.serverId")}</th>
                <th className="px-4 py-2.5">{t("audit.duration")}</th>
                <th className="px-4 py-2.5">{t("audit.args")}</th>
                <th className="px-4 py-2.5">{t("audit.error")}</th>
                <th className="px-4 py-2.5">{t("common.ip")}</th>
              </tr>
            </thead>
            <tbody>
              {(mcpQuery.data?.logs ?? []).map((log: MCPAuditLog) => (
                <tr key={log.id} className="border-b border-border last:border-0">
                  <td className="whitespace-nowrap px-4 py-2.5 text-xs text-muted">{fmtDateTime(log.created_at)}</td>
                  <td className="whitespace-nowrap px-4 py-2.5 font-mono text-xs text-accent">{log.tool}</td>
                  <td className="whitespace-nowrap px-4 py-2.5 font-mono text-xs">{log.outcome}</td>
                  <td className="px-4 py-2.5 tabular text-muted">{log.server_id || "—"}</td>
                  <td className="px-4 py-2.5 tabular text-muted">{log.duration_ms}ms</td>
                  <td className="max-w-xs truncate px-4 py-2.5 font-mono text-xs text-muted" title={log.args_peek}>{log.args_peek || "—"}</td>
                  <td className="max-w-xs truncate px-4 py-2.5 text-err" title={log.error_msg}>{log.error_msg || "—"}</td>
                  <td className="whitespace-nowrap px-4 py-2.5 font-mono text-xs text-muted">{log.ip}</td>
                </tr>
              ))}
              {(mcpQuery.data?.logs ?? []).length === 0 && <tr><td colSpan={8} className="px-4 py-8 text-center text-muted">{t("audit.noneMcp")}</td></tr>}
            </tbody>
          </table>
        )}
      </div>

      <div className="mt-3 flex items-center gap-3 text-sm">
        <button disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - pageSize))} className="rounded-lg border border-border px-3 py-1.5 disabled:opacity-40">
          {t("audit.prev")}
        </button>
        <span className="text-muted">{t("audit.page", { page: page + 1 })}</span>
        <button disabled={offset + pageSize >= total} onClick={() => setOffset(offset + pageSize)} className="rounded-lg border border-border px-3 py-1.5 disabled:opacity-40">
          {t("audit.next")}
        </button>
        {isFetching && <span className="text-xs text-muted">{t("audit.loading")}</span>}
      </div>
    </div>
  );
}
