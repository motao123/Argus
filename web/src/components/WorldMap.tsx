import { useEffect, useMemo, useRef, useState } from "react";
import * as echarts from "echarts";
import { feature } from "topojson-client";

import { countryFlag, type Server } from "../lib/api";
import { useI18n } from "../lib/i18n";

let worldRegistered = false;
let registerPromise: Promise<boolean> | null = null;

// 国家/地区中心点坐标（ISO 3166-1 alpha-2）。使用固定坐标，避免把国家码误当作经纬度。
const COUNTRY_COORDINATES: Readonly<Record<string, readonly [number, number]>> = {
  AD: [1.6, 42.55], AE: [54.37, 24.47], AF: [67.71, 33.94], AG: [-61.8, 17.06], AL: [20.17, 41.15],
  AM: [45.04, 40.07], AO: [17.87, -11.2], AR: [-63.62, -38.42], AT: [14.55, 47.52], AU: [133.78, -25.27],
  AZ: [47.58, 40.14], BA: [17.68, 43.92], BB: [-59.54, 13.19], BD: [90.36, 23.68], BE: [4.47, 50.5],
  BF: [-1.56, 12.24], BG: [25.49, 42.73], BH: [50.56, 26.07], BI: [29.92, -3.37], BJ: [2.32, 9.31],
  BN: [114.73, 4.54], BO: [-63.59, -16.29], BR: [-51.93, -14.24], BS: [-77.4, 25.03], BT: [90.43, 27.51],
  BW: [24.68, -22.33], BY: [27.95, 53.71], BZ: [-88.5, 17.19], CA: [-106.35, 56.13], CD: [21.76, -4.04],
  CF: [20.94, 6.61], CG: [15.83, -0.23], CH: [8.23, 46.82], CI: [-5.55, 7.54], CL: [-71.54, -35.68],
  CM: [12.35, 7.37], CN: [104.2, 35.86], CO: [-74.3, 4.57], CR: [-83.75, 9.75], CU: [-77.78, 21.52],
  CV: [-24.01, 16.0], CY: [33.43, 35.13], CZ: [15.47, 49.82], DE: [10.45, 51.17], DJ: [42.59, 11.83],
  DK: [9.5, 56.26], DO: [-70.16, 18.74], DZ: [1.66, 28.03], EC: [-78.18, -1.83], EE: [25.01, 58.6],
  EG: [30.8, 26.82], ER: [39.78, 15.18], ES: [-3.75, 40.46], ET: [40.49, 9.15], FI: [25.75, 61.92],
  FJ: [178.07, -17.71], FR: [2.21, 46.23], GA: [11.61, -0.8], GB: [-3.44, 55.38], GE: [43.36, 42.32],
  GH: [-1.02, 7.95], GM: [-15.31, 13.44], GN: [-9.7, 9.95], GQ: [10.27, 1.65], GR: [21.82, 39.07],
  GT: [-90.23, 15.78], GW: [-15.18, 11.8], GY: [-58.93, 4.86], HK: [114.17, 22.32], HN: [-86.24, 15.2],
  HR: [15.2, 45.1], HT: [-72.29, 18.97], HU: [19.5, 47.16], ID: [113.92, -0.79], IE: [-8.24, 53.41],
  IL: [34.85, 31.05], IN: [78.96, 20.59], IQ: [43.68, 33.22], IR: [53.69, 32.43], IS: [-19.02, 64.96],
  IT: [12.57, 41.87], JM: [-77.3, 18.11], JO: [36.24, 30.59], JP: [138.25, 36.2], KE: [37.91, -0.02],
  KG: [74.77, 41.2], KH: [104.99, 12.57], KP: [127.51, 40.34], KR: [127.77, 35.91], KW: [47.48, 29.31],
  KZ: [66.92, 48.02], LA: [102.5, 19.86], LB: [35.86, 33.85], LI: [9.56, 47.17], LK: [80.77, 7.87],
  LR: [-9.43, 6.43], LS: [28.23, -29.61], LT: [23.88, 55.17], LU: [6.13, 49.82], LV: [24.6, 56.88],
  LY: [17.23, 26.34], MA: [-7.09, 31.79], MC: [7.42, 43.74], MD: [28.37, 47.41], ME: [19.37, 42.71],
  MG: [46.87, -18.77], MK: [21.75, 41.61], ML: [-4.0, 17.57], MM: [95.96, 21.91], MN: [103.85, 46.86],
  MO: [113.54, 22.2], MR: [-10.94, 21.01], MT: [14.38, 35.94], MU: [57.55, -20.35], MV: [73.22, 3.2],
  MW: [34.3, -13.25], MX: [-102.55, 23.63], MY: [101.98, 4.21], MZ: [35.53, -18.67], NA: [18.49, -22.96],
  NE: [8.08, 17.61], NG: [8.68, 9.08], NI: [-85.21, 12.87], NL: [5.29, 52.13], NO: [8.47, 60.47],
  NP: [84.12, 28.39], NZ: [174.89, -40.9], OM: [55.98, 21.47], PA: [-80.78, 8.54], PE: [-75.02, -9.19],
  PG: [143.96, -6.31], PH: [121.77, 12.88], PK: [69.35, 30.38], PL: [19.15, 51.92], PR: [-66.59, 18.22],
  PT: [-8.22, 39.4], PY: [-58.44, -23.44], QA: [51.18, 25.35], RO: [24.97, 45.94], RS: [21.01, 44.02],
  RU: [105.32, 61.52], RW: [29.87, -1.94], SA: [45.08, 23.89], SD: [30.22, 12.86], SE: [18.64, 60.13],
  SG: [103.82, 1.35], SI: [14.995, 46.15], SK: [19.7, 48.67], SL: [-11.78, 8.46], SM: [12.46, 43.94],
  SN: [-14.45, 14.5], SO: [46.2, 5.15], SR: [-56.03, 3.92], SS: [31.31, 6.88], SV: [-88.9, 13.79],
  SY: [38.0, 34.8], SZ: [31.47, -26.52], TD: [18.73, 15.45], TG: [0.82, 8.62], TH: [100.99, 15.87],
  TJ: [71.28, 38.86], TL: [125.73, -8.87], TM: [59.56, 38.97], TN: [9.54, 33.89], TR: [35.24, 38.96],
  TT: [-61.22, 10.69], TW: [120.96, 23.7], TZ: [34.89, -6.37], UA: [31.17, 48.38], UG: [32.29, 1.37],
  US: [-95.71, 37.09], UY: [-55.77, -32.52], UZ: [64.59, 41.38], VA: [12.45, 41.9], VE: [-66.59, 6.42],
  VN: [108.28, 14.06], YE: [48.52, 15.55], ZA: [22.94, -30.56], ZM: [27.85, -13.13], ZW: [29.15, -19.02],
  // 海外领地、岛国及常见地区码。
  AI: [-63.07, 18.22], AQ: [0, -82.86], AS: [-170.13, -14.27], AW: [-69.97, 12.52], AX: [19.92, 60.18],
  BL: [-62.83, 17.9], BM: [-64.75, 32.31], BQ: [-68.26, 12.2], BV: [3.41, -54.42], CC: [96.87, -12.16],
  CK: [-159.78, -21.24], CW: [-68.99, 12.17], CX: [105.69, -10.45], DM: [-61.37, 15.41], EH: [-12.89, 24.22],
  FK: [-59.52, -51.8], FM: [150.55, 7.43], FO: [-6.91, 61.89], GF: [-53.13, 3.93], GG: [-2.59, 49.47],
  GI: [-5.35, 36.14], GL: [-42.6, 71.71], GP: [-61.55, 16.27], GS: [-36.59, -54.43], GU: [144.79, 13.44],
  HM: [73.5, -53.08], IM: [-4.55, 54.24], IO: [71.88, -6.34], JE: [-2.13, 49.21], KI: [-168.73, 1.87],
  KM: [43.87, -11.88], KN: [-62.78, 17.36], KY: [-80.57, 19.51], LC: [-60.98, 13.91], MF: [-63.05, 18.08],
  MH: [171.18, 7.13], MP: [145.67, 15.1], MQ: [-61.02, 14.64], MS: [-62.19, 16.74], NC: [165.62, -20.9],
  NF: [167.95, -29.04], NR: [166.93, -0.52], NU: [-169.87, -19.05], PF: [-149.41, -17.68], PM: [-56.27, 46.94],
  PN: [-127.44, -24.38], PS: [35.23, 31.95], PW: [134.58, 7.51], RE: [55.54, -21.12], SB: [160.16, -9.65],
  SC: [55.49, -4.68], SH: [-5.71, -15.97], SJ: [23.67, 77.55], ST: [6.61, 0.19], SX: [-63.05, 18.04],
  TC: [-71.8, 21.69], TF: [69.35, -49.28], TK: [-171.85, -9.2], TO: [-175.2, -21.18], TV: [177.65, -7.11],
  UM: [-177.37, 24.22], VC: [-61.29, 12.98], VG: [-64.64, 18.42], VI: [-64.9, 18.34], VU: [166.96, -15.38],
  WF: [-177.16, -13.77], WS: [-172.1, -13.76], XK: [20.9, 42.6], YT: [45.17, -12.83],
};

