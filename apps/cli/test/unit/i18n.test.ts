import { describe, expect, it } from "vitest";

import { DEFAULT_LOCALE, resolveLocale, resolveLocaleFromArgv, setLocale, t } from "../../src/i18n.js";

describe("cli i18n", () => {
  it("resolves locale from language tags", () => {
    expect(resolveLocale("zh")).toBe("zh-CN");
    expect(resolveLocale("en-US")).toBe("en-US");
    expect(resolveLocale("ja")).toBe(DEFAULT_LOCALE);
  });

  it("extracts locale from argv", () => {
    expect(resolveLocaleFromArgv(["--locale", "en-US", "chats", "list"])).toBe("en-US");
    expect(resolveLocaleFromArgv(["--locale=zh-CN", "chats", "list"])).toBe("zh-CN");
    expect(resolveLocaleFromArgv(["chats", "list"])).toBeUndefined();
  });

  it("translates keys", () => {
    setLocale("en-US");
    expect(t("cli.command.chats")).toBe("Chat management");

    setLocale("zh-CN");
    expect(t("cli.command.chats")).toBe("会话管理");
  });
});
