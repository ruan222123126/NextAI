const DEFAULT_BASE_URL = "http://127.0.0.1:8088";
const DEFAULT_REQUEST_SOURCE_HEADER = "X-NextAI-Source";
const DEFAULT_REQUEST_SOURCE_VALUE = "sdk-ts";
export class ApiClientError extends Error {
    status;
    code;
    details;
    payload;
    constructor(input) {
        super(input.message);
        this.name = "ApiClientError";
        this.status = input.status;
        this.code = input.code;
        this.details = input.details;
        this.payload = input.payload;
    }
}
function parseResponsePayload(text) {
    try {
        return text ? JSON.parse(text) : {};
    }
    catch {
        return { raw: text };
    }
}
function resolveAPIErrorEnvelope(payload) {
    if (typeof payload !== "object" || payload === null) {
        return {};
    }
    return payload;
}
function toClientError(status, payload, fallbackMessage) {
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
function isBodyInitLike(value) {
    return (typeof value === "string"
        || value instanceof Blob
        || value instanceof FormData
        || value instanceof URLSearchParams
        || value instanceof ReadableStream
        || value instanceof ArrayBuffer
        || ArrayBuffer.isView(value));
}
function fallbackStatusMessage(response) {
    const message = `${response.status} ${response.statusText}`.trim();
    if (message !== "") {
        return message;
    }
    return `request failed (${response.status})`;
}
export class ApiClient {
    base;
    apiKey;
    requestSourceHeader;
    requestSourceValue;
    getApiBase;
    getApiKey;
    getLocale;
    constructor(input) {
        const init = typeof input === "string" ? { base: input } : input;
        const env = globalThis.process?.env;
        this.base = (init?.base ?? env?.NEXTAI_API_BASE ?? DEFAULT_BASE_URL).trim();
        this.apiKey = (init?.apiKey ?? env?.NEXTAI_API_KEY ?? "").trim();
        this.requestSourceHeader = init?.requestSourceHeader ?? DEFAULT_REQUEST_SOURCE_HEADER;
        this.requestSourceValue = init?.requestSourceValue ?? DEFAULT_REQUEST_SOURCE_VALUE;
        this.getApiBase = init?.getApiBase;
        this.getApiKey = init?.getApiKey;
        this.getLocale = init?.getLocale;
    }
    resolveBaseURL() {
        const dynamicBase = this.getApiBase?.().trim();
        const resolved = dynamicBase && dynamicBase !== "" ? dynamicBase : this.base;
        return resolved.replace(/\/+$/g, "");
    }
    resolveAPIKey() {
        const dynamicKey = this.getApiKey?.().trim();
        return dynamicKey && dynamicKey !== "" ? dynamicKey : this.apiKey;
    }
    setBaseURL(base) {
        const normalized = base.trim();
        if (normalized !== "") {
            this.base = normalized;
        }
    }
    getBaseURL() {
        return this.resolveBaseURL();
    }
    setAPIKey(apiKey) {
        this.apiKey = (apiKey ?? "").trim();
    }
    getAPIKey() {
        return this.resolveAPIKey();
    }
    toAbsoluteURL(path) {
        return `${this.resolveBaseURL()}${path}`;
    }
    applyDefaultHeaders(headers) {
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
    buildRequest(path, init) {
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
    async request(path, init) {
        const request = this.buildRequest(path, init);
        const response = await fetch(request.url, request.init);
        const text = await response.text();
        const payload = parseResponsePayload(text);
        if (!response.ok) {
            throw toClientError(response.status, payload, fallbackStatusMessage(response));
        }
        return payload;
    }
    async requestWithOptions(path, options, defaults) {
        const method = options.method ?? "GET";
        const headers = new Headers(options.headers ?? {});
        const accept = options.accept ?? defaults?.accept;
        if (accept && !headers.has("accept")) {
            headers.set("accept", accept);
        }
        this.applyDefaultHeaders(headers);
        const init = {
            method,
            headers,
            signal: options.signal,
        };
        if (options.body !== undefined) {
            if (options.body instanceof FormData) {
                init.body = options.body;
            }
            else if (isBodyInitLike(options.body)) {
                init.body = options.body;
            }
            else {
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
    async requestJSON(path, options = {}) {
        const response = await this.requestWithOptions(path, options, { accept: options.accept ?? "application/json" });
        if (response.status === 204) {
            return undefined;
        }
        const raw = await response.text();
        if (raw.trim() === "") {
            return undefined;
        }
        const payload = parseResponsePayload(raw);
        return payload;
    }
    async openStream(path, options = {}) {
        const response = await this.requestWithOptions(path, options, { accept: options.accept ?? "text/event-stream,application/json" });
        if (!response.body) {
            throw new Error("stream unsupported");
        }
        return response;
    }
    get(path, options) {
        return this.requestJSON(path, { ...options, method: "GET" });
    }
    post(path, body, options) {
        return this.requestJSON(path, { ...options, method: "POST", body });
    }
    put(path, body, options) {
        return this.requestJSON(path, { ...options, method: "PUT", body });
    }
    delete(path, options) {
        return this.requestJSON(path, { ...options, method: "DELETE" });
    }
    workspaceLs() {
        return this.get("/workspace/files");
    }
    workspaceCat(path) {
        return this.get(`/workspace/files/${encodeURIComponent(path)}`);
    }
    workspacePut(path, payload) {
        return this.put(`/workspace/files/${encodeURIComponent(path)}`, payload);
    }
    workspaceRm(path) {
        return this.delete(`/workspace/files/${encodeURIComponent(path)}`);
    }
    workspaceExport() {
        return this.get("/workspace/export");
    }
    workspaceImport(payload) {
        return this.post("/workspace/import", payload);
    }
}
