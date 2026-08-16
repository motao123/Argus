#!/usr/bin/env node
// i18n key 一致性检查（里程碑7）：
//   1. zh-CN 与 en 语言包的扁平 key 集合必须完全一致（缺 key / 多余 key 均报错）
//   2. 每个 key 的插值占位符（{name}）集合必须一致
//   3. src 中 t("...") / t('...') 使用的 key 必须存在于语言包
// 用法: node --experimental-strip-types scripts/check-i18n.mjs
// （Node >= 22.18 类型剥离默认开启，可省略该 flag）
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const webRoot = fileURLToPath(new URL("..", import.meta.url));
const srcRoot = join(webRoot, "src");

// 与 src/locales/index.ts 的 flattenMessages 保持一致的扁平化逻辑
function flatten(messages, prefix = "") {
  const out = {};
  for (const [key, value] of Object.entries(messages)) {
    const flatKey = prefix ? `${prefix}.${key}` : key;
    if (typeof value === "string") out[flatKey] = value;
    else Object.assign(out, flatten(value, flatKey));
  }
  return out;
}

function placeholderSet(template) {
  const set = new Set();
  for (const match of String(template).matchAll(/\{(\w+)\}/g)) set.add(match[1]);
  return set;
}

function sortedKeys(record) {
  return Object.keys(record).sort();
}

function diffKeys(aKeys, bKeys) {
  const aSet = new Set(aKeys);
  const bSet = new Set(bKeys);
  return {
    onlyInZh: aKeys.filter((k) => !bSet.has(k)),
    onlyInEn: bKeys.filter((k) => !aSet.has(k)),
  };
}

// ---- 1. 加载语言包（纯 JS 字面量，Node type-stripping 可直接加载） ----
const zhCN = (await import("../src/locales/zh-CN.ts")).default;
const en = (await import("../src/locales/en.ts")).default;

const zhFlat = flatten(zhCN);
const enFlat = flatten(en);
const zhKeys = sortedKeys(zhFlat);
const enKeys = sortedKeys(enFlat);

const errors = [];
const warnings = [];

// ---- 2. key 集合一致性 ----
const { onlyInZh, onlyInEn } = diffKeys(zhKeys, enKeys);
if (onlyInZh.length > 0) {
  errors.push(`以下 key 仅存在于 zh-CN（en 缺失）：\n${onlyInZh.map((k) => `  - ${k}`).join("\n")}`);
}
if (onlyInEn.length > 0) {
  errors.push(`以下 key 仅存在于 en（zh-CN 缺失）：\n${onlyInEn.map((k) => `  - ${k}`).join("\n")}`);
}

// ---- 3. 占位符一致性 ----
for (const key of zhKeys) {
  const zhPlaceholders = placeholderSet(zhFlat[key]);
  const enPlaceholders = placeholderSet(enFlat[key]);
  const missingInEn = [...zhPlaceholders].filter((p) => !enPlaceholders.has(p));
  const missingInZh = [...enPlaceholders].filter((p) => !zhPlaceholders.has(p));
  if (missingInEn.length > 0 || missingInZh.length > 0) {
    errors.push(
      `key "${key}" 占位符不一致：zh-CN=${[...zhPlaceholders].join(",") || "(无)"} en=${[...enPlaceholders].join(",") || "(无)"}`,
    );
  }
}

// ---- 4. src 中使用的 key 必须存在 ----
// 严格检查：t("literal") / t('literal') 直接调用点（覆盖动态 key 之外的绝大多数情况）
const validKeys = new Set([...zhKeys, ...enKeys]);
const usedKeys = new Set();
const strictUsedKeys = new Set();
function walk(dir) {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) {
      walk(full);
    } else if (/\.(ts|tsx)$/.test(entry)) {
      const source = readFileSync(full, "utf8");
      for (const match of source.matchAll(/\bt\(\s*["']([^"']+)["']/g)) {
        usedKeys.add(match[1]);
        strictUsedKeys.add(match[1]);
      }
      // 宽松检查：源码任意位置的合法 key 字面量（覆盖 t(someVar) 间接引用）
      for (const match of source.matchAll(/["']([a-zA-Z][a-zA-Z0-9-]*\.[a-zA-Z0-9.]+)["']/g)) {
        if (validKeys.has(match[1])) usedKeys.add(match[1]);
      }
    }
  }
}
walk(srcRoot);

const missingKeys = [...strictUsedKeys].filter((k) => !validKeys.has(k));
if (missingKeys.length > 0) {
  errors.push(`src 中使用了语言包不存在的 key：\n${missingKeys.map((k) => `  - ${k}`).join("\n")}`);
}

const unusedKeys = validKeys.size > 0 ? [...validKeys].filter((k) => !usedKeys.has(k)) : [];
if (unusedKeys.length > 0) {
  warnings.push(`语言包中存在未使用的 key（${unusedKeys.length} 个，仅供参考）：\n${unusedKeys.map((k) => `  - ${k}`).join("\n")}`);
}

// ---- 输出 ----
for (const warning of warnings) console.warn(`[warn] ${warning}\n`);
if (errors.length > 0) {
  console.error(`[fail] 发现 ${errors.length} 个 i18n 一致性问题：\n`);
  for (const error of errors) console.error(`  ${error}\n`);
  process.exit(1);
}
console.log(`[ok] i18n key 一致性检查通过：zh-CN ${zhKeys.length} 个 key / en ${enKeys.length} 个 key，src 中使用了 ${usedKeys.size} 个 key${warnings.length > 0 ? `（${warnings.length} 条警告，见上）` : ""}`);
process.exit(0);
