export type TUIPromptMode = "default";

export interface TUIModeState {
  promptMode: TUIPromptMode;
}

export function normalizePromptMode(raw: unknown): TUIPromptMode {
  void raw;
  return "default";
}

export function nextPromptMode(current: TUIPromptMode): TUIPromptMode {
  void current;
  return "default";
}

export function resolveModesFromMeta(meta: unknown): TUIModeState {
  if (!meta || typeof meta !== "object" || Array.isArray(meta)) {
    return {
      promptMode: "default",
    };
  }
  const row = meta as Record<string, unknown>;
  return {
    promptMode: normalizePromptMode(row.prompt_mode),
  };
}

export function buildProcessBizParams(promptMode: TUIPromptMode): Record<string, unknown> {
  return {
    prompt_mode: promptMode,
  };
}
