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
  access_key: string;
  domains: string;
  webhook_url: string;
  enabled: boolean;
};
type NATForm = typeof emptyNAT & { id?: number };

const emptyDDNS: DDNSForm = { server_id: 0, name: "", provider: "webhook", access_key: "", domains: "", webhook_url: "", enabled: true };
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
    onSuccess: (r) => setTestResult([`当前 IP: ${r.ip}`, ...Object.entries(r.results).map(([k, v]) => `${k}: ${v}`)].join("\n")),
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
            <input aria-label="域名" placeholder="域名，多个用逗号分隔" value={ddnsForm.domains} onChange={(e) => setDDNSForm({ ...ddnsForm, domains: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" />
            {ddnsForm.provider === "webhook" ? <input aria-label="Webhook URL" placeholder="Webhook URL（支持 {ip}）" value={ddnsForm.webhook_url} onChange={(e) => setDDNSForm({ ...ddnsForm, webhook_url: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm md:col-span-2" /> : <input type="password" aria-label="Cloudflare API Token" placeholder="Cloudflare API Token（留空保留原值）" value={ddnsForm.access_key} onChange={(e) => setDDNSForm({ ...ddnsForm, access_key: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm md:col-span-2" />}
            <div className="flex gap-2 md:col-span-2"><button disabled={!ddnsForm.server_id || !ddnsForm.name || !ddnsForm.domains} onClick={() => saveDDNS.mutate(ddnsForm)} className="rounded-lg bg-accent px-4 py-2 text-sm text-white disabled:opacity-40">保存</button><button onClick={() => setDDNSForm(null)} className="rounded-lg border border-border px-4 py-2 text-sm">取消</button></div>
          </div>
        )}

        <div className="overflow-hidden rounded-xl border border-border bg-panel"><table className="w-full text-sm"><thead><tr className="border-b border-border text-left text-xs text-muted"><th className="px-4 py-3">名称</th><th className="px-4 py-3">服务器</th><th className="px-4 py-3">Provider</th><th className="px-4 py-3">域名</th><th className="px-4 py-3">最近 IP</th><th className="px-4 py-3 text-right">操作</th></tr></thead><tbody>{profiles.map((p: DDNSProfile) => <tr key={p.id} className="border-b border-border last:border-0"><td className="px-4 py-3 font-medium">{p.name}</td><td className="px-4 py-3">{serverName(p.server_id)}</td><td className="px-4 py-3">{p.provider}</td><td className="px-4 py-3">{p.domains}</td><td className="px-4 py-3 text-xs text-muted">{p.last_ip || "—"}{p.last_updated ? ` · ${fmtDateTime(p.last_updated)}` : ""}</td><td className="px-4 py-3"><div className="flex justify-end gap-1"><button title="测试" onClick={() => testDDNS.mutate(p.id)} className="rounded p-1.5 text-accent hover:bg-accent/10"><Play className="h-4 w-4" /></button><button title="编辑" onClick={() => setDDNSForm({ id: p.id, server_id: p.server_id, name: p.name, provider: p.provider, access_key: "", domains: p.domains, webhook_url: p.webhook_url, enabled: p.enabled })} className="rounded p-1.5 text-muted"><Pencil className="h-4 w-4" /></button><button title="删除" onClick={() => confirm(`删除 DDNS「${p.name}」？`) && removeDDNS.mutate(p.id)} className="rounded p-1.5 text-err"><Trash2 className="h-4 w-4" /></button></div></td></tr>)}{profiles.length === 0 && <tr><td colSpan={6} className="px-4 py-8 text-center text-muted">暂无 DDNS 配置</td></tr>}</tbody></table></div>
        {testResult && <pre className="mt-3 whitespace-pre-wrap rounded-lg border border-border bg-bg p-3 text-xs">{testResult}</pre>}
      </section>

      <section>
        <div className="mb-4 flex items-center justify-between"><div><h2 className="text-xl font-semibold">NAT 内网穿透</h2><p className="text-sm text-muted">将域名流量转发到服务器可访问的内网地址</p></div><button onClick={() => setNATForm({ ...emptyNAT, server_id: servers[0]?.id ?? 0 })} className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-white"><Plus className="h-4 w-4" /> 新建 NAT</button></div>
        {natForm && <div className="mb-4 grid grid-cols-1 gap-3 rounded-xl border border-border bg-panel p-4 md:grid-cols-3"><select value={natForm.server_id} onChange={(e) => setNATForm({ ...natForm, server_id: Number(e.target.value) })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm"><option value={0}>选择服务器</option>{servers.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}</select><input aria-label="NAT 域名" placeholder="nat.example.com" value={natForm.domain} onChange={(e) => setNATForm({ ...natForm, domain: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" /><input aria-label="目标地址" placeholder="127.0.0.1:3000" value={natForm.target_addr} onChange={(e) => setNATForm({ ...natForm, target_addr: e.target.value })} className="rounded-lg border border-border bg-bg px-3 py-2 text-sm" /><div className="flex gap-2 md:col-span-3"><button disabled={!natForm.server_id || !natForm.domain || !natForm.target_addr} onClick={() => saveNAT.mutate(natForm)} className="rounded-lg bg-accent px-4 py-2 text-sm text-white disabled:opacity-40">保存</button><button onClick={() => setNATForm(null)} className="rounded-lg border border-border px-4 py-2 text-sm">取消</button></div></div>}
        <div className="overflow-hidden rounded-xl border border-border bg-panel"><table className="w-full text-sm"><thead><tr className="border-b border-border text-left text-xs text-muted"><th className="px-4 py-3">域名</th><th className="px-4 py-3">服务器</th><th className="px-4 py-3">目标地址</th><th className="px-4 py-3">状态</th><th className="px-4 py-3 text-right">操作</th></tr></thead><tbody>{nats.map((n: NATProfile) => <tr key={n.id} className="border-b border-border last:border-0"><td className="px-4 py-3 font-medium">{n.domain}</td><td className="px-4 py-3">{serverName(n.server_id)}</td><td className="px-4 py-3 font-mono text-xs">{n.target_addr}</td><td className="px-4 py-3"><span className={n.enabled ? "text-ok" : "text-muted"}>{n.enabled ? "启用" : "停用"}</span></td><td className="px-4 py-3"><div className="flex justify-end gap-1"><button title="编辑" onClick={() => setNATForm({ id: n.id, server_id: n.server_id, domain: n.domain, target_addr: n.target_addr, enabled: n.enabled })} className="rounded p-1.5 text-muted"><Pencil className="h-4 w-4" /></button><button title="删除" onClick={() => confirm(`删除 NAT「${n.domain}」？`) && removeNAT.mutate(n.id)} className="rounded p-1.5 text-err"><Trash2 className="h-4 w-4" /></button></div></td></tr>)}{nats.length === 0 && <tr><td colSpan={5} className="px-4 py-8 text-center text-muted">暂无 NAT 配置</td></tr>}</tbody></table></div>
      </section>
    </div>
  );
}