export interface WorldMapPoint {
  country: string;
  coordinates: readonly [number, number];
  online: number;
  total: number;
  names: string[];
}

// 纯聚合函数单独导出，便于验证国家码规范化、未知码过滤和在线计数。
export function aggregateServersByCountry(servers: readonly Server[]): WorldMapPoint[] {
  const byCountry = new Map<string, WorldMapPoint>();
  for (const server of servers) {
    const country = server.host?.country_code?.trim().toUpperCase();
    const coordinates = country ? COUNTRY_COORDINATES[country] : undefined;
    if (!country || !coordinates) continue;

    const point = byCountry.get(country) ?? { country, coordinates, online: 0, total: 0, names: [] };
    point.total += 1;
    if (server.online) point.online += 1;
    if (point.names.length < 3 && server.name) point.names.push(server.name);
    byCountry.set(country, point);
  }
  return Array.from(byCountry.values());
}

// 加载并注册世界地图；失败后清空 Promise，让用户可以真正重试。
async function ensureWorldMap(): Promise<boolean> {
  if (worldRegistered) return true;
  if (!registerPromise) {
    registerPromise = (async () => {
      try {
        const response = await fetch("/world.json");
        if (!response.ok) throw new Error(`HTTP ${response.status}`);
        const topo = (await response.json()) as any;
        const countries = topo?.objects?.countries;
        if (!countries) throw new Error("world.json 缺少 countries 对象");
        const geo = feature(topo, countries);
        echarts.registerMap("world", geo as any);
        worldRegistered = true;
        return true;
      } catch {
        registerPromise = null;
        return false;
      }
    })();
  }
  return registerPromise;
}

