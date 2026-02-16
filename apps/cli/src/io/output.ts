import { ApiClientError } from "../client/api-client.js";

let jsonMode = false;

const codeHints: Record<string, string> = {
  invalid_json: "请求 JSON 格式非法，检查命令参数中的 JSON 字符串。",
  invalid_request: "请求字段缺失或格式错误，检查必填参数。",
  not_found: "资源不存在，确认 ID 或名称是否正确。",
  unauthorized: "鉴权失败，请检查 COPAW_API_KEY 或 Authorization。",
  provider_not_configured: "模型提供方未配置，先执行 models config/active-set。",
  provider_not_supported: "模型提供方不受支持，检查 provider_id。",
  provider_request_failed: "上游模型请求失败，检查 API Key/Base URL/网络。",
  provider_invalid_reply: "上游返回格式异常，检查模型服务返回。",
  store_error: "网关存储写入失败，检查 COPAW_DATA_DIR 权限与磁盘状态。",
};

export function setOutputJSONMode(enabled: boolean): void {
  jsonMode = enabled;
}

export function printResult(data: unknown): void {
  console.log(JSON.stringify(data, null, jsonMode ? 0 : 2));
}

function printJSONError(error: { code: string; message: string; details?: unknown; status?: number }): void {
  const payload: Record<string, unknown> = {
    error: {
      code: error.code,
      message: error.message,
      details: error.details ?? null,
    },
  };
  if (typeof error.status === "number") {
    payload.status = error.status;
  }
  console.error(JSON.stringify(payload));
}

export function printError(err: unknown): void {
  if (err instanceof ApiClientError) {
    if (jsonMode) {
      printJSONError({
        code: err.code,
        message: err.message,
        details: err.details,
        status: err.status,
      });
      return;
    }
    const hint = codeHints[err.code];
    console.error(`[${err.code}] ${err.message}`);
    if (hint) {
      console.error(`hint: ${hint}`);
    }
    return;
  }

  const message = err instanceof Error ? err.message : String(err);
  if (jsonMode) {
    printJSONError({ code: "cli_error", message });
    return;
  }
  console.error(message);
}
