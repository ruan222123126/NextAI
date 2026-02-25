import { describe, expect, it } from "vitest";

import {
  buildProcessBizParams,
  nextPromptMode,
  normalizePromptMode,
  resolveModesFromMeta,
} from "../../src/tui/mode.js";

describe("tui mode helpers", () => {
  it("normalizes prompt mode", () => {
    expect(normalizePromptMode("codex")).toBe("default");
    expect(normalizePromptMode("unknown")).toBe("default");
    expect(normalizePromptMode(null)).toBe("default");
  });

  it("cycles prompt mode", () => {
    expect(nextPromptMode("default")).toBe("default");
  });

  it("resolves modes from chat meta", () => {
    expect(resolveModesFromMeta({ prompt_mode: "codex", collaboration_mode: "plan" })).toEqual({
      promptMode: "default",
    });
    expect(resolveModesFromMeta({ prompt_mode: "legacy", collaboration_mode: "execute" })).toEqual({
      promptMode: "default",
    });
    expect(resolveModesFromMeta({})).toEqual({
      promptMode: "default",
    });
  });

  it("builds biz_params with explicit prompt mode", () => {
    expect(buildProcessBizParams("default")).toEqual({
      prompt_mode: "default",
    });
    expect(buildProcessBizParams("default")).toEqual({
      prompt_mode: "default",
    });
  });
});
