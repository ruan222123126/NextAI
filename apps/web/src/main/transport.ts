import { ApiClient, type JSONRequestOptions as SDKJSONRequestOptions } from "@nextai/sdk-ts";
import { parseErrorMessage } from "../api-utils.js";

type HttpMethod = "GET" | "POST" | "PUT" | "DELETE";

export interface JSONRequestOptions {
  method?: HttpMethod;
  body?: unknown;
  headers?: Record<string, string>;
  signal?: AbortSignal;
  accept?: string;
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
  openStream(path: string, options?: JSONRequestOptions): Promise<Response>;
  toAbsoluteURL(path: string): string;
}

export function createTransport(config: TransportConfig): TransportClient {
  const client = new ApiClient({
    getApiBase: config.getApiBase,
    getApiKey: config.getApiKey,
    getLocale: config.getLocale,
    requestSourceHeader: config.requestSourceHeader ?? "X-NextAI-Source",
    requestSourceValue: config.requestSourceValue ?? "web",
  });

  function toSDKOptions(options: JSONRequestOptions): SDKJSONRequestOptions {
    return {
      method: options.method,
      body: options.body,
      headers: options.headers,
      signal: options.signal,
      accept: options.accept,
      parseErrorMessage: (raw, status, fallback) => parseErrorMessage(raw, status, fallback),
    };
  }

  async function requestJSON<T>(path: string, options: JSONRequestOptions = {}): Promise<T> {
    return client.requestJSON<T>(path, toSDKOptions(options));
  }

  async function openStream(path: string, options: JSONRequestOptions = {}): Promise<Response> {
    return client.openStream(path, toSDKOptions(options));
  }

  return {
    applyDefaultHeaders: (headers: Headers) => {
      client.applyDefaultHeaders(headers);
    },
    requestJSON,
    openStream,
    toAbsoluteURL: (path: string) => client.toAbsoluteURL(path),
  };
}
