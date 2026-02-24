type HttpMethod = "GET" | "POST" | "PUT" | "DELETE" | "PATCH";

const DEFAULT_BASE_URL = "http://127.0.0.1:8088";
const DEFAULT_REQUEST_SOURCE_HEADER = "X-NextAI-Source";
const DEFAULT_REQUEST_SOURCE_VALUE = "sdk-ts";

export interface ApiClientInit {
  base?: string;
  apiKey?: string;
  getApiBase?: () => string;
  getApiKey?: () => string;
  getLocale?: () => string;
  requestSourceHeader?: string;
  requestSourceValue?: string;
}

export interface ApiErrorEnvelope {
  error?: {
    code?: string;
    message?: string;
    details?: unknown;
  };
  [k: string]: unknown;
}

export class ApiClientError extends Error {
  readonly status: number;
  readonly code: string;
  readonly details?: unknown;
  readonly payload: unknown;

  constructor(input: { status: number; code: string; message: string; details?: unknown; payload: unknown }) {
    super(input.message);
    this.name = "ApiClientError";
    this.status = input.status;
    this.code = input.code;
    this.details = input.details;
    this.payload = input.payload;
  }
}

export interface JSONRequestOptions {
  method?: HttpMethod;
  body?: unknown;
  headers?: HeadersInit;
  signal?: AbortSignal;
  accept?: string;
  parseErrorMessage?: (raw: string, status: number, fallback: string) => string;
}

function parseResponsePayload(text: string): unknown {
  try {
    return text ? JSON.parse(text) : {};
  } catch {
    return { raw: text };
  }
}

function resolveAPIErrorEnvelope(payload: unknown): ApiErrorEnvelope {
  if (typeof payload !== "object" || payload === null) {
    return {};
  }
  return payload as ApiErrorEnvelope;
}

function toClientError(status: number, payload: unknown, fallbackMessage: string): ApiClientError {
  const envelope = resolveAPIErrorEnvelope(payload);
  const code = envelope?.error?.code ?? "http_error";
  const message = envelope?.error?.message ?? fallbackMessage;
  const details = envelope?.error?.details;
  return new ApiClientError({
    status,
    code,
    message,
    details,
    payload,
  });
}

function isBodyInitLike(value: unknown): value is BodyInit {
  return (
    typeof value === "string"
    || value instanceof Blob
    || value instanceof FormData
    || value instanceof URLSearchParams
    || value instanceof ReadableStream
    || value instanceof ArrayBuffer
    || ArrayBuffer.isView(value)
  );
}

function fallbackStatusMessage(response: Response): string {
  const message = `${response.status} ${response.statusText}`.trim();
  if (message !== "") {
    return message;
  }
  return `request failed (${response.status})`;
}

export class ApiClient {
  private base: string;
  private apiKey: string;
  private readonly requestSourceHeader: string;
  private readonly requestSourceValue: string;
  private readonly getApiBase?: () => string;
  private readonly getApiKey?: () => string;
  private readonly getLocale?: () => string;

  constructor(input?: string | ApiClientInit) {
    const init = typeof input === "string" ? { base: input } : input;
    const env = (globalThis as { process?: { env?: Record<string, string | undefined> } }).process?.env;
    this.base = (init?.base ?? env?.NEXTAI_API_BASE ?? DEFAULT_BASE_URL).trim();
    this.apiKey = (init?.apiKey ?? env?.NEXTAI_API_KEY ?? "").trim();
    this.requestSourceHeader = init?.requestSourceHeader ?? DEFAULT_REQUEST_SOURCE_HEADER;
    this.requestSourceValue = init?.requestSourceValue ?? DEFAULT_REQUEST_SOURCE_VALUE;
    this.getApiBase = init?.getApiBase;
    this.getApiKey = init?.getApiKey;
    this.getLocale = init?.getLocale;
  }

  private resolveBaseURL(): string {
    const dynamicBase = this.getApiBase?.().trim();
    const resolved = dynamicBase && dynamicBase !== "" ? dynamicBase : this.base;
    return resolved.replace(/\/+$/g, "");
  }

  private resolveAPIKey(): string {
    const dynamicKey = this.getApiKey?.().trim();
    return dynamicKey && dynamicKey !== "" ? dynamicKey : this.apiKey;
  }

  setBaseURL(base: string): void {
    const normalized = base.trim();
    if (normalized !== "") {
      this.base = normalized;
    }
  }

  getBaseURL(): string {
    return this.resolveBaseURL();
  }

  setAPIKey(apiKey?: string): void {
    this.apiKey = (apiKey ?? "").trim();
  }

  getAPIKey(): string {
    return this.resolveAPIKey();
  }

  toAbsoluteURL(path: string): string {
    return `${this.resolveBaseURL()}${path}`;
  }

