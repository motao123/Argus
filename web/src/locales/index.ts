// 语言包聚合：类型定义、扁平化与消息表。
// 保持 zh-CN.ts / en.ts 为纯 JS 字面量（供 check-i18n.mjs 直接加载），
// 类型一致性在这里通过 `const en: Messages = ...` 在编译期强制。
import zhCN from "./zh-CN";
import enRaw from "./en";

export type Lang = "zh-CN" | "en";

export const LANGS: readonly Lang[] = ["zh-CN", "en"];

/** 嵌套消息结构（以 zh-CN 为基准类型）。 */
export type Messages = typeof zhCN;

/** 编译期校验：en 与 zh-CN 结构完全一致（缺 key 或多余 key 都会报错）。 */
const en: Messages = enRaw;

export const locales: Record<Lang, Messages> = { "zh-CN": zhCN, en };

/** 递归展开嵌套消息 → 扁平 { "common.login": "登录", ... }。 */
export function flattenMessages<T>(messages: T, prefix = ""): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [key, value] of Object.entries(messages as Record<string, unknown>)) {
    const flatKey = prefix ? `${prefix}.${key}` : key;
    if (typeof value === "string") out[flatKey] = value;
    else Object.assign(out, flattenMessages(value, flatKey));
  }
  return out;
}

/** 扁平后的全部合法 key（模板字面量联合类型，供 t() 编译期校验）。 */
type FlattenKeys<T, P extends string = ""> = {
  [K in keyof T]: T[K] extends string
    ? P extends ""
      ? K
      : `${P}.${K & string}`
    : FlattenKeys<T[K], P extends "" ? K & string : `${P}.${K & string}`>;
}[keyof T];

export type TKey = FlattenKeys<Messages>;

export const messages: Record<Lang, Record<string, string>> = {
  "zh-CN": flattenMessages(zhCN),
  en: flattenMessages(en),
};
