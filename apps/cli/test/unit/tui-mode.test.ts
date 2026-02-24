import { describe, expect, it } from "vitest";

import {
  buildProcessBizParams,
  nextCollaborationMode,
  nextPromptMode,
  normalizeCollaborationMode,
  normalizePromptMode,
  resolveModesFromMeta,
} from "../../src/tui/mode.js";

describe("tui mode helpers", () => {
  it("normalizes prompt mode", () => {
    expect(normalizePromptMode("codex")).toBe("codex");
    expect(normalizePromptMode("claude")).toBe("claude");
    expect(normalizePromptMode("unknown")).toBe("default");
    expect(normalizePromptMode(null)).toBe("default");
  });

  it("normalizes collaboration mode", () => {
    expect(normalizeCollaborationMode("plan")).toBe("plan");
    expect(normalizeCollaborationMode("execute")).toBe("execute");
    expect(normalizeCollaborationMode("pair-programming")).toBe("pair_programming");
    expect(normalizeCollaborationMode("unknown")).toBe("default");
    expect(normalizeCollaborationMode(undefined)).toBe("default");
  });

  it("cycles prompt and collaboration modes", () => {
    expect(nextPromptMode("default")).toBe("codex");
    expect(nextPromptMode("codex")).toBe("claude");
    expect(nextPromptMode("claude")).toBe("default");

    expect(nextCollaborationMode("default")).toBe("plan");
    expect(nextCollaborationMode("plan")).toBe("execute");
    expect(nextCollaborationMode("execute")).toBe("pair_programming");
    expect(nextCollaborationMode("pair_programming")).toBe("default");
  });

  it("resolves modes from chat meta", () => {
    expect(resolveModesFromMeta({ prompt_mode: "codex", collaboration_mode: "plan" })).toEqual({
      promptMode: "codex",
      collaborationMode: "plan",
    });
    expect(resolveModesFromMeta({ prompt_mode: "claude", collaboration_mode: "execute" })).toEqual({
      promptMode: "claude",
      collaborationMode: "execute",
    });
    expect(resolveModesFromMeta({})).toEqual({
      promptMode: "default",
      collaborationMode: "default",
    });
  });

  it("builds biz_params with explicit prompt/collaboration mode", () => {
    expect(buildProcessBizParams("default", "plan")).toEqual({
      prompt_mode: "default",
    });
    expect(buildProcessBizParams("codex", "plan")).toEqual({
      prompt_mode: "codex",
      collaboration_mode: "plan",
    });
  });
});