  applyDefaultHeaders(headers: Headers): void {
    if (!headers.has("accept-language") && this.getLocale) {
      headers.set("accept-language", this.getLocale());
    }

    if (!headers.has("x-api-key") && !headers.has("authorization")) {
      const apiKey = this.resolveAPIKey();
      if (apiKey !== "") {
        headers.set("X-API-Key", apiKey);
      }
    }

    if (!headers.has(this.requestSourceHeader)) {
      headers.set(this.requestSourceHeader, this.requestSourceValue);
    }
  }

  buildRequest(path: string, init?: RequestInit): { url: string; init: RequestInit } {
    const method = (init?.method ?? "GET").toUpperCase();
    const headers = new Headers(init?.headers ?? {});
    if (!headers.has("accept")) {
      headers.set("accept", "application/json");
    }
    this.applyDefaultHeaders(headers);

    if (init?.body !== undefined && init?.body !== null && !(init.body instanceof FormData) && !headers.has("content-type")) {
      headers.set("content-type", "application/json");
    }

    return {
      url: this.toAbsoluteURL(path),
      init: {
        ...init,
        method,
        headers,
      },
    };
  }

  async request<T>(path: string, init?: RequestInit): Promise<T> {
    const request = this.buildRequest(path, init);
    const response = await fetch(request.url, request.init);
    const text = await response.text();
    const payload = parseResponsePayload(text);

    if (!response.ok) {
      throw toClientError(response.status, payload, fallbackStatusMessage(response));
    }

    return payload as T;
  }

  private async requestWithOptions(
    path: string,
    options: JSONRequestOptions,
    defaults?: { accept?: string },
  ): Promise<Response> {
    const method = options.method ?? "GET";
    const headers = new Headers(options.headers ?? {});
    const accept = options.accept ?? defaults?.accept;
    if (accept && !headers.has("accept")) {
      headers.set("accept", accept);
    }
    this.applyDefaultHeaders(headers);

    const init: RequestInit = {
      method,
      headers,
      signal: options.signal,
    };

    if (options.body !== undefined) {
      if (options.body instanceof FormData) {
        init.body = options.body;
      } else if (isBodyInitLike(options.body)) {
        init.body = options.body;
      } else {
        if (!headers.has("content-type")) {
          headers.set("content-type", "application/json");
        }
        init.body = JSON.stringify(options.body);
      }
    }

    const response = await fetch(this.toAbsoluteURL(path), init);
    if (response.ok) {
      return response;
    }

    const raw = await response.text();
    const fallback = fallbackStatusMessage(response);
    if (options.parseErrorMessage) {
      throw new Error(options.parseErrorMessage(raw, response.status, fallback));
    }
    throw toClientError(response.status, parseResponsePayload(raw), fallback);
  }

  async requestJSON<T>(path: string, options: JSONRequestOptions = {}): Promise<T> {
    const response = await this.requestWithOptions(path, options, { accept: options.accept ?? "application/json" });
    if (response.status === 204) {
      return undefined as T;
    }

    const raw = await response.text();
    if (raw.trim() === "") {
      return undefined as T;
    }

    const payload = parseResponsePayload(raw);
    return payload as T;
  }

  async openStream(path: string, options: JSONRequestOptions = {}): Promise<Response> {
    const response = await this.requestWithOptions(path, options, { accept: options.accept ?? "text/event-stream,application/json" });
    if (!response.body) {
      throw new Error("stream unsupported");
    }
    return response;
  }

  get<T>(path: string, options?: Omit<JSONRequestOptions, "method" | "body">): Promise<T> {
    return this.requestJSON<T>(path, { ...options, method: "GET" });
  }

  post<T>(path: string, body?: unknown, options?: Omit<JSONRequestOptions, "method" | "body">): Promise<T> {
    return this.requestJSON<T>(path, { ...options, method: "POST", body });
  }

  put<T>(path: string, body?: unknown, options?: Omit<JSONRequestOptions, "method" | "body">): Promise<T> {
    return this.requestJSON<T>(path, { ...options, method: "PUT", body });
  }

  delete<T>(path: string, options?: Omit<JSONRequestOptions, "method" | "body">): Promise<T> {
    return this.requestJSON<T>(path, { ...options, method: "DELETE" });
  }

  workspaceLs(): Promise<unknown> {
    return this.get("/workspace/files");
  }

  workspaceCat(path: string): Promise<unknown> {
    return this.get(`/workspace/files/${encodeURIComponent(path)}`);
  }

  workspacePut(path: string, payload: unknown): Promise<unknown> {
    return this.put(`/workspace/files/${encodeURIComponent(path)}`, payload);
  }

  workspaceRm(path: string): Promise<unknown> {
    return this.delete(`/workspace/files/${encodeURIComponent(path)}`);
  }

  workspaceExport(): Promise<unknown> {
    return this.get("/workspace/export");
  }

  workspaceImport(payload: unknown): Promise<unknown> {
    return this.post("/workspace/import", payload);
  }
}
