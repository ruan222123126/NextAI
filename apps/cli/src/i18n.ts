import { enUS } from "./locales/en-US.js";
import { zhCN, type CliMessageKey, type CliMessages } from "./locales/zh-CN.js";

export type Locale = "zh-CN" | "en-US";

export const DEFAULT_LOCALE: Locale = "zh-CN";

const dictionaries: Record<Locale, CliMessages> = {
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

export function resolveLocaleFromArgv(argv: string[]): string | undefined {
  for (let i = 0; i < argv.length; i += 1) {
    const token = argv[i];
    if (token === "--locale") {
      return argv[i + 1];
    }
    if (token.startsWith("--locale=")) {
      return token.slice("--locale=".length);
    }
  }
  return undefined;
}

export function initializeLocale(argv: string[], envLocale: string | undefined): Locale {
  return setLocale(resolveLocaleFromArgv(argv) ?? envLocale ?? DEFAULT_LOCALE);
}

export function t(key: CliMessageKey, params?: Record<string, string | number | boolean>): string {
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
