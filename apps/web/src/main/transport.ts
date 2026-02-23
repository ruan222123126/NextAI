import { parseErrorMessage } from "../api-utils.js";

type HttpMethod = "GET" | "POST" | "PUT" | "DELETE";

export interface JSONRequestOptions {
  method?: HttpMethod;
  body?: unknown;
  headers?: Record<string, string>;
}

export interface TransportConfig {
  getApiBase: () => string;
  getApiKey: () => string;
  getLocale?: () => string;
  requestSourceHeader?: string;
  requestSourceValue?: string;
}

export interface TransportClient {
  applyDefaultHeaders(headers: Headers): void;
  requestJSON<T>(path: string, options?: JSONRequestOptions): Promise<T>;
  toAbsoluteURL(path: string): string;
}

export function createTransport(config: TransportConfig): TransportClient {
  const requestSourceHeader = config.requestSourceHeader ?? "X-NextAI-Source";
  const requestSourceValue = config.requestSourceValue ?? "web";

  function toAbsoluteURL(path: string): string {
    const base = config.getApiBase().replace(/\/+$/g, "");
    return `${base}${path}`;
  }

  async function requestJSON<T>(path: string, options: JSONRequestOptions = {}): Promise<T> {
    const response = await fetch(toAbsoluteURL(path), buildRequestInit(options));
    if (!response.ok) {
      const message = await readErrorMessage(response);
      throw new Error(message);
    }
    if (response.status === 204) {
      return undefined as T;
    }
    const text = await response.text();
    if (!text) {
      return undefined as T;
    }
    return JSON.parse(text) as T;
  }

  function buildRequestInit(options: JSONRequestOptions): RequestInit {
    const method = options.method ?? "GET";
    const headers = new Headers(options.headers ?? {});
    if (!headers.has("accept")) {
      headers.set("accept", "application/json");
    }
    if (!headers.has("accept-language") && config.getLocale) {
      headers.set("accept-language", config.getLocale());
    }

    const init: RequestInit = {
      method,
      headers,
    };

    applyDefaultHeaders(headers);

    if (options.body !== undefined) {
      if (options.body instanceof FormData) {
        init.body = options.body;
      } else {
        headers.set("Content-Type", "application/json");
        init.body = JSON.stringify(options.body);
      }
    }

    return init;
  }

  function applyDefaultHeaders(headers: Headers): void {
    applyAuthHeaders(headers);
    applyRequestSourceHeader(headers);
  }

  function applyAuthHeaders(headers: Headers): void {
    if (headers.has("x-api-key") || headers.has("authorization")) {
      return;
    }
    const apiKey = config.getApiKey();
    if (apiKey !== "") {
      headers.set("X-API-Key", apiKey);
    }
  }

  function applyRequestSourceHeader(headers: Headers): void {
    if (!headers.has(requestSourceHeader)) {
      headers.set(requestSourceHeader, requestSourceValue);
    }
  }

  async function readErrorMessage(response: Response): Promise<string> {
    const raw = await response.text();
    const fallback = response.statusText || `请求失败（${response.status}）`;
    return parseErrorMessage(raw, response.status, fallback);
  }

  return {
    applyDefaultHeaders,
    requestJSON,
    toAbsoluteURL,
  };
}
