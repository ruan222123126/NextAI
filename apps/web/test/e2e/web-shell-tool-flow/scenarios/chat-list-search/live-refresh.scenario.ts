import { describe, expect, it, vi } from "vitest";

import { jsonResponse, mountWebApp, useWebShellFlowFixture, waitFor } from "../../support/test-helpers";
import { WebShellFlowPage } from "../../support/web-shell-flow.page";

describe("web e2e: 会话列表与搜索场景 - 实时刷新", () => {
  useWebShellFlowFixture();
  const page = new WebShellFlowPage();



  it("聊天页打开会话后应自动刷新后台新增消息", async () => {
    let chatsRequestCount = 0;

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
        chatsRequestCount += 1;
        const updatedAt = chatsRequestCount >= 2 ? "2026-02-17T13:00:20Z" : "2026-02-17T13:00:10Z";
        return jsonResponse([
          {
            id: "chat-live-1",
            name: "live-chat",
            session_id: "session-live-1",
            user_id: "demo-user",
            channel: "console",
            created_at: "2026-02-17T13:00:00Z",
            updated_at: updatedAt,
            meta: {},
          },
        ]);
      }

      if (url.pathname === "/chats/chat-live-1" && method === "GET") {
        if (chatsRequestCount >= 2) {
          return jsonResponse({
            messages: [
              {
                id: "msg-live-user",
                role: "user",
                type: "message",
                content: [{ type: "text", text: "你好" }],
              },
              {
                id: "msg-live-assistant",
                role: "assistant",
                type: "message",
                content: [{ type: "text", text: "实时新内容" }],
              },
            ],
          });
        }
        return jsonResponse({
          messages: [
            {
              id: "msg-live-user",
              role: "user",
              type: "message",
              content: [{ type: "text", text: "你好" }],
            },
          ],
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await mountWebApp();

    await waitFor(() => chatsRequestCount >= 2, 6000);
    await waitFor(() => {
      const assistant = document.querySelector<HTMLLIElement>("#message-list .message.assistant:last-child");
      return (assistant?.textContent ?? "").includes("实时新内容");
    }, 6000);
  });
});
