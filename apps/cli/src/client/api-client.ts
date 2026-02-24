import {
  ApiClient as SDKApiClient,
  ApiClientError,
  type ApiClientInit as SDKApiClientInit,
  type JSONRequestOptions,
} from "@nextai/sdk-ts";

const REQUEST_SOURCE_CLI = "cli";

export interface ApiClientInit extends Omit<SDKApiClientInit, "requestSourceHeader" | "requestSourceValue"> {}

export class ApiClient extends SDKApiClient {
  constructor(input?: string | ApiClientInit) {
    const init = typeof input === "string" ? { base: input } : input;
    super({
      ...init,
      requestSourceValue: REQUEST_SOURCE_CLI,
    });
  }
}

export { ApiClientError, type JSONRequestOptions };