// 世界地图：按服务器国家码聚合打点。需服务器带 country_code
// （服务端配置 ARGUS_GEOIP_ENDPOINT 提供 GeoIP 数据源）。
export default function WorldMap({ servers }: { servers: Server[] }) {
  const { t, lang, fmtNumber } = useI18n();
  const containerRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<echarts.ECharts | null>(null);
  const [collapsed, setCollapsed] = useState(false);
  const [loadFailed, setLoadFailed] = useState(false);
  const [retryVersion, setRetryVersion] = useState(0);
  const points = useMemo(() => aggregateServersByCountry(servers), [servers]);

  useEffect(() => {
    if (collapsed || points.length === 0 || !containerRef.current) return;

    let cancelled = false;
    let resizeObserver: ResizeObserver | undefined;
    const container = containerRef.current;

    void ensureWorldMap().then((loaded) => {
      if (cancelled) return;
      if (!loaded) {
        setLoadFailed(true);
        return;
      }

      setLoadFailed(false);
      const chart = echarts.init(container);
      chartRef.current = chart;
      chart.setOption({
        tooltip: {
          formatter: (params: any) => {
            const data = params.data as WorldMapPoint & { value: number[] };
            return `${countryFlag(data.country)} ${data.country} · ${t("worldMap.onlineOf", { online: data.online, total: data.total })}<br/>${data.names.join(", ")}`;
          },
        },
        geo: {
          map: "world",
          roam: false,
          silent: true,
          itemStyle: { areaColor: "rgba(99,102,241,0.08)", borderColor: "rgba(99,102,241,0.4)" },
        },
        series: [{
          type: "scatter",
          coordinateSystem: "geo",
          data: points.map((point) => ({ ...point, name: point.country, value: [...point.coordinates, point.online, point.total] })),
          symbolSize: (value: number[]) => 6 + Math.min(14, (value[3] ?? 1) * 3),
          itemStyle: { color: "rgba(99,102,241,0.85)" },
        }],
      });

      resizeObserver = new ResizeObserver(() => chart.resize());
      resizeObserver.observe(container);
      chart.resize();
    });

    return () => {
      cancelled = true;
      resizeObserver?.disconnect();
      chartRef.current?.dispose();
      chartRef.current = null;
    };
  }, [collapsed, points, retryVersion, lang, t]);

  if (points.length === 0) return null;

  return (
    <div className="mb-4 rounded-xl border border-border bg-panel p-4">
      <div className="mb-2 flex items-center justify-between">
        <span className="text-sm font-medium">{t("worldMap.title")}</span>
        <button onClick={() => setCollapsed((value) => !value)} className="text-xs text-muted hover:text-fg">
          {collapsed ? t("worldMap.expand") : t("worldMap.collapse")}
        </button>
      </div>
      {!collapsed && (
        <div className="relative h-64 w-full">
          <div ref={containerRef} className="h-full w-full" />
          {loadFailed && (
            <div className="absolute inset-0 flex items-center justify-center bg-panel/90 text-sm text-muted">
              {t("worldMap.loadFailed")}
              <button className="text-accent hover:underline" onClick={() => setRetryVersion((value) => value + 1)}>{t("worldMap.retry")}</button>
            </div>
          )}
        </div>
      )}
      {collapsed && (
        <p className="text-xs text-muted">{t("worldMap.summary", { countries: fmtNumber(points.length), servers: fmtNumber(points.reduce((sum, point) => sum + point.total, 0)) })}</p>
      )}
    </div>
  );
}
