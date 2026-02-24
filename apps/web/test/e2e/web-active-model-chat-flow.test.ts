// @vitest-environment jsdom

import { readFileSync } from "node:fs";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      "content-type": "application/json",
    },
  });
}

async function waitFor(condition: () => boolean, timeoutMS = 2000): Promise<void> {
  const startedAt = Date.now();
  while (!condition()) {
    if (Date.now() - startedAt > timeoutMS) {
      throw new Error("timeout waiting for condition");
    }
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
}

async function waitForComposerSelectReady(selectID: string): Promise<HTMLSelectElement> {
  await waitFor(() => document.body.getAttribute("data-nextai-boot-ready") === "true");
  await waitFor(() => {
    const select = document.getElementById(selectID);
    return select instanceof HTMLSelectElement && !select.disabled && select.options.length > 0;
  });
  return document.getElementById(selectID) as HTMLSelectElement;
}

function rect(top: number, bottom: number, left = 0, right = 0): DOMRect {
  return {
    x: left,
    y: top,
    width: Math.max(0, right - left),
    height: Math.max(0, bottom - top),
    top,
    right,
    bottom,
    left,
    toJSON: () => ({}),
  } as DOMRect;
}

describe("web e2e: auto activate model then send chat", () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    vi.resetModules();
    document.documentElement.innerHTML = readFileSync(join(process.cwd(), "src/index.html"), "utf8").replace(
      /<!doctype html>/i,
      "",
    );
    window.localStorage.clear();
    window.localStorage.setItem("nextai.web.locale", "zh-CN");
    originalFetch = globalThis.fetch;
  });

  afterEach(() => {
    vi.restoreAllMocks();
    globalThis.fetch = originalFetch;
    delete (window as typeof window & { __NEXTAI_STREAM_RETRY_DELAY_MS__?: number }).__NEXTAI_STREAM_RETRY_DELAY_MS__;
    document.documentElement.innerHTML = "<head></head><body></body>";
  });

  it("auto activates model and sends chat without Echo fallback", async () => {
    const replies = {
      model: "杩欐槸妯″瀷鍥炲锛屼笉鏄?Echo",
    };
    const providerID = "openai-compatible";
    const modelID = "ark-code-latest";

    let processRequestSessionID = "";
    let processRequestUserID = "";
    let processRequestChannel = "";
    let processCalled = false;
    let activeSetCalled = false;

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/chats" && method === "GET") {
        if (!processCalled) {
          return jsonResponse([]);
        }
        return jsonResponse([
          {
            id: "chat-e2e-1",
            name: "浣犲ソ",
            session_id: processRequestSessionID,
            user_id: processRequestUserID,
            channel: processRequestChannel,
            created_at: "2026-02-16T06:00:00Z",
            updated_at: "2026-02-16T06:00:10Z",
            meta: {},
          },
        ]);
      }

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [
            {
              id: providerID,
              name: "OPENAI-COMPATIBLE",
              display_name: "ruan",
              openai_compatible: true,
              api_key_prefix: "OPENAI_COMPATIBLE_API_KEY",
              models: [{ id: modelID, name: modelID }],
              allow_custom_base_url: true,
              enabled: true,
              has_api_key: true,
              current_api_key: "skm***123",
              current_base_url: "https://example.com/v1",
            },
          ],
          provider_types: [
            { id: "openai", display_name: "openai" },
            { id: "openai-compatible", display_name: "openai Compatible" },
          ],
          defaults: {
            [providerID]: modelID,
          },
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/models/active" && method === "PUT") {
        activeSetCalled = true;
        const payload = JSON.parse(String(init?.body ?? "{}")) as { provider_id?: string; model?: string };
        expect(payload.provider_id).toBe(providerID);
        expect(payload.model).toBe(modelID);
        return jsonResponse({
          active_llm: {
            provider_id: providerID,
            model: modelID,
          },
        });
      }

      if (url.pathname === "/agent/process" && method === "POST") {
        processCalled = true;
        const payload = JSON.parse(String(init?.body ?? "{}")) as {
          session_id?: string;
          user_id?: string;
          channel?: string;
        };
        processRequestSessionID = payload.session_id ?? "";
        processRequestUserID = payload.user_id ?? "";
        processRequestChannel = payload.channel ?? "";

        const sse = [
          `data: ${JSON.stringify({ type: "step_started", step: 1 })}`,
          `data: ${JSON.stringify({ type: "assistant_delta", step: 1, delta: replies.model })}`,
          `data: ${JSON.stringify({ type: "completed", step: 1, reply: replies.model })}`,
          "data: [DONE]",
          "",
        ].join("\n\n");
        return new Response(sse, {
          status: 200,
          headers: {
            "content-type": "text/event-stream",
          },
        });
      }

      if (url.pathname === "/chats/chat-e2e-1" && method === "GET") {
        return jsonResponse({
          messages: [
            {
              id: "msg-user",
              role: "user",
              type: "message",
              content: [{ type: "text", text: "浣犲ソ" }],
            },
            {
              id: "msg-assistant",
              role: "assistant",
              type: "message",
              content: [{ type: "text", text: replies.model }],
            },
          ],
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");
    await waitFor(() => activeSetCalled);

    const settingsToggleButton = document.getElementById("settings-toggle") as HTMLButtonElement;
    settingsToggleButton.click();
    const modelsSectionButton = document.querySelector<HTMLButtonElement>('button[data-settings-section="models"]');
    expect(modelsSectionButton).not.toBeNull();
    modelsSectionButton?.click();

    await waitFor(() =>
      Boolean(
        document.querySelector<HTMLButtonElement>(
          `button[data-provider-action="select"][data-provider-id="${providerID}"]`,
        ),
      ),
    );

    const providerDeleteButton = document.querySelector<HTMLButtonElement>(
      `button[data-provider-action="delete"][data-provider-id="${providerID}"]`,
    );
    expect(providerDeleteButton).not.toBeNull();
    expect(providerDeleteButton?.classList.contains("chat-delete-btn")).toBe(true);
    expect(providerDeleteButton?.querySelector("svg")).not.toBeNull();
    expect(providerDeleteButton?.getAttribute("aria-label")).toBe("删除提供商");

    const providerCardButton = document.querySelector<HTMLButtonElement>(
      `button[data-provider-action="select"][data-provider-id="${providerID}"]`,
    );
    expect(providerCardButton).not.toBeNull();
    const providerMetaLines = Array.from(providerCardButton?.querySelectorAll<HTMLElement>(".models-provider-card-meta") ?? []).map(
      (item) => item.textContent ?? "",
    );
    expect(providerMetaLines.some((line) => line.includes("openai Compatible"))).toBe(true);
    providerCardButton?.click();

    await waitFor(() => {
      const level2 = document.getElementById("models-level2-view");
      return Boolean(level2 && !level2.hasAttribute("hidden"));
    });

    const chatTabButton = document.querySelector<HTMLButtonElement>('button[data-tab="chat"]');
    expect(chatTabButton).not.toBeNull();
    chatTabButton?.click();

    const messageInput = document.getElementById("message-input") as HTMLTextAreaElement;
    const composerForm = document.getElementById("composer") as HTMLFormElement;
    messageInput.value = "浣犲ソ";
    composerForm.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => {
      const messages = Array.from(document.querySelectorAll<HTMLLIElement>("#message-list .message.assistant"));
      return messages.some((item) => item.textContent?.includes(replies.model));
    }, 4000);

    const assistantMessages = Array.from(document.querySelectorAll<HTMLLIElement>("#message-list .message.assistant"));
    expect(assistantMessages.length).toBeGreaterThan(0);
    const text = assistantMessages[assistantMessages.length - 1]?.textContent ?? "";
    expect(text).toContain(replies.model);
    expect(text).not.toContain("Echo:");
    expect(activeSetCalled).toBe(true);
    expect(processCalled).toBe(true);
  });

  it("shows request error in assistant bubble instead of ellipsis when request fails", async () => {
    const errorMessage = "failed to connect gateway";
    let processCalled = false;

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/agent/process" && method === "POST") {
        processCalled = true;
        throw new Error(errorMessage);
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const messageInput = document.getElementById("message-input") as HTMLTextAreaElement;
    const composerForm = document.getElementById("composer") as HTMLFormElement;
    messageInput.value = "hello";
    composerForm.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => processCalled, 4000);
    await waitFor(() => {
      const assistant = document.querySelector<HTMLLIElement>("#message-list .message.assistant:last-child");
      if (!assistant) {
        return false;
      }
      const text = assistant.textContent?.trim() ?? "";
      return text.includes(errorMessage);
    }, 4000);

    const assistant = document.querySelector<HTMLLIElement>("#message-list .message.assistant:last-child");
    const text = assistant?.textContent?.trim() ?? "";
    expect(text).toContain(errorMessage);
    expect(text).not.toBe("...");
  });

  it("stream 连接中断后会自动重试并最终成功", async () => {
    const replyText = "retry succeeded";
    let processCallCount = 0;
    (window as typeof window & { __NEXTAI_STREAM_RETRY_DELAY_MS__?: number }).__NEXTAI_STREAM_RETRY_DELAY_MS__ = 1;

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/agent/process" && method === "POST") {
        processCallCount += 1;
        if (processCallCount === 1) {
          const brokenStream = new ReadableStream<Uint8Array>({
            start(controller) {
              controller.error(new TypeError("net::ERR_INCOMPLETE_CHUNKED_ENCODING"));
            },
          });
          return new Response(brokenStream, {
            status: 200,
            headers: {
              "content-type": "text/event-stream",
            },
          });
        }

        const sse = [
          `data: ${JSON.stringify({ type: "step_started", step: 1 })}`,
          `data: ${JSON.stringify({ type: "assistant_delta", step: 1, delta: replyText })}`,
          `data: ${JSON.stringify({ type: "completed", step: 1, reply: replyText })}`,
          "data: [DONE]",
          "",
        ].join("\n\n");
        return new Response(sse, {
          status: 200,
          headers: {
            "content-type": "text/event-stream",
          },
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const messageInput = document.getElementById("message-input") as HTMLTextAreaElement;
    const composerForm = document.getElementById("composer") as HTMLFormElement;
    messageInput.value = "retry me";
    composerForm.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => processCallCount === 2, 4000);
    await waitFor(() => {
      const assistant = document.querySelector<HTMLLIElement>("#message-list .message.assistant:last-child");
      return (assistant?.textContent ?? "").includes(replyText);
    }, 4000);

    const assistant = document.querySelector<HTMLLIElement>("#message-list .message.assistant:last-child");
    const text = assistant?.textContent ?? "";
    const statusLine = document.getElementById("status-line")?.textContent ?? "";
    expect(text).toContain(replyText);
    expect(processCallCount).toBe(2);
    expect(statusLine).toContain("回复接收完成");
  });

  it("发送中点击发送按钮会暂停请求并恢复按钮状态", async () => {
    let processCalled = false;
    let aborted = false;

    globalThis.fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/chats" && method === "GET") {
        return Promise.resolve(jsonResponse([]));
      }

      if (url.pathname === "/models/catalog" && method === "GET") {
        return Promise.resolve(jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        }));
      }

      if (url.pathname === "/agent/process" && method === "POST") {
        processCalled = true;
        const signal = init?.signal as AbortSignal | null | undefined;
        return new Promise<Response>((_resolve, reject) => {
          if (signal?.aborted) {
            aborted = true;
            reject(new DOMException("aborted", "AbortError"));
            return;
          }
          signal?.addEventListener(
            "abort",
            () => {
              aborted = true;
              reject(new DOMException("aborted", "AbortError"));
            },
            { once: true },
          );
        });
      }

      return Promise.reject(new Error(`unexpected request: ${method} ${url.pathname}`));
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const messageInput = document.getElementById("message-input") as HTMLTextAreaElement;
    const composerForm = document.getElementById("composer") as HTMLFormElement;
    const sendButton = document.getElementById("send-btn") as HTMLButtonElement;
    messageInput.value = "pause me";
    composerForm.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => processCalled && sendButton.classList.contains("is-sending"), 4000);
    expect(sendButton.getAttribute("aria-label")).toBe("暂停请求");

    sendButton.click();

    await waitFor(() => aborted, 4000);
    await waitFor(() => !sendButton.classList.contains("is-sending"), 4000);
    const statusLine = document.getElementById("status-line")?.textContent ?? "";
    expect(statusLine).toContain("已暂停回复接收");
    expect(sendButton.getAttribute("aria-label")).toBe("发送消息");
  });

  it("协作模式选择器在 codex 下可用，其他 prompt_mode 下禁用", async () => {
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const promptModeSelect = document.getElementById("chat-prompt-mode-select") as HTMLSelectElement;
    const collaborationModeSelect = document.getElementById("chat-collaboration-mode-select") as HTMLSelectElement;
    expect(promptModeSelect.value).toBe("default");
    expect(collaborationModeSelect.disabled).toBe(true);

    promptModeSelect.value = "codex";
    promptModeSelect.dispatchEvent(new Event("change", { bubbles: true }));
    await waitFor(() => collaborationModeSelect.disabled === false, 4000);

    promptModeSelect.value = "claude";
    promptModeSelect.dispatchEvent(new Event("change", { bubbles: true }));
    await waitFor(() => collaborationModeSelect.disabled === true, 4000);
  });

  it("switches active model from composer toolbar provider and model selectors", async () => {
    const activeSetPayloads: Array<{ provider_id?: string; model?: string }> = [];
    let activeLLM = {
      provider_id: "openai",
      model: "gpt-4o-mini",
    };

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [
            {
              id: "openai",
              name: "OPENAI",
              display_name: "OpenAI",
              openai_compatible: true,
              api_key_prefix: "OPENAI_API_KEY",
              models: [{ id: "gpt-4o-mini", name: "gpt-4o-mini" }],
              allow_custom_base_url: true,
              enabled: true,
              has_api_key: true,
              current_api_key: "sk-***",
              current_base_url: "https://api.openai.com/v1",
            },
            {
              id: "openai-compatible",
              name: "OPENAI-COMPATIBLE",
              display_name: "ruan",
              openai_compatible: true,
              api_key_prefix: "OPENAI_COMPATIBLE_API_KEY",
              models: [
                { id: "ark-code-latest", name: "ark-code-latest" },
                { id: "deepseek-chat", name: "deepseek-chat" },
              ],
              allow_custom_base_url: true,
              enabled: true,
              has_api_key: true,
              current_api_key: "skm***123",
              current_base_url: "https://example.com/v1",
            },
          ],
          provider_types: [
            { id: "openai", display_name: "openai" },
            { id: "openai-compatible", display_name: "openai Compatible" },
          ],
          defaults: {
            openai: "gpt-4o-mini",
            "openai-compatible": "ark-code-latest",
          },
          active_llm: activeLLM,
        });
      }

      if (url.pathname === "/models/active" && method === "PUT") {
        const payload = JSON.parse(String(init?.body ?? "{}")) as { provider_id?: string; model?: string };
        activeSetPayloads.push(payload);
        activeLLM = {
          provider_id: payload.provider_id ?? "",
          model: payload.model ?? "",
        };
        return jsonResponse({
          active_llm: activeLLM,
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    await waitFor(() => {
      const providerSelect = document.getElementById("composer-provider-select") as HTMLSelectElement | null;
      return Boolean(providerSelect && providerSelect.options.length >= 2);
    });

    const providerSelect = document.getElementById("composer-provider-select") as HTMLSelectElement;
    const modelSelect = document.getElementById("composer-model-select") as HTMLSelectElement;

    expect(providerSelect.value).toBe("openai");
    expect(modelSelect.value).toBe("gpt-4o-mini");

    providerSelect.value = "openai-compatible";
    providerSelect.dispatchEvent(new Event("change", { bubbles: true }));

    await waitFor(() => activeSetPayloads.length >= 1);
    expect(activeSetPayloads[0]).toEqual({
      provider_id: "openai-compatible",
      model: "ark-code-latest",
    });
    expect(providerSelect.value).toBe("openai-compatible");
    expect(modelSelect.value).toBe("ark-code-latest");

    modelSelect.value = "deepseek-chat";
    modelSelect.dispatchEvent(new Event("change", { bubbles: true }));

    await waitFor(() => activeSetPayloads.length >= 2);
    expect(activeSetPayloads[1]).toEqual({
      provider_id: "openai-compatible",
      model: "deepseek-chat",
    });
  });

  it("shows used and total context tokens with active model context limit", async () => {
    const agentsPath = "prompts/AGENTS.md";
    const aiToolsPath = "prompts/ai-tools.md";
    const agentsPathEncoded = encodeURIComponent(agentsPath);
    const aiToolsPathEncoded = encodeURIComponent(aiToolsPath);
    const agentsContent = "aaaa ".repeat(100).trim();
    const aiToolsContent = "bbbb ".repeat(50).trim();
    const historyUserText = "cccc ".repeat(120).trim();
    const historyAssistantText = "dddd ".repeat(80).trim();
    const draftText = "eeee ".repeat(150).trim();
    let chatHistoryLoaded = false;

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([
          {
            id: "chat-context-1",
            name: "context",
            session_id: "session-context-1",
            user_id: "demo-user",
            channel: "console",
            created_at: "2026-02-20T10:00:00Z",
            updated_at: "2026-02-20T10:00:01Z",
            meta: {},
          },
        ]);
      }

      if (url.pathname === "/chats/chat-context-1" && method === "GET") {
        chatHistoryLoaded = true;
        return jsonResponse({
          messages: [
            {
              id: "msg-user-1",
              role: "user",
              type: "message",
              content: [{ type: "text", text: historyUserText }],
            },
            {
              id: "msg-assistant-1",
              role: "assistant",
              type: "message",
              content: [{ type: "text", text: historyAssistantText }],
            },
          ],
        });
      }

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [
            {
              id: "openai",
              name: "OPENAI",
              display_name: "OpenAI",
              openai_compatible: true,
              api_key_prefix: "OPENAI_API_KEY",
              models: [{ id: "gpt-4o-mini", name: "gpt-4o-mini", limit: { context: 32000 } }],
              allow_custom_base_url: true,
              enabled: true,
              has_api_key: true,
              current_api_key: "sk-***",
              current_base_url: "https://api.openai.com/v1",
            },
          ],
          provider_types: [{ id: "openai", display_name: "openai" }],
          defaults: {
            openai: "gpt-4o-mini",
          },
          active_llm: {
            provider_id: "openai",
            model: "gpt-4o-mini",
          },
        });
      }

      if (url.pathname === `/workspace/files/${agentsPathEncoded}` && method === "GET") {
        return jsonResponse({ content: agentsContent });
      }

      if (url.pathname === `/workspace/files/${aiToolsPathEncoded}` && method === "GET") {
        return jsonResponse({ content: aiToolsContent });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");
    await waitFor(() => chatHistoryLoaded);

    const messageInput = document.getElementById("message-input") as HTMLTextAreaElement;
    messageInput.value = draftText;
    messageInput.dispatchEvent(new Event("input", { bubbles: true }));

    await waitFor(() => {
      const text = document.getElementById("composer-token-estimate")?.textContent ?? "";
      return text.includes("0.5k/32.0k");
    });
  });

  it("falls back to 128.0k when model context limit is missing", async () => {
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [
            {
              id: "openai",
              name: "OPENAI",
              display_name: "OpenAI",
              openai_compatible: true,
              api_key_prefix: "OPENAI_API_KEY",
              models: [{ id: "gpt-4o-mini", name: "gpt-4o-mini" }],
              allow_custom_base_url: true,
              enabled: true,
              has_api_key: true,
              current_api_key: "sk-***",
              current_base_url: "https://api.openai.com/v1",
            },
          ],
          provider_types: [{ id: "openai", display_name: "openai" }],
          defaults: {
            openai: "gpt-4o-mini",
          },
          active_llm: {
            provider_id: "openai",
            model: "gpt-4o-mini",
          },
        });
      }

      if (url.pathname.startsWith("/workspace/files/") && method === "GET") {
        return jsonResponse({ error: { code: "not_found", message: "not found" } }, 404);
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    await waitFor(() => {
      const text = document.getElementById("composer-token-estimate")?.textContent ?? "";
      return text.includes("/128.0k");
    });
  });

  it("updates context limit denominator after switching active model", async () => {
    let activeLLM = {
      provider_id: "openai",
      model: "gpt-4o-mini",
    };

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [
            {
              id: "openai",
              name: "OPENAI",
              display_name: "OpenAI",
              openai_compatible: true,
              api_key_prefix: "OPENAI_API_KEY",
              models: [{ id: "gpt-4o-mini", name: "gpt-4o-mini", limit: { context: 32000 } }],
              allow_custom_base_url: true,
              enabled: true,
              has_api_key: true,
              current_api_key: "sk-***",
              current_base_url: "https://api.openai.com/v1",
            },
            {
              id: "openai-compatible",
              name: "OPENAI-COMPATIBLE",
              display_name: "Compat",
              openai_compatible: true,
              api_key_prefix: "OPENAI_COMPATIBLE_API_KEY",
              models: [{ id: "deepseek-chat", name: "deepseek-chat", limit: { context: 64000 } }],
              allow_custom_base_url: true,
              enabled: true,
              has_api_key: true,
              current_api_key: "skm***123",
              current_base_url: "https://example.com/v1",
            },
          ],
          provider_types: [
            { id: "openai", display_name: "openai" },
            { id: "openai-compatible", display_name: "openai Compatible" },
          ],
          defaults: {
            openai: "gpt-4o-mini",
            "openai-compatible": "deepseek-chat",
          },
          active_llm: activeLLM,
        });
      }

      if (url.pathname === "/models/active" && method === "PUT") {
        const payload = JSON.parse(String(init?.body ?? "{}")) as { provider_id?: string; model?: string };
        activeLLM = {
          provider_id: payload.provider_id ?? "",
          model: payload.model ?? "",
        };
        return jsonResponse({
          active_llm: activeLLM,
        });
      }

      if (url.pathname.startsWith("/workspace/files/") && method === "GET") {
        return jsonResponse({ error: { code: "not_found", message: "not found" } }, 404);
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    await waitFor(() => {
      const text = document.getElementById("composer-token-estimate")?.textContent ?? "";
      return text.includes("/32.0k");
    });

    const providerSelect = document.getElementById("composer-provider-select") as HTMLSelectElement;
    providerSelect.value = "openai-compatible";
    providerSelect.dispatchEvent(new Event("change", { bubbles: true }));

    await waitFor(() => {
      const text = document.getElementById("composer-token-estimate")?.textContent ?? "";
      return text.includes("/64.0k");
    });
  });

  it("updates used context tokens after saving system prompt files", async () => {
    const agentsPath = "prompts/AGENTS.md";
    const aiToolsPath = "prompts/ai-tools.md";
    const agentsPathEncoded = encodeURIComponent(agentsPath);
    const aiToolsPathEncoded = encodeURIComponent(aiToolsPath);
    let agentsContent = "aaaa ".repeat(100).trim();
    const aiToolsContent = "bbbb ".repeat(100).trim();
    const updatedAgentsContent = "aaaa ".repeat(300).trim();
    const draftText = "cccc ".repeat(100).trim();
    let agentFileSaved = false;

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [
            {
              id: "openai",
              name: "OPENAI",
              display_name: "OpenAI",
              openai_compatible: true,
              api_key_prefix: "OPENAI_API_KEY",
              models: [{ id: "gpt-4o-mini", name: "gpt-4o-mini", limit: { context: 32000 } }],
              allow_custom_base_url: true,
              enabled: true,
              has_api_key: true,
              current_api_key: "sk-***",
              current_base_url: "https://api.openai.com/v1",
            },
          ],
          provider_types: [{ id: "openai", display_name: "openai" }],
          defaults: {
            openai: "gpt-4o-mini",
          },
          active_llm: {
            provider_id: "openai",
            model: "gpt-4o-mini",
          },
        });
      }

      if (url.pathname === "/workspace/files" && method === "GET") {
        return jsonResponse([
          { path: agentsPath, kind: "config", size: agentsContent.length },
          { path: aiToolsPath, kind: "config", size: aiToolsContent.length },
        ]);
      }

      if (url.pathname === `/workspace/files/${agentsPathEncoded}` && method === "GET") {
        return jsonResponse({ content: agentsContent });
      }

      if (url.pathname === `/workspace/files/${agentsPathEncoded}` && method === "PUT") {
        const payload = JSON.parse(String(init?.body ?? "{}")) as { content?: string };
        agentsContent = payload.content ?? "";
        agentFileSaved = true;
        return jsonResponse({ ok: true });
      }

      if (url.pathname === `/workspace/files/${aiToolsPathEncoded}` && method === "GET") {
        return jsonResponse({ content: aiToolsContent });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const messageInput = document.getElementById("message-input") as HTMLTextAreaElement;
    messageInput.value = draftText;
    messageInput.dispatchEvent(new Event("input", { bubbles: true }));

    await waitFor(() => {
      const text = document.getElementById("composer-token-estimate")?.textContent ?? "";
      return text.includes("0.3k/32.0k");
    });

    const settingsToggleButton = document.getElementById("settings-toggle") as HTMLButtonElement;
    settingsToggleButton.click();
    const workspaceSectionButton = document.querySelector<HTMLButtonElement>('button[data-settings-section="workspace"]');
    expect(workspaceSectionButton).not.toBeNull();
    workspaceSectionButton?.click();

    await waitFor(() =>
      Boolean(document.querySelector<HTMLButtonElement>('button[data-workspace-action="open-prompt"]')),
    );
    const openPromptButton = document.querySelector<HTMLButtonElement>('button[data-workspace-action="open-prompt"]');
    openPromptButton?.click();

    await waitFor(() =>
      Boolean(
        document.querySelector<HTMLButtonElement>(`button[data-workspace-open="${agentsPath}"]`),
      ),
    );
    const openAgentsButton = document.querySelector<HTMLButtonElement>(`button[data-workspace-open="${agentsPath}"]`);
    openAgentsButton?.click();

    await waitFor(() => {
      const editor = document.getElementById("workspace-file-content") as HTMLTextAreaElement | null;
      return Boolean(editor && editor.value.includes("aaaa"));
    });

    const workspaceContentInput = document.getElementById("workspace-file-content") as HTMLTextAreaElement;
    workspaceContentInput.value = updatedAgentsContent;
    const workspaceEditorForm = document.getElementById("workspace-editor-form") as HTMLFormElement;
    workspaceEditorForm.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => agentFileSaved);
    await waitFor(() => {
      const text = document.getElementById("composer-token-estimate")?.textContent ?? "";
      return text.includes("0.5k/32.0k");
    });
  });

  it("切换 prompt_mode 后会按新模式重载 system layer token 估算", async () => {
    const requestedModes: string[] = [];
    let defaultModeCalls = 0;

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/runtime-config" && method === "GET") {
        return jsonResponse({
          features: {
            prompt_templates: false,
            prompt_context_introspect: true,
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [
            {
              id: "openai",
              name: "OPENAI",
              display_name: "OpenAI",
              openai_compatible: true,
              api_key_prefix: "OPENAI_API_KEY",
              models: [{ id: "gpt-4o-mini", name: "gpt-4o-mini", limit: { context: 32000 } }],
              allow_custom_base_url: true,
              enabled: true,
              has_api_key: true,
              current_api_key: "sk-***",
              current_base_url: "https://api.openai.com/v1",
            },
          ],
          provider_types: [{ id: "openai", display_name: "openai" }],
          defaults: {
            openai: "gpt-4o-mini",
          },
          active_llm: {
            provider_id: "openai",
            model: "gpt-4o-mini",
          },
        });
      }

      if (url.pathname === "/agent/system-layers" && method === "GET") {
        const promptMode = url.searchParams.get("prompt_mode") ?? "default";
        requestedModes.push(promptMode);
        let total = 1000;
        if (promptMode === "codex") {
          total = 11000;
        } else {
          defaultModeCalls += 1;
          if (defaultModeCalls >= 2) {
            total = 2000;
          }
        }
        return jsonResponse({
          version: "v1",
          mode_variant: promptMode === "codex" ? "codex_v1" : "default",
          layers: [],
          estimated_tokens_total: total,
        });
      }

      if (url.pathname.startsWith("/workspace/files/") && method === "GET") {
        return jsonResponse({ error: { code: "not_found", message: "not found" } }, 404);
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    await waitFor(() => requestedModes.includes("default"), 4000);
    await waitFor(() => {
      const text = document.getElementById("composer-token-estimate")?.textContent ?? "";
      return text.includes("1.0k/32.0k");
    });

    const promptModeSelect = document.getElementById("chat-prompt-mode-select") as HTMLSelectElement;
    promptModeSelect.value = "codex";
    promptModeSelect.dispatchEvent(new Event("change", { bubbles: true }));

    await waitFor(() => requestedModes.includes("codex"), 4000);
    await waitFor(() => {
      const text = document.getElementById("composer-token-estimate")?.textContent ?? "";
      return text.includes("11.0k/32.0k");
    });

    promptModeSelect.value = "default";
    promptModeSelect.dispatchEvent(new Event("change", { bubbles: true }));

    await waitFor(() => defaultModeCalls >= 2, 4000);
    await waitFor(() => {
      const text = document.getElementById("composer-token-estimate")?.textContent ?? "";
      return text.includes("2.0k/32.0k");
    });
  });

  it("首个 system layer 请求未完成时切换 prompt_mode 仍会刷新到新模式估算", async () => {
    let pendingDefaultResolve: ((value: Response) => void) | null = null;
    let defaultModeCalls = 0;
    let codexModeCalls = 0;

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/runtime-config" && method === "GET") {
        return jsonResponse({
          features: {
            prompt_templates: false,
            prompt_context_introspect: true,
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [
            {
              id: "openai",
              name: "OPENAI",
              display_name: "OpenAI",
              openai_compatible: true,
              api_key_prefix: "OPENAI_API_KEY",
              models: [{ id: "gpt-4o-mini", name: "gpt-4o-mini", limit: { context: 32000 } }],
              allow_custom_base_url: true,
              enabled: true,
              has_api_key: true,
              current_api_key: "sk-***",
              current_base_url: "https://api.openai.com/v1",
            },
          ],
          provider_types: [{ id: "openai", display_name: "openai" }],
          defaults: {
            openai: "gpt-4o-mini",
          },
          active_llm: {
            provider_id: "openai",
            model: "gpt-4o-mini",
          },
        });
      }

      if (url.pathname === "/agent/system-layers" && method === "GET") {
        const promptMode = url.searchParams.get("prompt_mode") ?? "default";
        if (promptMode === "default") {
          defaultModeCalls += 1;
          return new Promise<Response>((resolve) => {
            pendingDefaultResolve = resolve;
          });
        }
        codexModeCalls += 1;
        return jsonResponse({
          version: "v1",
          mode_variant: "codex_v1",
          layers: [],
          estimated_tokens_total: 9000,
        });
      }

      if (url.pathname.startsWith("/workspace/files/") && method === "GET") {
        return jsonResponse({ error: { code: "not_found", message: "not found" } }, 404);
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");
    await waitFor(() => pendingDefaultResolve !== null, 4000);
    expect(defaultModeCalls).toBeGreaterThan(0);
    const statusLine = document.getElementById("status-line") as HTMLElement;
    await waitFor(() => (statusLine.textContent ?? "").includes("草稿会话"), 4000);

    const promptModeSelect = document.getElementById("chat-prompt-mode-select") as HTMLSelectElement;
    promptModeSelect.value = "codex";
    promptModeSelect.dispatchEvent(new Event("change", { bubbles: true }));

    pendingDefaultResolve?.(
      jsonResponse({
        version: "v1",
        mode_variant: "default",
        layers: [],
        estimated_tokens_total: 1000,
      }),
    );

    await waitFor(() => codexModeCalls > 0, 4000);
    await waitFor(() => {
      const text = document.getElementById("composer-token-estimate")?.textContent ?? "";
      return text.includes("9.0k/32.0k");
    }, 4000);
  });

  it("codex 协作模式会驱动 collaboration_mode 级别的 system layer 估算", async () => {
    const requestedCollaborationModes: string[] = [];

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/runtime-config" && method === "GET") {
        return jsonResponse({
          features: {
            prompt_templates: false,
            prompt_context_introspect: true,
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [
            {
              id: "openai",
              name: "OPENAI",
              display_name: "OpenAI",
              openai_compatible: true,
              api_key_prefix: "OPENAI_API_KEY",
              models: [{ id: "gpt-4o-mini", name: "gpt-4o-mini", limit: { context: 32000 } }],
              allow_custom_base_url: true,
              enabled: true,
              has_api_key: true,
              current_api_key: "sk-***",
              current_base_url: "https://api.openai.com/v1",
            },
          ],
          provider_types: [{ id: "openai", display_name: "openai" }],
          defaults: {
            openai: "gpt-4o-mini",
          },
          active_llm: {
            provider_id: "openai",
            model: "gpt-4o-mini",
          },
        });
      }

      if (url.pathname === "/agent/system-layers" && method === "GET") {
        const collaborationMode = url.searchParams.get("collaboration_mode") ?? "";
        requestedCollaborationModes.push(collaborationMode);
        const total = collaborationMode === "plan" ? 7000 : 3000;
        return jsonResponse({
          version: "v1",
          mode_variant: "codex_v1",
          layers: [],
          estimated_tokens_total: total,
        });
      }

      if (url.pathname.startsWith("/workspace/files/") && method === "GET") {
        return jsonResponse({ error: { code: "not_found", message: "not found" } }, 404);
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const statusLine = document.getElementById("status-line") as HTMLElement;
    const promptModeSelect = document.getElementById("chat-prompt-mode-select") as HTMLSelectElement;
    const collaborationModeSelect = document.getElementById("chat-collaboration-mode-select") as HTMLSelectElement;
    await waitFor(() => (statusLine.textContent ?? "").includes("草稿会话"), 4000);

    const initialRequestCount = requestedCollaborationModes.length;
    promptModeSelect.value = "codex";
    promptModeSelect.dispatchEvent(new Event("change", { bubbles: true }));
    await waitFor(() => promptModeSelect.value === "codex", 4000);
    await waitFor(() => requestedCollaborationModes.length > initialRequestCount, 4000);

    await waitFor(() => {
      const text = document.getElementById("composer-token-estimate")?.textContent ?? "";
      return text.includes("3.0k/32.0k");
    }, 4000);

    collaborationModeSelect.value = "plan";
    collaborationModeSelect.dispatchEvent(new Event("change", { bubbles: true }));

    await waitFor(() => requestedCollaborationModes.includes("plan"), 4000);
    await waitFor(() => {
      const text = document.getElementById("composer-token-estimate")?.textContent ?? "";
      return text.includes("7.0k/32.0k");
    }, 4000);
  });

  it("uploads selected files and appends returned paths into composer", async () => {
    let uploadCount = 0;
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/workspace/uploads" && method === "POST") {
        uploadCount += 1;
        expect(init?.body instanceof FormData).toBe(true);
        const uploadedPath = uploadCount === 1 ? "/tmp/upload-notes.txt" : "/tmp/upload-photo.png";
        return jsonResponse({
          uploaded: true,
          path: uploadedPath,
          name: uploadedPath.split("/").pop(),
          size: 3,
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const attachButton = document.getElementById("composer-attach-btn") as HTMLButtonElement;
    const attachInput = document.getElementById("composer-attach-input") as HTMLInputElement;
    const messageInput = document.getElementById("message-input") as HTMLTextAreaElement;
    expect(attachButton).not.toBeNull();
    expect(attachInput).not.toBeNull();
    expect(messageInput).not.toBeNull();

    const attachInputClick = vi.spyOn(attachInput, "click");
    attachButton.click();
    expect(attachInputClick).toHaveBeenCalledTimes(1);

    messageInput.value = "look at these";
    const fakeFiles = [
      new File(["doc"], "notes.txt", { type: "text/plain" }),
      new File([new Uint8Array([1, 2, 3])], "photo.png", { type: "image/png" }),
    ] as unknown as FileList;
    Object.defineProperty(attachInput, "files", {
      configurable: true,
      value: fakeFiles,
    });
    attachInput.dispatchEvent(new Event("change", { bubbles: true }));

    await waitFor(() => messageInput.value.includes("@/tmp/upload-notes.txt") && messageInput.value.includes("@/tmp/upload-photo.png"));
    expect(uploadCount).toBe(2);
    expect(messageInput.value).toContain("look at these");
    expect(messageInput.value).toContain("@/tmp/upload-notes.txt");
    expect(messageInput.value).toContain("@/tmp/upload-photo.png");
  });

  it("uploads dropped files and appends returned paths into composer", async () => {
    let uploadCount = 0;
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/workspace/uploads" && method === "POST") {
        uploadCount += 1;
        expect(init?.body instanceof FormData).toBe(true);
        const uploadedPath = uploadCount === 1 ? "/tmp/drop-draft.md" : "/tmp/drop-diagram.svg";
        return jsonResponse({
          uploaded: true,
          path: uploadedPath,
          name: uploadedPath.split("/").pop(),
          size: 4,
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const composerMain = document.getElementById("composer-main") as HTMLElement;
    const messageInput = document.getElementById("message-input") as HTMLTextAreaElement;
    expect(composerMain).not.toBeNull();
    expect(messageInput).not.toBeNull();

    const dragOverEvent = new Event("dragover", { bubbles: true, cancelable: true }) as DragEvent;
    Object.defineProperty(dragOverEvent, "dataTransfer", {
      configurable: true,
      value: {
        types: ["Files"],
        dropEffect: "none",
      } satisfies Partial<DataTransfer>,
    });
    composerMain.dispatchEvent(dragOverEvent);
    expect(dragOverEvent.defaultPrevented).toBe(true);
    expect(composerMain.classList.contains("is-file-drag-over")).toBe(true);

    messageInput.value = "please review";
    const fakeFiles = [
      new File(["demo"], "draft.md", { type: "text/markdown" }),
      new File([new Uint8Array([9, 8, 7])], "diagram.svg", { type: "image/svg+xml" }),
    ] as unknown as FileList;
    const dropEvent = new Event("drop", { bubbles: true, cancelable: true }) as DragEvent;
    const droppedURIList = [
      "file:///Users/demo/workspace/docs/draft.md",
      "file:///Users/demo/workspace/assets/diagram.svg",
    ].join("\n");
    Object.defineProperty(dropEvent, "dataTransfer", {
      configurable: true,
      value: {
        types: ["Files"],
        files: fakeFiles,
        dropEffect: "none",
        getData: (type: string) => (type === "text/uri-list" ? droppedURIList : ""),
      } satisfies Partial<DataTransfer>,
    });
    composerMain.dispatchEvent(dropEvent);
    expect(dropEvent.defaultPrevented).toBe(true);

    await waitFor(() => messageInput.value.includes("@/tmp/drop-draft.md") && messageInput.value.includes("@/tmp/drop-diagram.svg"));
    expect(uploadCount).toBe(2);
    expect(composerMain.classList.contains("is-file-drag-over")).toBe(false);
    expect(messageInput.value).toContain("please review");
    expect(messageInput.value).toContain("@/tmp/drop-draft.md");
    expect(messageInput.value).toContain("@/tmp/drop-diagram.svg");
  });

  it("filters custom select options with the dropdown search input", async () => {
    let activeLLM = {
      provider_id: "openai",
      model: "gpt-4o-mini",
    };

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [
            {
              id: "openai",
              name: "OPENAI",
              display_name: "OpenAI",
              openai_compatible: true,
              api_key_prefix: "OPENAI_API_KEY",
              models: [
                { id: "gpt-4o-mini", name: "gpt-4o-mini" },
                { id: "gpt-4.1-mini", name: "gpt-4.1-mini" },
                { id: "deepseek-chat", name: "deepseek-chat" },
              ],
              allow_custom_base_url: true,
              enabled: true,
              has_api_key: true,
              current_api_key: "sk-***",
              current_base_url: "https://api.openai.com/v1",
            },
          ],
          provider_types: [{ id: "openai", display_name: "openai" }],
          defaults: {
            openai: "gpt-4o-mini",
          },
          active_llm: activeLLM,
        });
      }

      if (url.pathname === "/models/active" && method === "PUT") {
        const payload = JSON.parse(String(init?.body ?? "{}")) as { provider_id?: string; model?: string };
        activeLLM = {
          provider_id: payload.provider_id ?? "",
          model: payload.model ?? "",
        };
        return jsonResponse({
          active_llm: activeLLM,
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const modelSelect = await waitForComposerSelectReady("composer-model-select");
    const container = modelSelect.closest<HTMLDivElement>(".custom-select-container");
    const trigger = container?.querySelector<HTMLDivElement>(".select-trigger") ?? null;
    expect(container).not.toBeNull();
    expect(trigger).not.toBeNull();
    if (!container || !trigger) {
      return;
    }

    trigger.dispatchEvent(new MouseEvent("click", { bubbles: true }));

    await waitFor(() => container.classList.contains("open"));

    const searchInput = container.querySelector<HTMLInputElement>(".options-search-input");
    expect(searchInput).not.toBeNull();
    if (!searchInput) {
      return;
    }

    searchInput.value = "deep";
    searchInput.dispatchEvent(new Event("input", { bubbles: true }));

    await waitFor(() => {
      const visibleOptions = Array.from(container.querySelectorAll<HTMLElement>(".options-body .option")).filter(
        (option) => !option.classList.contains("is-hidden"),
      );
      return visibleOptions.length === 1 && (visibleOptions[0]?.textContent ?? "").includes("deepseek-chat");
    });

    const deepseekOption = container.querySelector<HTMLElement>('.options-body .option[data-value="deepseek-chat"]');
    expect(deepseekOption?.classList.contains("is-hidden")).toBe(false);
    deepseekOption?.dispatchEvent(new MouseEvent("click", { bubbles: true }));

    expect(modelSelect.value).toBe("deepseek-chat");
  });

  it("opens composer toolbar select upward when lower space is clipped", async () => {
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [
            {
              id: "openai",
              name: "OPENAI",
              display_name: "OpenAI",
              openai_compatible: true,
              api_key_prefix: "OPENAI_API_KEY",
              models: [{ id: "gpt-4o-mini", name: "gpt-4o-mini" }],
              allow_custom_base_url: true,
              enabled: true,
              has_api_key: true,
              current_api_key: "sk-***",
              current_base_url: "https://api.openai.com/v1",
            },
          ],
          provider_types: [
            { id: "openai", display_name: "openai" },
          ],
          defaults: {
            openai: "gpt-4o-mini",
          },
          active_llm: {
            provider_id: "openai",
            model: "gpt-4o-mini",
          },
        });
      }

      if (url.pathname === "/models/active" && method === "PUT") {
        return jsonResponse({
          active_llm: {
            provider_id: "openai",
            model: "gpt-4o-mini",
          },
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const providerSelect = await waitForComposerSelectReady("composer-provider-select");
    const container = providerSelect.closest<HTMLDivElement>(".custom-select-container");
    const trigger = container?.querySelector<HTMLDivElement>(".select-trigger") ?? null;
    const chatPanel = document.querySelector<HTMLElement>(".chat.panel");
    expect(container).not.toBeNull();
    expect(trigger).not.toBeNull();
    expect(chatPanel).not.toBeNull();
    if (!container || !trigger || !chatPanel) {
      return;
    }

    chatPanel.style.overflow = "hidden";

    vi.spyOn(chatPanel, "getBoundingClientRect").mockReturnValue(rect(120, 360, 0, 940));
    vi.spyOn(trigger, "getBoundingClientRect").mockReturnValue(rect(310, 344, 20, 220));

    trigger.dispatchEvent(new MouseEvent("click", { bubbles: true }));

    expect(container.classList.contains("open")).toBe(true);
    expect(container.classList.contains("open-upward")).toBe(true);
  });

  it("adding openai-compatible provider does not overwrite existing same-type config", async () => {
    const existingProviderID = "openai-compatible";
    const modelID = "ark-code-latest";
    const catalogProviders = [
      {
        id: existingProviderID,
        name: "OPENAI-COMPATIBLE",
        display_name: "宸叉湁 Provider",
        openai_compatible: true,
        api_key_prefix: "OPENAI_COMPATIBLE_API_KEY",
        models: [{ id: modelID, name: modelID }],
        allow_custom_base_url: true,
        enabled: true,
        has_api_key: true,
        current_api_key: "skm***123",
        current_base_url: "https://example.com/v1",
      },
    ];

    let configuredProviderID = "";
    let configuredStore = false;
    let overwroteExisting = false;

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: catalogProviders,
          provider_types: [
            { id: "openai", display_name: "openai" },
            { id: "openai-compatible", display_name: "openai Compatible" },
          ],
          defaults: Object.fromEntries(catalogProviders.map((provider) => [provider.id, modelID])),
          active_llm: {
            provider_id: existingProviderID,
            model: modelID,
          },
        });
      }

      if (url.pathname.startsWith("/models/") && url.pathname.endsWith("/config") && method === "PUT") {
        const payload = JSON.parse(String(init?.body ?? "{}")) as { store?: boolean };
        const rawProviderID = url.pathname.slice("/models/".length, url.pathname.length - "/config".length);
        configuredProviderID = decodeURIComponent(rawProviderID);
        configuredStore = payload.store === true;
        const exists = catalogProviders.some((provider) => provider.id === configuredProviderID);
        if (exists) {
          overwroteExisting = true;
        } else {
          catalogProviders.push({
            id: configuredProviderID,
            name: configuredProviderID.toUpperCase(),
            display_name: configuredProviderID,
            openai_compatible: true,
            api_key_prefix: "OPENAI_COMPATIBLE_API_KEY",
            models: [{ id: modelID, name: modelID }],
            allow_custom_base_url: true,
            enabled: true,
            store: configuredStore,
            has_api_key: false,
            current_api_key: "",
            current_base_url: "",
          });
        }
        return jsonResponse(catalogProviders.find((provider) => provider.id === configuredProviderID) ?? {}, 200);
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const settingsToggleButton = document.getElementById("settings-toggle") as HTMLButtonElement;
    settingsToggleButton.click();
    const modelsSectionButton = document.querySelector<HTMLButtonElement>('button[data-settings-section="models"]');
    expect(modelsSectionButton).not.toBeNull();
    modelsSectionButton?.click();

    await waitFor(() =>
      Boolean(
        document.querySelector<HTMLButtonElement>(
          `button[data-provider-action="select"][data-provider-id="${existingProviderID}"]`,
        ),
      ),
    );

    const addProviderButton = document.getElementById("models-add-provider-btn") as HTMLButtonElement;
    addProviderButton.click();

    await waitFor(() => {
      const level2 = document.getElementById("models-level2-view");
      return Boolean(level2 && !level2.hasAttribute("hidden"));
    });

    const providerTypeSelect = document.getElementById("models-provider-type-select") as HTMLSelectElement;
    providerTypeSelect.value = "openai-compatible";
    providerTypeSelect.dispatchEvent(new Event("change", { bubbles: true }));

    const providerStoreField = document.getElementById("models-provider-store-field") as HTMLElement;
    const providerStoreInput = document.getElementById("models-provider-store-input") as HTMLInputElement;
    expect(providerStoreField.hidden).toBe(false);
    expect(providerStoreInput.checked).toBe(false);
    providerStoreInput.checked = true;
    providerStoreInput.dispatchEvent(new Event("change", { bubbles: true }));

    const providerForm = document.getElementById("models-provider-form") as HTMLFormElement;
    providerForm.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => configuredProviderID !== "");

    expect(configuredProviderID).toBe("openai-compatible-2");
    expect(configuredStore).toBe(true);
    expect(overwroteExisting).toBe(false);
    expect(catalogProviders.map((provider) => provider.id)).toContain(existingProviderID);
    expect(catalogProviders.map((provider) => provider.id)).toContain("openai-compatible-2");
  });

  it("adding codex-compatible provider uses codex-compatible id and type", async () => {
    const openAIProvider = {
      id: "openai",
      name: "OPENAI",
      display_name: "OpenAI Default",
      openai_compatible: true,
      api_key_prefix: "OPENAI_API_KEY",
      models: [{ id: "gpt-4o-mini", name: "gpt-4o-mini" }],
      allow_custom_base_url: true,
      enabled: true,
      store: false,
      has_api_key: true,
      current_api_key: "sk***123",
      current_base_url: "https://api.openai.com/v1",
    };
    const catalogProviders = [openAIProvider];
    let configuredProviderID = "";
    let configuredStore = false;

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: catalogProviders,
          provider_types: [
            { id: "openai", display_name: "openai" },
            { id: "openai-compatible", display_name: "openai Compatible" },
            { id: "codex-compatible", display_name: "codex Compatible" },
          ],
          defaults: {
            openai: "gpt-4o-mini",
          },
          active_llm: {
            provider_id: "openai",
            model: "gpt-4o-mini",
          },
        });
      }

      if (url.pathname.startsWith("/models/") && url.pathname.endsWith("/config") && method === "PUT") {
        const payload = JSON.parse(String(init?.body ?? "{}")) as { store?: boolean };
        const rawProviderID = url.pathname.slice("/models/".length, url.pathname.length - "/config".length);
        configuredProviderID = decodeURIComponent(rawProviderID);
        configuredStore = payload.store === true;
        catalogProviders.push({
          id: configuredProviderID,
          name: "CODEX-COMPATIBLE",
          display_name: "Codex Remote",
          openai_compatible: false,
          api_key_prefix: "CODEX_COMPATIBLE_API_KEY",
          models: [{ id: "gpt-5-codex", name: "gpt-5-codex" }],
          allow_custom_base_url: true,
          enabled: true,
          store: configuredStore,
          has_api_key: false,
          current_api_key: "",
          current_base_url: "",
        });
        return jsonResponse(catalogProviders.find((provider) => provider.id === configuredProviderID) ?? {}, 200);
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const settingsToggleButton = document.getElementById("settings-toggle") as HTMLButtonElement;
    settingsToggleButton.click();
    const modelsSectionButton = document.querySelector<HTMLButtonElement>('button[data-settings-section="models"]');
    expect(modelsSectionButton).not.toBeNull();
    modelsSectionButton?.click();

    await waitFor(() =>
      Boolean(
        document.querySelector<HTMLButtonElement>(
          'button[data-provider-action="select"][data-provider-id="openai"]',
        ),
      ),
    );

    const addProviderButton = document.getElementById("models-add-provider-btn") as HTMLButtonElement;
    addProviderButton.click();

    await waitFor(() => {
      const level2 = document.getElementById("models-level2-view");
      return Boolean(level2 && !level2.hasAttribute("hidden"));
    });

    const providerTypeSelect = document.getElementById("models-provider-type-select") as HTMLSelectElement;
    providerTypeSelect.value = "codex-compatible";
    providerTypeSelect.dispatchEvent(new Event("change", { bubbles: true }));

    const providerStoreField = document.getElementById("models-provider-store-field") as HTMLElement;
    const providerStoreInput = document.getElementById("models-provider-store-input") as HTMLInputElement;
    expect(providerStoreField.hidden).toBe(false);
    expect(providerStoreInput.checked).toBe(false);
    providerStoreInput.checked = true;
    providerStoreInput.dispatchEvent(new Event("change", { bubbles: true }));

    const providerNameInput = document.getElementById("models-provider-name-input") as HTMLInputElement;
    providerNameInput.value = "Codex Remote";

    const providerForm = document.getElementById("models-provider-form") as HTMLFormElement;
    providerForm.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => configuredProviderID !== "");

    expect(configuredProviderID).toBe("codex-compatible");
    expect(configuredStore).toBe(true);
    await waitFor(() =>
      Boolean(
        document.querySelector<HTMLButtonElement>(
          `button[data-provider-action="select"][data-provider-id="${configuredProviderID}"]`,
        ),
      ),
    );
    const createdProviderButton = document.querySelector<HTMLButtonElement>(
      `button[data-provider-action="select"][data-provider-id="${configuredProviderID}"]`,
    );
    expect(createdProviderButton).not.toBeNull();
    expect(createdProviderButton?.textContent?.includes("codex Compatible")).toBe(true);
  });

  it("adding openai provider keeps existing config and creates openai-2 with default aliases", async () => {
    const existingProviderID = "openai";
    const openAIModels = ["gpt-4o-mini", "gpt-4.1-mini"];
    const catalogProviders = [
      {
        id: existingProviderID,
        name: "OPENAI",
        display_name: "OpenAI Default",
        openai_compatible: true,
        api_key_prefix: "OPENAI_API_KEY",
        models: openAIModels.map((modelID) => ({ id: modelID, name: modelID })),
        allow_custom_base_url: true,
        enabled: true,
        has_api_key: true,
        current_api_key: "sk***123",
        current_base_url: "https://api.openai.com/v1",
      },
    ];

    let configuredProviderID = "";
    let overwroteExisting = false;
    let configuredAliases: Record<string, string> = {};

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: catalogProviders,
          provider_types: [
            { id: "openai", display_name: "openai" },
            { id: "openai-compatible", display_name: "openai Compatible" },
          ],
          defaults: {
            [existingProviderID]: openAIModels[0],
          },
          active_llm: {
            provider_id: existingProviderID,
            model: openAIModels[0],
          },
        });
      }

      if (url.pathname.startsWith("/models/") && url.pathname.endsWith("/config") && method === "PUT") {
        const rawProviderID = url.pathname.slice("/models/".length, url.pathname.length - "/config".length);
        configuredProviderID = decodeURIComponent(rawProviderID);
        const payload = JSON.parse(String(init?.body ?? "{}")) as { model_aliases?: Record<string, string> };
        configuredAliases = payload.model_aliases ?? {};
        const exists = catalogProviders.some((provider) => provider.id === configuredProviderID);
        if (exists) {
          overwroteExisting = true;
        } else {
          catalogProviders.push({
            id: configuredProviderID,
            name: configuredProviderID.toUpperCase(),
            display_name: configuredProviderID,
            openai_compatible: true,
            api_key_prefix: "OPENAI_2_API_KEY",
            models: Object.keys(configuredAliases).map((modelID) => ({ id: modelID, name: modelID })),
            allow_custom_base_url: true,
            enabled: true,
            has_api_key: false,
            current_api_key: "",
            current_base_url: "",
          });
        }
        return jsonResponse(catalogProviders.find((provider) => provider.id === configuredProviderID) ?? {}, 200);
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const settingsToggleButton = document.getElementById("settings-toggle") as HTMLButtonElement;
    settingsToggleButton.click();
    const modelsSectionButton = document.querySelector<HTMLButtonElement>('button[data-settings-section="models"]');
    expect(modelsSectionButton).not.toBeNull();
    modelsSectionButton?.click();

    await waitFor(() =>
      Boolean(
        document.querySelector<HTMLButtonElement>(
          `button[data-provider-action="select"][data-provider-id="${existingProviderID}"]`,
        ),
      ),
    );

    const addProviderButton = document.getElementById("models-add-provider-btn") as HTMLButtonElement;
    addProviderButton.click();

    await waitFor(() => {
      const level2 = document.getElementById("models-level2-view");
      return Boolean(level2 && !level2.hasAttribute("hidden"));
    });

    const providerTypeSelect = document.getElementById("models-provider-type-select") as HTMLSelectElement;
    providerTypeSelect.value = "openai";
    providerTypeSelect.dispatchEvent(new Event("change", { bubbles: true }));

    const providerForm = document.getElementById("models-provider-form") as HTMLFormElement;
    providerForm.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => configuredProviderID !== "");

    expect(configuredProviderID).toBe("openai-2");
    expect(overwroteExisting).toBe(false);
    expect(configuredAliases).toEqual({
      "gpt-4o-mini": "gpt-4o-mini",
      "gpt-4.1-mini": "gpt-4.1-mini",
    });
    expect(catalogProviders.map((provider) => provider.id)).toContain(existingProviderID);
    expect(catalogProviders.map((provider) => provider.id)).toContain("openai-2");
  });

  it("reopens provider edit with custom models and aliases restored from model_aliases", async () => {
    const openAIProvider = {
      id: "openai",
      name: "OPENAI",
      display_name: "OPENAI",
      openai_compatible: true,
      api_key_prefix: "OPENAI_API_KEY",
      models: [{ id: "gpt-4o-mini", name: "gpt-4o-mini" }],
      allow_custom_base_url: true,
      enabled: true,
      has_api_key: true,
      current_api_key: "sk-***",
      current_base_url: "https://api.openai.com/v1",
    };
    const providers = [openAIProvider];
    const providerAliases = new Map<string, Record<string, string>>();
    const providerHeaders = new Map<string, Record<string, string>>();
    const providerTimeoutMS = new Map<string, number>();
    let savedProviderID = "";

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: providers.map((provider) => ({
            ...provider,
            headers: providerHeaders.get(provider.id) ?? {},
            timeout_ms: providerTimeoutMS.get(provider.id),
            model_aliases: providerAliases.get(provider.id) ?? {},
          })),
          provider_types: [
            { id: "openai", display_name: "openai" },
            { id: "openai-compatible", display_name: "openai Compatible" },
          ],
          defaults: {
            openai: "gpt-4o-mini",
          },
          active_llm: {
            provider_id: "openai",
            model: "gpt-4o-mini",
          },
        });
      }

      if (url.pathname.startsWith("/models/") && url.pathname.endsWith("/config") && method === "PUT") {
        const rawProviderID = url.pathname.slice("/models/".length, url.pathname.length - "/config".length);
        const providerID = decodeURIComponent(rawProviderID);
        const payload = JSON.parse(String(init?.body ?? "{}")) as {
          display_name?: string;
          headers?: Record<string, string>;
          timeout_ms?: number;
          model_aliases?: Record<string, string>;
        };
        savedProviderID = providerID;
        providerHeaders.set(providerID, payload.headers ?? {});
        if (typeof payload.timeout_ms === "number") {
          providerTimeoutMS.set(providerID, payload.timeout_ms);
        }
        providerAliases.set(providerID, payload.model_aliases ?? {});

        const exists = providers.some((provider) => provider.id === providerID);
        if (!exists) {
          providers.push({
            id: providerID,
            name: providerID.toUpperCase(),
            display_name: payload.display_name ?? providerID,
            openai_compatible: true,
            api_key_prefix: "OPENAI_COMPATIBLE_API_KEY",
            models: [],
            allow_custom_base_url: true,
            enabled: true,
            has_api_key: false,
            current_api_key: "",
            current_base_url: "",
          });
        }
        return jsonResponse({
          id: providerID,
          name: providerID.toUpperCase(),
          display_name: payload.display_name ?? providerID,
          openai_compatible: true,
          api_key_prefix: "OPENAI_COMPATIBLE_API_KEY",
          models: [],
          headers: payload.headers ?? {},
          timeout_ms: payload.timeout_ms,
          model_aliases: payload.model_aliases ?? {},
          allow_custom_base_url: true,
          enabled: true,
          has_api_key: false,
          current_api_key: "",
          current_base_url: "",
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const settingsToggleButton = document.getElementById("settings-toggle") as HTMLButtonElement;
    settingsToggleButton.click();
    const modelsSectionButton = document.querySelector<HTMLButtonElement>('button[data-settings-section="models"]');
    expect(modelsSectionButton).not.toBeNull();
    modelsSectionButton?.click();

    await waitFor(() =>
      Boolean(
        document.querySelector<HTMLButtonElement>('button[data-provider-action="select"][data-provider-id="openai"]'),
      ),
    );

    const addProviderButton = document.getElementById("models-add-provider-btn") as HTMLButtonElement;
    addProviderButton.click();

    await waitFor(() => {
      const level2 = document.getElementById("models-level2-view");
      return Boolean(level2 && !level2.hasAttribute("hidden"));
    });

    const providerTypeSelect = document.getElementById("models-provider-type-select") as HTMLSelectElement;
    providerTypeSelect.value = "openai-compatible";
    providerTypeSelect.dispatchEvent(new Event("change", { bubbles: true }));

    const providerNameInput = document.getElementById("models-provider-name-input") as HTMLInputElement;
    providerNameInput.value = "My Compat";

    const customModelInput = document.querySelector<HTMLInputElement>('#models-provider-custom-models-rows input[data-custom-model-input="true"]');
    expect(customModelInput).not.toBeNull();
    if (customModelInput) {
      customModelInput.value = "claude-3-5-sonnet";
    }

    const aliasRow = document.querySelector<HTMLElement>("#models-provider-aliases-rows .kv-row");
    expect(aliasRow).not.toBeNull();
    const aliasKeyInput = aliasRow?.querySelector<HTMLInputElement>('input[data-kv-field="key"]') ?? null;
    const aliasValueInput = aliasRow?.querySelector<HTMLInputElement>('input[data-kv-field="value"]') ?? null;
    expect(aliasKeyInput).not.toBeNull();
    expect(aliasValueInput).not.toBeNull();
    if (aliasKeyInput && aliasValueInput) {
      aliasKeyInput.value = "fast";
      aliasValueInput.value = "gpt-4o-mini";
    }

    const timeoutInput = document.getElementById("models-provider-timeout-ms-input") as HTMLInputElement;
    timeoutInput.value = "15000";

    const headerRow = document.querySelector<HTMLElement>("#models-provider-headers-rows .kv-row");
    expect(headerRow).not.toBeNull();
    const headerKeyInput = headerRow?.querySelector<HTMLInputElement>('input[data-kv-field="key"]') ?? null;
    const headerValueInput = headerRow?.querySelector<HTMLInputElement>('input[data-kv-field="value"]') ?? null;
    expect(headerKeyInput).not.toBeNull();
    expect(headerValueInput).not.toBeNull();
    if (headerKeyInput && headerValueInput) {
      headerKeyInput.value = "x-tenant";
      headerValueInput.value = "nextai";
    }

    const providerForm = document.getElementById("models-provider-form") as HTMLFormElement;
    providerForm.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => savedProviderID !== "");
    expect(savedProviderID).toBe("my-compat");

    await waitFor(() =>
      Boolean(
        document.querySelector<HTMLButtonElement>(
          `button[data-provider-action="select"][data-provider-id="${savedProviderID}"]`,
        ),
      ),
    );

    const providerCardButton = document.querySelector<HTMLButtonElement>(
      `button[data-provider-action="select"][data-provider-id="${savedProviderID}"]`,
    );
    expect(providerCardButton).not.toBeNull();
    providerCardButton?.click();

    await waitFor(() => {
      const level2 = document.getElementById("models-level2-view");
      return Boolean(level2 && !level2.hasAttribute("hidden"));
    });

    const customModelInputs = Array.from(
      document.querySelectorAll<HTMLInputElement>('#models-provider-custom-models-rows input[data-custom-model-input="true"]'),
    );
    expect(customModelInputs.map((item) => item.value.trim())).toContain("claude-3-5-sonnet");

    const aliasRows = Array.from(document.querySelectorAll<HTMLElement>("#models-provider-aliases-rows .kv-row"));
    const aliasPairs = aliasRows.map((row) => {
      const key = row.querySelector<HTMLInputElement>('input[data-kv-field="key"]')?.value.trim() ?? "";
      const value = row.querySelector<HTMLInputElement>('input[data-kv-field="value"]')?.value.trim() ?? "";
      return `${key}:${value}`;
    });
    expect(aliasPairs).toContain("fast:gpt-4o-mini");

    const reopenedTimeoutInput = document.getElementById("models-provider-timeout-ms-input") as HTMLInputElement;
    expect(reopenedTimeoutInput.value).toBe("15000");

    const headerRows = Array.from(document.querySelectorAll<HTMLElement>("#models-provider-headers-rows .kv-row"));
    const headerPairs = headerRows.map((row) => {
      const key = row.querySelector<HTMLInputElement>('input[data-kv-field="key"]')?.value.trim() ?? "";
      const value = row.querySelector<HTMLInputElement>('input[data-kv-field="value"]')?.value.trim() ?? "";
      return `${key}:${value}`;
    });
    expect(headerPairs).toContain("x-tenant:nextai");
  });

  it("clears model_aliases timeout_ms and headers when provider form values are emptied", async () => {
    const openAIProvider = {
      id: "openai",
      name: "OPENAI",
      display_name: "OPENAI",
      openai_compatible: true,
      api_key_prefix: "OPENAI_API_KEY",
      models: [{ id: "gpt-4o-mini", name: "gpt-4o-mini" }],
      allow_custom_base_url: true,
      enabled: true,
      has_api_key: true,
      current_api_key: "sk-***",
      current_base_url: "https://api.openai.com/v1",
    };
    const customProviderID = "my-compat";
    const customProviderBase = {
      id: customProviderID,
      name: "MY-COMPAT",
      display_name: "My Compat",
      openai_compatible: true,
      api_key_prefix: "MY_COMPAT_API_KEY",
      allow_custom_base_url: true,
      enabled: true,
      has_api_key: true,
      current_api_key: "skm***123",
      current_base_url: "https://example.com/v1",
    };
    const providerAliases = new Map<string, Record<string, string>>([[customProviderID, { fast: "gpt-4o-mini" }]]);
    const providerHeaders = new Map<string, Record<string, string>>([[customProviderID, { "x-tenant": "nextai" }]]);
    const providerTimeoutMS = new Map<string, number>([[customProviderID, 15000]]);
    let savedPayload: {
      timeout_ms?: number;
      headers?: Record<string, string>;
      model_aliases?: Record<string, string>;
    } | null = null;
    let catalogRequestCount = 0;

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/models/catalog" && method === "GET") {
        catalogRequestCount += 1;
        const aliases = providerAliases.get(customProviderID) ?? {};
        const timeoutMS = providerTimeoutMS.get(customProviderID);
        return jsonResponse({
          providers: [
            openAIProvider,
            {
              ...customProviderBase,
              models: Object.entries(aliases).map(([alias, target]) => ({
                id: alias,
                name: alias,
                ...(alias === target ? {} : { alias_of: target }),
              })),
              headers: providerHeaders.get(customProviderID) ?? {},
              timeout_ms: typeof timeoutMS === "number" && timeoutMS > 0 ? timeoutMS : undefined,
              model_aliases: aliases,
            },
          ],
          provider_types: [
            { id: "openai", display_name: "openai" },
            { id: "openai-compatible", display_name: "openai Compatible" },
          ],
          defaults: {
            openai: "gpt-4o-mini",
          },
          active_llm: {
            provider_id: "openai",
            model: "gpt-4o-mini",
          },
        });
      }

      if (url.pathname === `/models/${encodeURIComponent(customProviderID)}/config` && method === "PUT") {
        const payload = JSON.parse(String(init?.body ?? "{}")) as {
          timeout_ms?: number;
          headers?: Record<string, string>;
          model_aliases?: Record<string, string>;
        };
        savedPayload = payload;
        providerHeaders.set(customProviderID, payload.headers ?? {});
        if (typeof payload.timeout_ms === "number") {
          providerTimeoutMS.set(customProviderID, payload.timeout_ms);
        } else {
          providerTimeoutMS.delete(customProviderID);
        }
        providerAliases.set(customProviderID, payload.model_aliases ?? {});
        return jsonResponse({
          ...customProviderBase,
          models: [],
          headers: payload.headers ?? {},
          timeout_ms: payload.timeout_ms,
          model_aliases: payload.model_aliases ?? {},
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const settingsToggleButton = document.getElementById("settings-toggle") as HTMLButtonElement;
    settingsToggleButton.click();
    const modelsSectionButton = document.querySelector<HTMLButtonElement>('button[data-settings-section="models"]');
    expect(modelsSectionButton).not.toBeNull();
    modelsSectionButton?.click();

    await waitFor(() =>
      Boolean(
        document.querySelector<HTMLButtonElement>(
          `button[data-provider-action="select"][data-provider-id="${customProviderID}"]`,
        ),
      ),
    );

    const providerCardButton = document.querySelector<HTMLButtonElement>(
      `button[data-provider-action="select"][data-provider-id="${customProviderID}"]`,
    );
    expect(providerCardButton).not.toBeNull();
    providerCardButton?.click();

    await waitFor(() => {
      const level2 = document.getElementById("models-level2-view");
      return Boolean(level2 && !level2.hasAttribute("hidden"));
    });

    const timeoutInput = document.getElementById("models-provider-timeout-ms-input") as HTMLInputElement;
    expect(timeoutInput.value).toBe("15000");
    timeoutInput.value = "";

    const aliasRow = document.querySelector<HTMLElement>("#models-provider-aliases-rows .kv-row");
    expect(aliasRow).not.toBeNull();
    const aliasKeyInput = aliasRow?.querySelector<HTMLInputElement>('input[data-kv-field="key"]') ?? null;
    const aliasValueInput = aliasRow?.querySelector<HTMLInputElement>('input[data-kv-field="value"]') ?? null;
    expect(aliasKeyInput).not.toBeNull();
    expect(aliasValueInput).not.toBeNull();
    if (aliasKeyInput && aliasValueInput) {
      aliasKeyInput.value = "";
      aliasValueInput.value = "";
    }

    const headerRow = document.querySelector<HTMLElement>("#models-provider-headers-rows .kv-row");
    expect(headerRow).not.toBeNull();
    const headerKeyInput = headerRow?.querySelector<HTMLInputElement>('input[data-kv-field="key"]') ?? null;
    const headerValueInput = headerRow?.querySelector<HTMLInputElement>('input[data-kv-field="value"]') ?? null;
    expect(headerKeyInput).not.toBeNull();
    expect(headerValueInput).not.toBeNull();
    if (headerKeyInput && headerValueInput) {
      headerKeyInput.value = "";
      headerValueInput.value = "";
    }

    const providerForm = document.getElementById("models-provider-form") as HTMLFormElement;
    const catalogBeforeSave = catalogRequestCount;
    providerForm.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => savedPayload !== null);
    expect(savedPayload?.timeout_ms).toBe(0);
    expect(savedPayload?.headers ?? {}).toEqual({});
    expect(savedPayload?.model_aliases ?? {}).toEqual({});
    await waitFor(() => catalogRequestCount > catalogBeforeSave);

    await waitFor(() =>
      Boolean(
        document.querySelector<HTMLButtonElement>(
          `button[data-provider-action="select"][data-provider-id="${customProviderID}"]`,
        ),
      ),
    );

    const reopenedProviderCardButton = document.querySelector<HTMLButtonElement>(
      `button[data-provider-action="select"][data-provider-id="${customProviderID}"]`,
    );
    expect(reopenedProviderCardButton).not.toBeNull();
    reopenedProviderCardButton?.click();

    await waitFor(() => {
      const level2 = document.getElementById("models-level2-view");
      return Boolean(level2 && !level2.hasAttribute("hidden"));
    });

    const reopenedTimeoutInput = document.getElementById("models-provider-timeout-ms-input") as HTMLInputElement;
    expect(reopenedTimeoutInput.value).toBe("");

    const reopenedAliasRows = Array.from(document.querySelectorAll<HTMLElement>("#models-provider-aliases-rows .kv-row"));
    const reopenedAliasPairs = reopenedAliasRows
      .map((row) => {
        const key = row.querySelector<HTMLInputElement>('input[data-kv-field="key"]')?.value.trim() ?? "";
        const value = row.querySelector<HTMLInputElement>('input[data-kv-field="value"]')?.value.trim() ?? "";
        return `${key}:${value}`;
      })
      .filter((pair) => pair !== ":");
    expect(reopenedAliasPairs).toEqual([]);

    const reopenedHeaderRows = Array.from(document.querySelectorAll<HTMLElement>("#models-provider-headers-rows .kv-row"));
    const reopenedHeaderPairs = reopenedHeaderRows
      .map((row) => {
        const key = row.querySelector<HTMLInputElement>('input[data-kv-field="key"]')?.value.trim() ?? "";
        const value = row.querySelector<HTMLInputElement>('input[data-kv-field="value"]')?.value.trim() ?? "";
        return `${key}:${value}`;
      })
      .filter((pair) => pair !== ":");
    expect(reopenedHeaderPairs).toEqual([]);
  });

  it("auto saves provider edit changes without submitting form", async () => {
    const providerID = "openai";
    const providerBase = {
      id: providerID,
      name: "OPENAI",
      display_name: "OpenAI",
      openai_compatible: true,
      api_key_prefix: "OPENAI_API_KEY",
      models: [{ id: "gpt-4o-mini", name: "gpt-4o-mini" }],
      allow_custom_base_url: true,
      enabled: true,
      has_api_key: true,
      current_api_key: "sk-***",
      current_base_url: "https://api.openai.com/v1",
    };
    let providerHeaders: Record<string, string> = {};
    let providerAliases: Record<string, string> = {};
    let providerTimeoutMS = 0;
    let savedPayload: { headers?: Record<string, string>; model_aliases?: Record<string, string>; timeout_ms?: number } | null = null;

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [
            {
              ...providerBase,
              headers: providerHeaders,
              timeout_ms: providerTimeoutMS,
              model_aliases: providerAliases,
            },
          ],
          provider_types: [
            { id: "openai", display_name: "openai" },
            { id: "openai-compatible", display_name: "openai Compatible" },
          ],
          defaults: {
            [providerID]: "gpt-4o-mini",
          },
          active_llm: {
            provider_id: providerID,
            model: "gpt-4o-mini",
          },
        });
      }

      if (url.pathname === `/models/${providerID}/config` && method === "PUT") {
        const payload = JSON.parse(String(init?.body ?? "{}")) as {
          headers?: Record<string, string>;
          model_aliases?: Record<string, string>;
          timeout_ms?: number;
        };
        savedPayload = payload;
        providerHeaders = payload.headers ?? {};
        providerAliases = payload.model_aliases ?? {};
        providerTimeoutMS = payload.timeout_ms ?? 0;
        return jsonResponse({
          ...providerBase,
          headers: providerHeaders,
          timeout_ms: providerTimeoutMS,
          model_aliases: providerAliases,
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const settingsToggleButton = document.getElementById("settings-toggle") as HTMLButtonElement;
    settingsToggleButton.click();
    const modelsSectionButton = document.querySelector<HTMLButtonElement>('button[data-settings-section="models"]');
    expect(modelsSectionButton).not.toBeNull();
    modelsSectionButton?.click();

    await waitFor(() =>
      Boolean(
        document.querySelector<HTMLButtonElement>(
          `button[data-provider-action="select"][data-provider-id="${providerID}"]`,
        ),
      ),
    );

    const providerCardButton = document.querySelector<HTMLButtonElement>(
      `button[data-provider-action="select"][data-provider-id="${providerID}"]`,
    );
    expect(providerCardButton).not.toBeNull();
    providerCardButton?.click();

    await waitFor(() => {
      const level2 = document.getElementById("models-level2-view");
      return Boolean(level2 && !level2.hasAttribute("hidden"));
    });

    const timeoutInput = document.getElementById("models-provider-timeout-ms-input") as HTMLInputElement;
    timeoutInput.value = "12000";
    timeoutInput.dispatchEvent(new Event("input", { bubbles: true }));

    const headerRow = document.querySelector<HTMLElement>("#models-provider-headers-rows .kv-row");
    expect(headerRow).not.toBeNull();
    const headerKeyInput = headerRow?.querySelector<HTMLInputElement>('input[data-kv-field="key"]') ?? null;
    const headerValueInput = headerRow?.querySelector<HTMLInputElement>('input[data-kv-field="value"]') ?? null;
    expect(headerKeyInput).not.toBeNull();
    expect(headerValueInput).not.toBeNull();
    if (headerKeyInput && headerValueInput) {
      headerKeyInput.value = "x-auto";
      headerKeyInput.dispatchEvent(new Event("input", { bubbles: true }));
      headerValueInput.value = "true";
      headerValueInput.dispatchEvent(new Event("input", { bubbles: true }));
    }

    await waitFor(() => savedPayload !== null, 4000);
    expect(savedPayload?.timeout_ms).toBe(12000);
    expect(savedPayload?.headers ?? {}).toEqual({ "x-auto": "true" });

    const level2View = document.getElementById("models-level2-view");
    expect(level2View?.hasAttribute("hidden")).toBe(false);
  });
});
