import { enUS } from "./locales/en-US.js";
import { zhCN, type WebMessageKey, type WebMessages } from "./locales/zh-CN.js";

export type Locale = "zh-CN" | "en-US";

export const DEFAULT_LOCALE: Locale = "zh-CN";

const dictionaries: Record<Locale, WebMessages> = {
  "zh-CN": zhCN,
  "en-US": enUS,
};

let currentLocale: Locale = DEFAULT_LOCALE;

export function resolveLocale(raw?: string | null): Locale {
  const normalized = (raw ?? "").trim().toLowerCase();
  if (normalized.startsWith("zh")) {
    return "zh-CN";
  }
  if (normalized.startsWith("en")) {
    return "en-US";
  }
  return DEFAULT_LOCALE;
}

export function setLocale(raw?: string | null): Locale {
  currentLocale = resolveLocale(raw);
  return currentLocale;
}

export function getLocale(): Locale {
  return currentLocale;
}

export function isWebMessageKey(value: string): value is WebMessageKey {
  return Object.prototype.hasOwnProperty.call(zhCN, value);
}

export function t(key: WebMessageKey, params?: Record<string, string | number | boolean>): string {
  const localeDict = dictionaries[currentLocale] ?? dictionaries[DEFAULT_LOCALE];
  const fallbackDict = dictionaries[DEFAULT_LOCALE];
  const template = localeDict[key] ?? fallbackDict[key] ?? key;
  if (!params) {
    return template;
  }
  return template.replace(/\{\{\s*(\w+)\s*\}\}/g, (_full, token: string) => {
    const value = params[token];
    return value === undefined ? "" : String(value);
  });
}
