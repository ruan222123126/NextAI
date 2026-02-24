type HttpMethod = "GET" | "POST" | "PUT" | "DELETE" | "PATCH";
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
export declare class ApiClientError extends Error {
    readonly status: number;
    readonly code: string;
    readonly details?: unknown;
    readonly payload: unknown;
    constructor(input: {
        status: number;
        code: string;
        message: string;
        details?: unknown;
        payload: unknown;
    });
}
export interface JSONRequestOptions {
    method?: HttpMethod;
    body?: unknown;
    headers?: HeadersInit;
    signal?: AbortSignal;
    accept?: string;
    parseErrorMessage?: (raw: string, status: number, fallback: string) => string;
}
export declare class ApiClient {
    private base;
    private apiKey;
    private readonly requestSourceHeader;
    private readonly requestSourceValue;
    private readonly getApiBase?;
    private readonly getApiKey?;
    private readonly getLocale?;
    constructor(input?: string | ApiClientInit);
    private resolveBaseURL;
    private resolveAPIKey;
    setBaseURL(base: string): void;
    getBaseURL(): string;
    setAPIKey(apiKey?: string): void;
    getAPIKey(): string;
    toAbsoluteURL(path: string): string;
    applyDefaultHeaders(headers: Headers): void;
    buildRequest(path: string, init?: RequestInit): {
        url: string;
        init: RequestInit;
    };
    request<T>(path: string, init?: RequestInit): Promise<T>;
    private requestWithOptions;
    requestJSON<T>(path: string, options?: JSONRequestOptions): Promise<T>;
    openStream(path: string, options?: JSONRequestOptions): Promise<Response>;
    get<T>(path: string, options?: Omit<JSONRequestOptions, "method" | "body">): Promise<T>;
    post<T>(path: string, body?: unknown, options?: Omit<JSONRequestOptions, "method" | "body">): Promise<T>;
    put<T>(path: string, body?: unknown, options?: Omit<JSONRequestOptions, "method" | "body">): Promise<T>;
    delete<T>(path: string, options?: Omit<JSONRequestOptions, "method" | "body">): Promise<T>;
    workspaceLs(): Promise<unknown>;
    workspaceCat(path: string): Promise<unknown>;
    workspacePut(path: string, payload: unknown): Promise<unknown>;
    workspaceRm(path: string): Promise<unknown>;
    workspaceExport(): Promise<unknown>;
    workspaceImport(payload: unknown): Promise<unknown>;
}
export {};
