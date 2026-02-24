export type TUIPromptMode = "default" | "codex";
export type TUICollaborationMode = "default" | "plan" | "execute" | "pair_programming";

export interface TUIModeState {
  promptMode: TUIPromptMode;
  collaborationMode: TUICollaborationMode;
}

const promptModes: TUIPromptMode[] = ["default", "codex"];
const collaborationModes: TUICollaborationMode[] = ["default", "plan", "execute", "pair_programming"];

export function normalizePromptMode(raw: unknown): TUIPromptMode {
  if (typeof raw !== "string") {
    return "default";
  }
  const normalized = raw.trim().toLowerCase();
  if (normalized === "codex") {
    return "codex";
  }
  return "default";
}

export function normalizeCollaborationMode(raw: unknown): TUICollaborationMode {
  if (typeof raw !== "string") {
    return "default";
  }
  const normalized = raw.trim().toLowerCase();
  if (normalized === "plan") {
    return "plan";
  }
  if (normalized === "execute") {
    return "execute";
  }
  if (normalized === "pair_programming" || normalized === "pair-programming" || normalized === "pairprogramming") {
    return "pair_programming";
  }
  return "default";
}

export function nextPromptMode(current: TUIPromptMode): TUIPromptMode {
  const index = promptModes.indexOf(current);
  if (index < 0) {
    return promptModes[0] ?? "default";
  }
  return promptModes[(index + 1) % promptModes.length] ?? "default";
}

export function nextCollaborationMode(current: TUICollaborationMode): TUICollaborationMode {
  const index = collaborationModes.indexOf(current);
  if (index < 0) {
    return collaborationModes[0] ?? "default";
  }
  return collaborationModes[(index + 1) % collaborationModes.length] ?? "default";
}

export function resolveModesFromMeta(meta: unknown): TUIModeState {
  if (!meta || typeof meta !== "object" || Array.isArray(meta)) {
    return {
      promptMode: "default",
      collaborationMode: "default",
    };
  }
  const row = meta as Record<string, unknown>;
  return {
    promptMode: normalizePromptMode(row.prompt_mode),
    collaborationMode: normalizeCollaborationMode(row.collaboration_mode),
  };
}

export function buildProcessBizParams(promptMode: TUIPromptMode, collaborationMode: TUICollaborationMode): Record<string, unknown> {
  const out: Record<string, unknown> = {
    prompt_mode: promptMode,
  };
  if (promptMode === "codex") {
    out.collaboration_mode = collaborationMode;
  }
  return out;
}
