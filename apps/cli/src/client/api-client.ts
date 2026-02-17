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

function parseResponsePayload(text: string): unknown {
  try {
    return text ? JSON.parse(text) : {};
  } catch {
    return { raw: text };
  }
}

function toClientError(status: number, payload: unknown, fallbackMessage: string): ApiClientError {
  const envelope = payload as ApiErrorEnvelope;
  const code = envelope?.error?.code ?? "http_error";
  const message = envelope?.error?.message ?? fallbackMessage;
  return new ApiClientError({
    status,
    code,
    message,
    details: envelope?.error?.details,
    payload,
  });
}

export class ApiClient {
  private readonly base: string;

  constructor(base?: string) {
    this.base = (base ?? process.env.COPAW_API_BASE ?? "http://127.0.0.1:8088").replace(/\/$/, "");
  }

  async request<T>(path: string, init?: RequestInit): Promise<T> {
    const res = await fetch(`${this.base}${path}`, {
      ...init,
      headers: {
        "content-type": "application/json",
        ...(init?.headers ?? {}),
      },
    });

    const text = await res.text();
    const data = parseResponsePayload(text);

    if (!res.ok) {
      throw toClientError(res.status, data, `${res.status} ${res.statusText}`.trim());
    }

    return data as T;
  }

  get<T>(path: string): Promise<T> {
    return this.request<T>(path);
  }

  post<T>(path: string, body?: unknown): Promise<T> {
    return this.request<T>(path, { method: "POST", body: body ? JSON.stringify(body) : undefined });
  }

  put<T>(path: string, body?: unknown): Promise<T> {
    return this.request<T>(path, { method: "PUT", body: body ? JSON.stringify(body) : undefined });
  }

  delete<T>(path: string): Promise<T> {
    return this.request<T>(path, { method: "DELETE" });
  }

  async uploadWorkspace(filePath: string): Promise<unknown> {
    const fs = await import("node:fs/promises");
    const form = new FormData();
    const data = await fs.readFile(filePath);
    const blob = new Blob([data], { type: "application/zip" });
    form.append("file", blob, "workspace.zip");
    const res = await fetch(`${this.base}/workspace/upload`, {
      method: "POST",
      body: form,
    });
    const text = await res.text();
    const json = parseResponsePayload(text);
    if (!res.ok) {
      throw toClientError(res.status, json, `${res.status} ${res.statusText}`.trim());
    }
    return json;
  }
}
