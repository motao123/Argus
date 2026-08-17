import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Ban as BanIcon, ShieldAlert, ShieldOff } from "lucide-react";
import { api, type OnlineUser, type WafBan } from "../lib/api";
import { useI18n } from "../lib/i18n";

const BAN_PAGE_SIZE = 50;
// 常用封禁时长（小时）：24 / 7天 / 30天 / 永久
const HOUR_OPTIONS = [24, 168, 720, 0];

export default function Waf() {
  const { t, fmtDateTime } = useI18n();
  const qc = useQueryClient();
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [ip, setIp] = useState("");
  const [reason, setReason] = useState("");
  const [hours, setHours] = useState(24);
  const [banOffset, setBanOffset] = useState(0);

  const onlineQuery = useQuery({
    queryKey: ["admin-online"],
    queryFn: api.onlineUsers,
    refetchInterval: 10000,
  });
  const online = onlineQuery.data?.online ?? [];
  const bansQuery = useQuery({
    queryKey: ["waf-bans", banOffset],
    queryFn: () => api.wafBans(banOffset, BAN_PAGE_SIZE),
  });
  const bans = bansQuery.data?.bans ?? [];
  const total = bansQuery.data?.pagination?.total ?? 0;

  const refresh = () => {
    qc.invalidateQueries({ queryKey: ["admin-online"] });
    qc.invalidateQueries({ queryKey: ["waf-bans"] });
  };

  const ban = useMutation({
    mutationFn: ({ ip: target, reason: r, hours: h }: { ip: string; reason: string; hours: number }) => api.banIP(target, r, h),
    onSuccess: refresh,
  });
  const unban = useMutation({
    mutationFn: api.unbanIP,
    onSuccess: refresh,
  });

  const toggle = (ip: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(ip)) next.delete(ip);
      else next.add(ip);
      return next;
    });
  };
  const toggleAll = (checked: boolean) => {
    setSelected(checked ? new Set(online.map((u) => u.ip)) : new Set());
  };

  const selectedIps = online.filter((u) => selected.has(u.ip)).map((u) => u.ip);

  const submitBan = () => {
    const target = ip.trim();
    if (!target) return;
    ban.mutate({ ip: target, reason: reason.trim(), hours });
    setIp("");
    setReason("");
  };

  const banOnline = (u: OnlineUser) => {
    if (!confirm(t("waf.confirmBan", { ip: u.ip }))) return;
    ban.mutate({ ip: u.ip, reason: "", hours });
  };
  const banSelected = () => {
    if (selectedIps.length === 0) return;
    if (!confirm(t("waf.confirmBanSelected", { n: selectedIps.length }))) return;
    selectedIps.forEach((target) => ban.mutate({ ip: target, reason: reason.trim(), hours }));
    setSelected(new Set());
  };

  const sourceLabel = (s: WafBan["source"]) =>
    s === "rate" ? t("waf.sourceRate") : s === "login" ? t("waf.sourceLogin") : t("waf.sourceManual");
  const methodLabel = (m: OnlineUser["auth_method"]) =>
    m === "jwt" ? t("waf.methodJwt") : m === "pat" ? t("waf.methodPat") : t("waf.methodGuest");

  return (
    <div className="space-y-8">
      <section>
        <h1 className="mb-1 flex items-center gap-2 text-xl font-semibold">
          <ShieldAlert className="h-5 w-5 text-accent" /> {t("waf.title")}
        </h1>
        <p className="mb-4 text-sm text-muted">{t("waf.subtitle")}</p>
      </section>

      {/* 在线用户列表 */}
      <section>
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
          <div>
            <h2 className="text-lg font-semibold">{t("waf.onlineTitle")}</h2>
            <p className="text-sm text-muted">{t("waf.onlineSubtitle")}</p>
          </div>
          <button
            onClick={banSelected}
            disabled={selectedIps.length === 0 || ban.isPending}
            className="flex items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-sm text-white disabled:cursor-not-allowed disabled:opacity-40"
          >
            <BanIcon className="h-4 w-4" />
            {t("waf.banSelected")}
            {selectedIps.length > 0 && <span className="rounded-full bg-white/20 px-1.5 text-xs">{selectedIps.length}</span>}
          </button>
        </div>

        <div className="overflow-x-auto rounded-xl border border-border bg-panel">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-muted">
                <th className="w-10 px-4 py-2.5">
                  <input
                    type="checkbox"
                    aria-label={t("waf.selectAll")}
                    checked={online.length > 0 && selected.size === online.length}
                    onChange={(e) => toggleAll(e.target.checked)}
                    className="accent-accent"
                  />
                </th>
                <th className="px-4 py-2.5">{t("common.ip")}</th>
                <th className="px-4 py-2.5">{t("common.user")}</th>
                <th className="px-4 py-2.5">{t("waf.authMethod")}</th>
                <th className="px-4 py-2.5">{t("waf.lastActive")}</th>
                <th className="px-4 py-2.5">{t("waf.connections")}</th>
                <th className="px-4 py-2.5 text-right">{t("common.actions")}</th>
              </tr>
            </thead>
            <tbody>
              {online.map((u) => (
                <tr key={u.ip} className="border-b border-border last:border-0">
                  <td className="px-4 py-2.5">
                    <input
                      type="checkbox"
                      aria-label={t("waf.select", { ip: u.ip })}
                      checked={selected.has(u.ip)}
                      onChange={() => toggle(u.ip)}
                      className="accent-accent"
                    />
                  </td>
                  <td className="px-4 py-2.5 font-mono text-xs">{u.ip}</td>
                  <td className="px-4 py-2.5 font-medium">{u.username || <span className="text-muted">—</span>}</td>
                  <td className="px-4 py-2.5 text-xs text-muted">{methodLabel(u.auth_method)}</td>
                  <td className="whitespace-nowrap px-4 py-2.5 text-xs text-muted">{fmtDateTime(u.last_active_at)}</td>
                  <td className="px-4 py-2.5 tabular">
                    {u.connections > 0 ? <span className="text-ok">{u.connections}</span> : <span className="text-muted">0</span>}
                  </td>
                  <td className="px-4 py-2.5 text-right">
                    <button
                      onClick={() => banOnline(u)}
                      title={t("waf.banIp", { ip: u.ip })}
                      className="flex items-center gap-1 rounded p-1.5 text-err hover:bg-err/10"
                    >
                      <BanIcon className="h-4 w-4" />
                    </button>
                  </td>
                </tr>
              ))}
              {online.length === 0 && (
                <tr>
                  <td colSpan={7} className="px-4 py-8 text-center text-muted">
                    {t("waf.noOnline")}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </section>

      {/* 封禁管理 */}
      <section>
        <h2 className="mb-1 text-lg font-semibold">{t("waf.banTitle")}</h2>
        <p className="mb-3 text-sm text-muted">{t("waf.banSubtitle")}</p>

        <div className="mb-4 flex flex-wrap items-center gap-2 rounded-xl border border-border bg-panel p-3">
          <input
            value={ip}
            onChange={(e) => setIp(e.target.value)}
            placeholder={t("waf.ipPlaceholder")}
            aria-label={t("waf.ipPlaceholder")}
            className="w-48 rounded-lg border border-border bg-bg px-3 py-2 text-sm"
          />
          <input
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            placeholder={t("waf.reasonPlaceholder")}
            aria-label={t("waf.reasonPlaceholder")}
            className="w-56 rounded-lg border border-border bg-bg px-3 py-2 text-sm"
          />
          <select
            value={hours}
            onChange={(e) => setHours(Number(e.target.value))}
            aria-label={t("waf.hours")}
            className="rounded-lg border border-border bg-bg px-3 py-2 text-sm"
          >
            {HOUR_OPTIONS.map((h) => (
              <option key={h} value={h}>
                {h === 0 ? t("waf.permanent") : t("waf.hoursOption", { n: h })}
              </option>
            ))}
          </select>
          <button
            onClick={submitBan}
            disabled={!ip.trim() || ban.isPending}
            className="flex items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-sm text-white disabled:cursor-not-allowed disabled:opacity-40"
          >
            <BanIcon className="h-4 w-4" /> {t("waf.ban")}
          </button>
        </div>

        <div className="overflow-x-auto rounded-xl border border-border bg-panel">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-muted">
                <th className="px-4 py-2.5">{t("common.ip")}</th>
                <th className="px-4 py-2.5">{t("waf.reason")}</th>
                <th className="px-4 py-2.5">{t("waf.source")}</th>
                <th className="px-4 py-2.5">{t("waf.count")}</th>
                <th className="px-4 py-2.5">{t("waf.bannedAt")}</th>
                <th className="px-4 py-2.5">{t("waf.expireAt")}</th>
                <th className="px-4 py-2.5 text-right">{t("common.actions")}</th>
              </tr>
            </thead>
            <tbody>
              {bans.map((b) => (
                <tr key={b.id} className="border-b border-border last:border-0">
                  <td className="px-4 py-2.5 font-mono text-xs">{b.ip}</td>
                  <td className="max-w-[200px] truncate px-4 py-2.5 text-xs text-muted" title={b.reason}>
                    {b.reason || "—"}
                  </td>
                  <td className="px-4 py-2.5 text-xs">
                    <span className={`rounded-full px-2 py-0.5 ${b.source === "manual" ? "bg-err/15 text-err" : "bg-muted/20 text-muted"}`}>
                      {sourceLabel(b.source)}
                    </span>
                  </td>
                  <td className="px-4 py-2.5 tabular text-muted">{b.count}</td>
                  <td className="whitespace-nowrap px-4 py-2.5 text-xs text-muted">{fmtDateTime(b.banned_at)}</td>
                  <td className="whitespace-nowrap px-4 py-2.5 text-xs text-muted">
                    {b.expire_at ? fmtDateTime(b.expire_at) : <span className="text-err">{t("waf.permanent")}</span>}
                  </td>
                  <td className="px-4 py-2.5 text-right">
                    <button
                      onClick={() => confirm(t("waf.confirmUnban", { ip: b.ip })) && unban.mutate(b.ip)}
                      title={t("waf.unban")}
                      className="flex items-center gap-1 rounded p-1.5 text-accent hover:bg-accent/10"
                    >
                      <ShieldOff className="h-4 w-4" />
                    </button>
                  </td>
                </tr>
              ))}
              {bans.length === 0 && (
                <tr>
                  <td colSpan={7} className="px-4 py-8 text-center text-muted">
                    {t("waf.noBans")}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
        {total > BAN_PAGE_SIZE && (
          <div className="mt-3 flex items-center gap-3 text-sm">
            <button
              disabled={banOffset === 0}
              onClick={() => setBanOffset(Math.max(0, banOffset - BAN_PAGE_SIZE))}
              className="rounded-lg border border-border px-3 py-1.5 disabled:opacity-40"
            >
              {t("waf.prev")}
            </button>
            <span className="text-muted">{t("waf.page", { page: Math.floor(banOffset / BAN_PAGE_SIZE) + 1 })}</span>
            <button
              disabled={banOffset + BAN_PAGE_SIZE >= total}
              onClick={() => setBanOffset(banOffset + BAN_PAGE_SIZE)}
              className="rounded-lg border border-border px-3 py-1.5 disabled:opacity-40"
            >
              {t("waf.next")}
            </button>
          </div>
        )}
      </section>
    </div>
  );
}
