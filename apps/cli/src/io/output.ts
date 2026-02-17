import { ApiClientError } from "../client/api-client.js";
import { t } from "../i18n.js";
import type { CliMessageKey } from "../locales/zh-CN.js";

let jsonMode = false;

const codeHintKeys: Record<string, CliMessageKey> = {
  invalid_json: "error_hint.invalid_json",
  invalid_request: "error_hint.invalid_request",
  not_found: "error_hint.not_found",
  unauthorized: "error_hint.unauthorized",
  provider_not_configured: "error_hint.provider_not_configured",
  provider_not_supported: "error_hint.provider_not_supported",
  provider_request_failed: "error_hint.provider_request_failed",
  provider_invalid_reply: "error_hint.provider_invalid_reply",
  store_error: "error_hint.store_error",
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
    const hintKey = codeHintKeys[err.code];
    console.error(`[${err.code}] ${err.message}`);
    if (hintKey) {
      const hint = t(hintKey);
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
