import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiClientError } from "../../src/client/api-client.js";
import { setLocale } from "../../src/i18n.js";
import { printError, setOutputJSONMode } from "../../src/io/output.js";

describe("cli output i18n", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    setOutputJSONMode(false);
    setLocale("zh-CN");
  });

  it("prints localized hint by locale", () => {
    const errors: string[] = [];
    vi.spyOn(console, "error").mockImplementation((...args) => {
      errors.push(args.join(" "));
    });

    setLocale("en-US");
    printError(
      new ApiClientError({
        status: 404,
        code: "not_found",
        message: "chat not found",
        details: null,
      }),
    );

    expect(errors[0]).toContain("[not_found] chat not found");
    expect(errors[1]).toContain("Resource not found");
  });
});
