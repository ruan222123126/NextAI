export interface ErrorEnvelope {
  error?: {
    code?: string;
    message?: string;
    details?: unknown;
  };
}

export interface EnvMapErrorMessages {
  invalidJSON: string;
  invalidMap: string;
  invalidKey: string;
  invalidValue: (key: string) => string;
}

const defaultEnvMapErrors: EnvMapErrorMessages = {
  invalidJSON: "invalid_json: 环境变量负载必须是合法 JSON",
  invalidMap: "invalid_env_map: 期望对象映射",
  invalidKey: "invalid_env_key: 键不能为空",
  invalidValue: (key: string) => `invalid_env_value: 键 ${key} 的值必须是字符串`,
};

export function parseErrorMessage(raw: string, status: number, fallbackMessage?: string): string {
  const fallback = fallbackMessage ?? `请求失败（${status}）`;
  if (raw.trim() === "") {
    return fallback;
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return raw;
  }

  const errorBody = (parsed as ErrorEnvelope).error;
  const code = typeof errorBody?.code === "string" ? errorBody.code : "";
  const message = typeof errorBody?.message === "string" ? errorBody.message : "";
  if (code !== "" && message !== "") {
    return `${code}: ${message}`;
  }
  if (message !== "") {
    return message;
  }
  return raw;
}

export function parseEnvMap(raw: string, messages: EnvMapErrorMessages = defaultEnvMapErrors): Record<string, string> {
  if (raw.trim() === "") {
    return {};
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    throw new Error(messages.invalidJSON);
  }

  if (!isRecord(parsed)) {
    throw new Error(messages.invalidMap);
  }

  const out: Record<string, string> = {};
  for (const [key, value] of Object.entries(parsed)) {
    if (key.trim() === "") {
      throw new Error(messages.invalidKey);
    }
    if (typeof value !== "string") {
      throw new Error(messages.invalidValue(key));
    }
    out[key] = value;
  }
  return out;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
