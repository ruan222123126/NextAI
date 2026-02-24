import { describe, expect, it, vi } from "vitest";

import { jsonResponse, mountWebApp, useWebShellFlowFixture, waitFor } from "../../support/test-helpers";
import { WebShellFlowPage } from "../../support/web-shell-flow.page";

describe("web e2e: prompt 模板与模式场景 - prompt_mode 路由", () => {
  useWebShellFlowFixture();
  const page = new WebShellFlowPage();

  it("聊天区请求会携带默认 biz_params.prompt_mode=default", async () => {
    let processCalls = 0;
    const capturedModes: string[] = [];

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

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

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/agent/process" && method === "POST") {
        processCalls += 1;
        const payload = JSON.parse(String(init?.body ?? "{}")) as {
          biz_params?: {
            prompt_mode?: string;
          };
        };
        capturedModes.push(payload.biz_params?.prompt_mode ?? "");
        const sse = [
          `data: ${JSON.stringify({ type: "assistant_delta", step: 1, delta: "ok" })}`,
          `data: ${JSON.stringify({ type: "completed", step: 1, reply: "ok" })}`,
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

    await mountWebApp();

    const promptModeSelect = page.promptModeSelect();
    expect(promptModeSelect.value).toBe("default");

    page.sendMessage("first");
    await waitFor(() => processCalls >= 1, 4000);
    page.sendMessage("second");
    await waitFor(() => processCalls >= 2, 4000);

    expect(capturedModes).toEqual(["default", "default"]);
  });

  it("会话 meta 中非法 prompt_mode 会回退为 default", async () => {
    let processCalls = 0;
    const captured: Array<{ sessionID: string; promptMode: string }> = [];
    const chats = [
      {
        id: "chat-invalid",
        name: "Invalid Prompt Mode Chat",
        session_id: "session-invalid",
        user_id: "demo-user",
        channel: "console",
        created_at: "2026-02-17T12:00:00Z",
        updated_at: "2026-02-17T12:00:20Z",
        meta: { prompt_mode: "legacy" },
      },
      {
        id: "chat-default",
        name: "Default Chat",
        session_id: "session-default",
        user_id: "demo-user",
        channel: "console",
        created_at: "2026-02-17T12:00:01Z",
        updated_at: "2026-02-17T12:00:10Z",
        meta: { prompt_mode: "default" },
      },
    ];

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

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

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse(chats);
      }

      if ((url.pathname === "/chats/chat-invalid" || url.pathname === "/chats/chat-default") && method === "GET") {
        return jsonResponse({ messages: [] });
      }

      if (url.pathname === "/agent/process" && method === "POST") {
        processCalls += 1;
        const payload = JSON.parse(String(init?.body ?? "{}")) as {
          session_id?: string;
          biz_params?: {
            prompt_mode?: string;
          };
        };
        captured.push({
          sessionID: payload.session_id ?? "",
          promptMode: payload.biz_params?.prompt_mode ?? "",
        });
        const sse = [
          `data: ${JSON.stringify({ type: "assistant_delta", step: 1, delta: "ok" })}`,
          `data: ${JSON.stringify({ type: "completed", step: 1, reply: "ok" })}`,
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

    await mountWebApp();

    const promptModeSelect = page.promptModeSelect();

    await waitFor(() => page.chatItemButton("chat-invalid") !== null, 4000);
    expect(promptModeSelect.value).toBe("default");

    page.sendMessage("for invalid mode");
    await waitFor(() => processCalls >= 1, 4000);
    expect(captured[0]).toEqual({ sessionID: "session-invalid", promptMode: "default" });

    const defaultChatButton = page.chatItemButton("chat-default");
    expect(defaultChatButton).not.toBeNull();
    defaultChatButton?.click();
    await waitFor(() => promptModeSelect.value === "default", 4000);

    page.sendMessage("for default");
    await waitFor(() => processCalls >= 2, 4000);
    expect(captured[1]).toEqual({ sessionID: "session-default", promptMode: "default" });
  });

  it("新会话默认 prompt_mode=default", async () => {
    let processCalls = 0;
    const capturedModes: string[] = [];

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

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

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/agent/process" && method === "POST") {
        processCalls += 1;
        const payload = JSON.parse(String(init?.body ?? "{}")) as {
          biz_params?: {
            prompt_mode?: string;
          };
        };
        capturedModes.push(payload.biz_params?.prompt_mode ?? "");
        const sse = [
          `data: ${JSON.stringify({ type: "assistant_delta", step: 1, delta: "ok" })}`,
          `data: ${JSON.stringify({ type: "completed", step: 1, reply: "ok" })}`,
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

    await mountWebApp();

    const promptModeSelect = page.promptModeSelect();
    expect(promptModeSelect.value).toBe("default");

    page.clickNewChat();
    expect(promptModeSelect.value).toBe("default");

    page.sendMessage("for new chat");
    await waitFor(() => processCalls >= 1, 4000);
    expect(capturedModes[0]).toBe("default");
  });
});
