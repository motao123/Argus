import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Pencil, Play, Plus, Trash2 } from "lucide-react";
import { api, type DDNSProfile, type NATProfile } from "../lib/api";
import { useI18n, type TKey } from "../lib/i18n";

type DDNSForm = {
  id?: number;
  server_id: number;
  name: string;
  provider: "cloudflare" | "webhook";
  record_type: "A" | "AAAA" | "dual";
  access_key: string;
  domains: string;
  webhook_url: string;
  webhook_method: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  webhook_headers: string;
  webhook_body: string;
  enabled: boolean;
};
type NATForm = typeof emptyNAT & { id?: number };

const emptyDDNS: DDNSForm = { server_id: 0, name: "", provider: "webhook", record_type: "A", access_key: "", domains: "", webhook_url: "", webhook_method: "GET", webhook_headers: "{}", webhook_body: "", enabled: true };
const emptyNAT = { server_id: 0, domain: "", target_addr: "", enabled: true };
const ddnsStatusKeys: Record<string, TKey> = {
  pending: "network.statusPending", success: "network.statusSuccess",
  retrying: "network.statusRetrying", stopped: "network.statusStopped",
};

export default function Network() {
  const { t, fmtDateTime } = useI18n();
  const qc = useQueryClient();
  const { data: serverData } = useQuery({ queryKey: ["servers-list"], queryFn: api.servers });
  const { data: ddnsData } = useQuery({ queryKey: ["ddns"], queryFn: api.ddns });
  const { data: natData } = useQuery({ queryKey: ["nats"], queryFn: api.nats });
  const servers = serverData?.servers ?? [];
  const profiles = ddnsData?.profiles ?? [];
  const nats = natData?.nats ?? [];
  const [ddnsForm, setDDNSForm] = useState<DDNSForm | null>(null);
  const [natForm, setNATForm] = useState<NATForm | null>(null);
  const [testResult, setTestResult] = useState("");

  const refresh = () => {
    qc.invalidateQueries({ queryKey: ["ddns"] });
    qc.invalidateQueries({ queryKey: ["nats"] });
  };
  const saveDDNS = useMutation({ mutationFn: api.saveDDNS, onSuccess: () => { setDDNSForm(null); refresh(); } });
  const removeDDNS = useMutation({ mutationFn: api.deleteDDNS, onSuccess: refresh });
  const testDDNS = useMutation({
    mutationFn: api.testDDNS,
    onSuccess: (r) => setTestResult([
      t("network.testAgent", { ipv4: r.ipv4 || t("common.unavailable"), ipv6: r.ipv6 || t("common.unavailable") }),
      ...r.records.map((x) => `${t("network.testRecord", { domain: x.domain, type: x.record_type, status: t(ddnsStatusKeys[x.status] ?? x.status) })}${x.last_ip ? ` → ${x.last_ip}` : ""}${x.last_error ? ` (${x.last_error})` : ""}`),
    ].join("\n")),
  });
  const saveNAT = useMutation({ mutationFn: api.saveNAT, onSuccess: () => { setNATForm(null); refresh(); } });
  const removeNAT = useMutation({ mutationFn: api.deleteNAT, onSuccess: refresh });

  const serverName = (id: number) => servers.find((s) => s.id === id)?.name ?? `#${id}`;

  return (
    <div className="space-y-8">
      <section>
        <div className="mb-4 flex items-center justify-between">
          <div>
            <h1 className="text-xl font-semibold">{t("network.ddnsTitle")}</h1>
            <p className="text-sm text-muted">{t("network.ddnsSubtitle")}</p>
          </div>
          <button onClick={() => setDDNSForm({ ...emptyDDNS, server_id: servers[0]?.id ?? 0 })} className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-white">
            <Plus className="h-4 w-4" /> {t("network.newDDNS")}
          </button>
        </div>

        {ddnsForm && (
          <div className="mb-4 grid grid-cols-1 gap-3 rounded-xl border border-border bg-panel p-4 md:grid-cols-2">
            <select value={ddnsForm.server_id} onChange={(e) => setDDNSForm({ ...ddnsForm, server_id: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm">
              <option value={0}>{t("common.selectServer")}</option>{servers.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
            </select>
            <input aria-label={t("network.ddnsName")} placeholder={t("common.name")} value={ddnsForm.name} onChange={(e) => setDDNSForm({ ...ddnsForm, name: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <select value={ddnsForm.provider} onChange={(e) => setDDNSForm({ ...ddnsForm, provider: e.target.value as "webhook" | "cloudflare" })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm"><option value="webhook">Webhook</option><option value="cloudflare">Cloudflare</option></select>
            <select aria-label={t("network.recordType")} value={ddnsForm.record_type} onChange={(e) => setDDNSForm({ ...ddnsForm, record_type: e.target.value as "A" | "AAAA" | "dual" })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm"><option value="A">A (IPv4)</option><option value="AAAA">AAAA (IPv6)</option><option value="dual">Dual (A + AAAA)</option></select>
            <input aria-label={t("network.domains")} placeholder={t("network.domainsHint")} value={ddnsForm.domains} onChange={(e) => setDDNSForm({ ...ddnsForm, domains: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm md:col-span-2" />
            {ddnsForm.provider === "webhook" ? <>
              <select aria-label="Webhook Method" value={ddnsForm.webhook_method} onChange={(e) => setDDNSForm({ ...ddnsForm, webhook_method: e.target.value as DDNSForm["webhook_method"] })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm"><option>GET</option><option>POST</option><option>PUT</option><option>PATCH</option><option>DELETE</option></select>
              <input aria-label={t("network.webhookUrl")} placeholder={t("network.webhookUrlHint")} value={ddnsForm.webhook_url} onChange={(e) => setDDNSForm({ ...ddnsForm, webhook_url: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
              <input aria-label={t("network.webhookHeaders")} placeholder={t("network.webhookHeadersHint")} value={ddnsForm.webhook_headers} onChange={(e) => setDDNSForm({ ...ddnsForm, webhook_headers: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm md:col-span-2" />
              <textarea aria-label={t("network.webhookBody")} placeholder={t("network.webhookBodyHint")} value={ddnsForm.webhook_body} onChange={(e) => setDDNSForm({ ...ddnsForm, webhook_body: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm md:col-span-2" />
            </> : <input type="password" aria-label={t("network.cfToken")} placeholder={t("network.cfTokenHint")} value={ddnsForm.access_key} onChange={(e) => setDDNSForm({ ...ddnsForm, access_key: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm md:col-span-2" />}
            <div className="flex gap-2 md:col-span-2"><button disabled={!ddnsForm.server_id || !ddnsForm.name || !ddnsForm.domains} onClick={() => saveDDNS.mutate(ddnsForm)} className="rounded-lg bg-accent px-4 py-2 text-sm text-white disabled:opacity-40">{t("common.save")}</button><button onClick={() => setDDNSForm(null)} className="rounded-lg border border-border px-4 py-2 text-sm">{t("common.cancel")}</button></div>
          </div>
        )}

        <div className="overflow-hidden rounded-xl border border-border bg-panel"><table className="w-full text-sm"><thead><tr className="border-b border-border text-left text-xs text-muted"><th className="px-4 py-3">{t("common.name")}</th><th className="px-4 py-3">{t("common.server")}</th><th className="px-4 py-3">{t("network.providerType")}</th><th className="px-4 py-3">{t("network.domainStatus")}</th><th className="px-4 py-3">{t("network.lastIp")}</th><th className="px-4 py-3 text-right">{t("common.actions")}</th></tr></thead><tbody>{profiles.map((p: DDNSProfile) => <tr key={p.id} className="border-b border-border last:border-0"><td className="px-4 py-3 font-medium">{p.name}</td><td className="px-4 py-3">{serverName(p.server_id)}</td><td className="px-4 py-3">{p.provider} · {p.record_type}</td><td className="px-4 py-3"><div>{p.domains}</div><div className="mt-1 space-y-0.5 text-xs text-muted">{(p.records ?? []).map((r) => <div key={`${r.domain}-${r.record_type}`}><span className={r.status === "success" ? "text-ok" : r.status === "stopped" ? "text-err" : "text-muted"}>{r.record_type} {t(ddnsStatusKeys[r.status] ?? r.status)}</span>{r.retry_count ? ` · retry ${r.retry_count}` : ""}{r.next_retry ? ` · ${fmtDateTime(r.next_retry)}` : ""}{r.last_error ? ` · ${r.last_error}` : ""}</div>)}</div></td><td className="px-4 py-3 text-xs text-muted">{p.last_ip || "—"}{p.last_updated ? ` · ${fmtDateTime(p.last_updated)}` : ""}</td><td className="px-4 py-3"><div className="flex justify-end gap-1"><button title={t("network.test")} onClick={() => testDDNS.mutate(p.id)} className="rounded p-1.5 text-accent hover:bg-accent/10"><Play className="h-4 w-4" /></button><button title={t("common.edit")} onClick={() => setDDNSForm({ id: p.id, server_id: p.server_id, name: p.name, provider: p.provider, record_type: p.record_type, access_key: "", domains: p.domains, webhook_url: p.webhook_url === "********" ? "" : p.webhook_url, webhook_method: p.webhook_method || "GET", webhook_headers: p.webhook_headers === "********" ? "********" : (p.webhook_headers || "{}"), webhook_body: p.webhook_body === "********" ? "********" : (p.webhook_body || ""), enabled: p.enabled })} className="rounded p-1.5 text-muted"><Pencil className="h-4 w-4" /></button><button title={t("common.delete")} onClick={() => confirm(t("network.confirmDeleteDDNS", { name: p.name })) && removeDDNS.mutate(p.id)} className="rounded p-1.5 text-err"><Trash2 className="h-4 w-4" /></button></div></td></tr>)}{profiles.length === 0 && <tr><td colSpan={6} className="px-4 py-8 text-center text-muted">{t("network.noDDNS")}</td></tr>}</tbody></table></div>
        {testResult && <pre className="mt-3 whitespace-pre-wrap rounded-lg border border-border bg-bg p-3 text-xs">{testResult}</pre>}
      </section>

      <section>
        <div className="mb-4 flex items-center justify-between"><div><h2 className="text-xl font-semibold">{t("network.natTitle")}</h2><p className="text-sm text-muted">{t("network.natSubtitle")}</p></div><button onClick={() => setNATForm({ ...emptyNAT, server_id: servers[0]?.id ?? 0 })} className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-white"><Plus className="h-4 w-4" /> {t("network.newNAT")}</button></div>
        {natData?.limits && <div className="mb-3 rounded-lg border border-border bg-panel px-3 py-2 text-xs text-muted">{t("network.natLimitServer", { limit: natData.limits.server })} · {t("network.natLimitUser", { limit: natData.limits.user })} · {t("network.natReservedHosts", { hosts: natData.reserved_hosts?.length ? natData.reserved_hosts.join(", ") : t("network.noReservedHosts") })}</div>}
        {natForm && <div className="mb-4 grid grid-cols-1 gap-3 rounded-xl border border-border bg-panel p-4 md:grid-cols-3"><select value={natForm.server_id} onChange={(e) => setNATForm({ ...natForm, server_id: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm"><option value={0}>{t("common.selectServer")}</option>{servers.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}</select><input aria-label={t("network.natDomain")} placeholder={t("network.natDomainHint")} value={natForm.domain} onChange={(e) => setNATForm({ ...natForm, domain: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" /><input aria-label={t("network.targetAddr")} placeholder={t("network.targetAddrHint")} value={natForm.target_addr} onChange={(e) => setNATForm({ ...natForm, target_addr: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" /><div className="flex gap-2 md:col-span-3"><button disabled={!natForm.server_id || !natForm.domain || !natForm.target_addr} onClick={() => saveNAT.mutate(natForm)} className="rounded-lg bg-accent px-4 py-2 text-sm text-white disabled:opacity-40">{t("common.save")}</button><button onClick={() => setNATForm(null)} className="rounded-lg border border-border px-4 py-2 text-sm">{t("common.cancel")}</button></div></div>}
        <div className="overflow-hidden rounded-xl border border-border bg-panel"><table className="w-full text-sm"><thead><tr className="border-b border-border text-left text-xs text-muted"><th className="px-4 py-3">{t("network.domains")}</th><th className="px-4 py-3">{t("common.server")}</th><th className="px-4 py-3">{t("network.targetAddr")}</th><th className="px-4 py-3">{t("common.status")}</th><th className="px-4 py-3">{t("network.tunnels")}</th><th className="px-4 py-3 text-right">{t("common.actions")}</th></tr></thead><tbody>{nats.map((n: NATProfile) => <tr key={n.id} className="border-b border-border last:border-0"><td className="px-4 py-3 font-medium">{n.domain}</td><td className="px-4 py-3">{serverName(n.server_id)}</td><td className="px-4 py-3 font-mono text-xs">{n.target_addr}</td><td className="px-4 py-3"><div className="space-y-0.5"><span className={n.enabled ? "text-ok" : "text-muted"}>{n.enabled ? t("common.enabled") : t("common.disabled")}</span>{n.status && <div className="text-xs text-muted">{n.status === "online" ? t("network.agentOnline") : t("network.agentOffline")}</div>}</div></td><td className="px-4 py-3 text-xs text-muted">{typeof n.active_connections === "number" ? t("network.tunnelCounts", { active: n.active_connections, limit: n.server_connection_limit ?? "?", owner: n.owner_active_connections ?? 0, ownerLimit: n.owner_connection_limit ?? "?" }) : "—"}</td><td className="px-4 py-3"><div className="flex justify-end gap-1"><button title={t("common.edit")} onClick={() => setNATForm({ id: n.id, server_id: n.server_id, domain: n.domain, target_addr: n.target_addr, enabled: n.enabled })} className="rounded p-1.5 text-muted"><Pencil className="h-4 w-4" /></button><button title={t("common.delete")} onClick={() => confirm(t("network.confirmDeleteNAT", { domain: n.domain })) && removeNAT.mutate(n.id)} className="rounded p-1.5 text-err"><Trash2 className="h-4 w-4" /></button></div></td></tr>)}{nats.length === 0 && <tr><td colSpan={6} className="px-4 py-8 text-center text-muted">{t("network.noNAT")}</td></tr>}</tbody></table></div>
      </section>
    </div>
  );
}
