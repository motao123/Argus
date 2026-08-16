import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Pencil, Play, Plus, Trash2 } from "lucide-react";
import { api, type DDNSProfile, type NATProfile } from "../lib/api";
import { fmtDateTime } from "../lib/format";

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

export default function Network() {
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
      `Agent IPv4: ${r.ipv4 || "不可用"} · IPv6: ${r.ipv6 || "不可用"}`,
      ...r.records.map((x) => `${x.domain} ${x.record_type}: ${x.status}${x.last_ip ? ` → ${x.last_ip}` : ""}${x.last_error ? ` (${x.last_error})` : ""}`),
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
            <h1 className="text-xl font-semibold">DDNS</h1>
            <p className="text-sm text-muted">服务器 IP 变化时自动更新域名解析（Cloudflare / Webhook）</p>
          </div>
          <button onClick={() => setDDNSForm({ ...emptyDDNS, server_id: servers[0]?.id ?? 0 })} className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-white">
            <Plus className="h-4 w-4" /> 新建 DDNS
          </button>
        </div>

        {ddnsForm && (
          <div className="mb-4 grid grid-cols-1 gap-3 rounded-xl border border-border bg-panel p-4 md:grid-cols-2">
            <select value={ddnsForm.server_id} onChange={(e) => setDDNSForm({ ...ddnsForm, server_id: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm">
              <option value={0}>选择服务器</option>{servers.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
            </select>
            <input aria-label="DDNS 名称" placeholder="名称" value={ddnsForm.name} onChange={(e) => setDDNSForm({ ...ddnsForm, name: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            <select value={ddnsForm.provider} onChange={(e) => setDDNSForm({ ...ddnsForm, provider: e.target.value as "webhook" | "cloudflare" })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm"><option value="webhook">Webhook</option><option value="cloudflare">Cloudflare</option></select>
            <select aria-label="记录类型" value={ddnsForm.record_type} onChange={(e) => setDDNSForm({ ...ddnsForm, record_type: e.target.value as "A" | "AAAA" | "dual" })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm"><option value="A">A (IPv4)</option><option value="AAAA">AAAA (IPv6)</option><option value="dual">Dual (A + AAAA)</option></select>
            <input aria-label="域名" placeholder="多域名可用逗号、空格或换行分隔；支持 IDN" value={ddnsForm.domains} onChange={(e) => setDDNSForm({ ...ddnsForm, domains: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm md:col-span-2" />
            {ddnsForm.provider === "webhook" ? <>
              <select aria-label="Webhook Method" value={ddnsForm.webhook_method} onChange={(e) => setDDNSForm({ ...ddnsForm, webhook_method: e.target.value as DDNSForm["webhook_method"] })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm"><option>GET</option><option>POST</option><option>PUT</option><option>PATCH</option><option>DELETE</option></select>
              <input aria-label="Webhook URL" placeholder="URL，支持 {domain} {ip} {record_type}（查询参数会转义）" value={ddnsForm.webhook_url} onChange={(e) => setDDNSForm({ ...ddnsForm, webhook_url: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
              <input aria-label="Webhook Headers" placeholder={'Headers JSON，例如 {"Authorization":"Bearer ..."}'} value={ddnsForm.webhook_headers} onChange={(e) => setDDNSForm({ ...ddnsForm, webhook_headers: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm md:col-span-2" />
              <textarea aria-label="Webhook Body" placeholder="Body，可使用占位符" value={ddnsForm.webhook_body} onChange={(e) => setDDNSForm({ ...ddnsForm, webhook_body: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm md:col-span-2" />
            </> : <input type="password" aria-label="Cloudflare API Token" placeholder="Cloudflare API Token（留空保留原值）" value={ddnsForm.access_key} onChange={(e) => setDDNSForm({ ...ddnsForm, access_key: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm md:col-span-2" />}
            <div className="flex gap-2 md:col-span-2"><button disabled={!ddnsForm.server_id || !ddnsForm.name || !ddnsForm.domains} onClick={() => saveDDNS.mutate(ddnsForm)} className="rounded-lg bg-accent px-4 py-2 text-sm text-white disabled:opacity-40">保存</button><button onClick={() => setDDNSForm(null)} className="rounded-lg border border-border px-4 py-2 text-sm">取消</button></div>
          </div>
        )}

        <div className="overflow-hidden rounded-xl border border-border bg-panel"><table className="w-full text-sm"><thead><tr className="border-b border-border text-left text-xs text-muted"><th className="px-4 py-3">名称</th><th className="px-4 py-3">服务器</th><th className="px-4 py-3">Provider / 类型</th><th className="px-4 py-3">域名 / 记录状态</th><th className="px-4 py-3">最近 IP</th><th className="px-4 py-3 text-right">操作</th></tr></thead><tbody>{profiles.map((p: DDNSProfile) => <tr key={p.id} className="border-b border-border last:border-0"><td className="px-4 py-3 font-medium">{p.name}</td><td className="px-4 py-3">{serverName(p.server_id)}</td><td className="px-4 py-3">{p.provider} · {p.record_type}</td><td className="px-4 py-3"><div>{p.domains}</div><div className="mt-1 space-y-0.5 text-xs text-muted">{(p.records ?? []).map((r) => <div key={`${r.domain}-${r.record_type}`}><span className={r.status === "success" ? "text-ok" : r.status === "stopped" ? "text-err" : "text-muted"}>{r.record_type} {r.status}</span>{r.retry_count ? ` · retry ${r.retry_count}` : ""}{r.next_retry ? ` · ${fmtDateTime(r.next_retry)}` : ""}{r.last_error ? ` · ${r.last_error}` : ""}</div>)}</div></td><td className="px-4 py-3 text-xs text-muted">{p.last_ip || "—"}{p.last_updated ? ` · ${fmtDateTime(p.last_updated)}` : ""}</td><td className="px-4 py-3"><div className="flex justify-end gap-1"><button title="测试" onClick={() => testDDNS.mutate(p.id)} className="rounded p-1.5 text-accent hover:bg-accent/10"><Play className="h-4 w-4" /></button><button title="编辑" onClick={() => setDDNSForm({ id: p.id, server_id: p.server_id, name: p.name, provider: p.provider, record_type: p.record_type, access_key: "", domains: p.domains, webhook_url: p.webhook_url === "********" ? "" : p.webhook_url, webhook_method: p.webhook_method || "GET", webhook_headers: p.webhook_headers === "********" ? "********" : (p.webhook_headers || "{}"), webhook_body: p.webhook_body === "********" ? "********" : (p.webhook_body || ""), enabled: p.enabled })} className="rounded p-1.5 text-muted"><Pencil className="h-4 w-4" /></button><button title="删除" onClick={() => confirm(`删除 DDNS「${p.name}」？`) && removeDDNS.mutate(p.id)} className="rounded p-1.5 text-err"><Trash2 className="h-4 w-4" /></button></div></td></tr>)}{profiles.length === 0 && <tr><td colSpan={6} className="px-4 py-8 text-center text-muted">暂无 DDNS 配置</td></tr>}</tbody></table></div>
        {testResult && <pre className="mt-3 whitespace-pre-wrap rounded-lg border border-border bg-bg p-3 text-xs">{testResult}</pre>}
      </section>

      <section>
        <div className="mb-4 flex items-center justify-between"><div><h2 className="text-xl font-semibold">NAT 内网穿透</h2><p className="text-sm text-muted">HTTP 隧道：按 Host 把流量转发到服务器可达的内网地址。仅承载 HTTP/1.x 与 WebSocket Upgrade，不提供通用 TCP/UDP 端口映射</p></div><button onClick={() => setNATForm({ ...emptyNAT, server_id: servers[0]?.id ?? 0 })} className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-white"><Plus className="h-4 w-4" /> 新建 NAT</button></div>
        {natData?.limits && <div className="mb-3 rounded-lg border border-border bg-panel px-3 py-2 text-xs text-muted">每服务器并发隧道上限 <span className="font-medium text-fg">{natData.limits.server}</span> · 每用户上限 <span className="font-medium text-fg">{natData.limits.user}</span> · 保留域名（拒绝代理）: <span className="font-medium text-fg">{natData.reserved_hosts?.length ? natData.reserved_hosts.join(", ") : "未配置"}</span></div>}
        {natForm && <div className="mb-4 grid grid-cols-1 gap-3 rounded-xl border border-border bg-panel p-4 md:grid-cols-3"><select value={natForm.server_id} onChange={(e) => setNATForm({ ...natForm, server_id: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm"><option value={0}>选择服务器</option>{servers.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}</select><input aria-label="NAT 域名" placeholder="nat.example.com（保留域名不可用）" value={natForm.domain} onChange={(e) => setNATForm({ ...natForm, domain: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" /><input aria-label="目标地址" placeholder="127.0.0.1:3000" value={natForm.target_addr} onChange={(e) => setNATForm({ ...natForm, target_addr: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" /><div className="flex gap-2 md:col-span-3"><button disabled={!natForm.server_id || !natForm.domain || !natForm.target_addr} onClick={() => saveNAT.mutate(natForm)} className="rounded-lg bg-accent px-4 py-2 text-sm text-white disabled:opacity-40">保存</button><button onClick={() => setNATForm(null)} className="rounded-lg border border-border px-4 py-2 text-sm">取消</button></div></div>}
        <div className="overflow-hidden rounded-xl border border-border bg-panel"><table className="w-full text-sm"><thead><tr className="border-b border-border text-left text-xs text-muted"><th className="px-4 py-3">域名</th><th className="px-4 py-3">服务器</th><th className="px-4 py-3">目标地址</th><th className="px-4 py-3">状态</th><th className="px-4 py-3">并发隧道</th><th className="px-4 py-3 text-right">操作</th></tr></thead><tbody>{nats.map((n: NATProfile) => <tr key={n.id} className="border-b border-border last:border-0"><td className="px-4 py-3 font-medium">{n.domain}</td><td className="px-4 py-3">{serverName(n.server_id)}</td><td className="px-4 py-3 font-mono text-xs">{n.target_addr}</td><td className="px-4 py-3"><div className="space-y-0.5"><span className={n.enabled ? "text-ok" : "text-muted"}>{n.enabled ? "启用" : "停用"}</span>{n.status && <div className="text-xs text-muted">{n.status === "online" ? "Agent 在线" : "Agent 离线"}</div>}</div></td><td className="px-4 py-3 text-xs text-muted">{typeof n.active_connections === "number" ? `${n.active_connections} / ${n.server_connection_limit ?? "?"}（本服务器）· ${n.owner_active_connections ?? 0} / ${n.owner_connection_limit ?? "?"}（本用户）` : "—"}</td><td className="px-4 py-3"><div className="flex justify-end gap-1"><button title="编辑" onClick={() => setNATForm({ id: n.id, server_id: n.server_id, domain: n.domain, target_addr: n.target_addr, enabled: n.enabled })} className="rounded p-1.5 text-muted"><Pencil className="h-4 w-4" /></button><button title="删除" onClick={() => confirm(`删除 NAT「${n.domain}」？`) && removeNAT.mutate(n.id)} className="rounded p-1.5 text-err"><Trash2 className="h-4 w-4" /></button></div></td></tr>)}{nats.length === 0 && <tr><td colSpan={6} className="px-4 py-8 text-center text-muted">暂无 NAT 配置</td></tr>}</tbody></table></div>
      </section>
    </div>
  );
}
