import { describe, expect, it, vi } from "vitest";

import { jsonResponse, mountWebApp, useWebShellFlowFixture, waitFor } from "../../support/test-helpers";
import { WebShellFlowPage } from "../../support/web-shell-flow.page";

describe("web e2e: shell/tool 渲染场景 - 工具调用摘要", () => {
  useWebShellFlowFixture();
  const page = new WebShellFlowPage();



  it("view 工具调用文案展示文件路径", async () => {
    let processCalled = false;
    const viewedPath = "/mnt/Files/CodeXR/AGENTS.md";

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
        if (!processCalled) {
          return jsonResponse([]);
        }
        return jsonResponse([
          {
            id: "chat-view-1",
            name: "view",
            session_id: "session-view",
            user_id: "user-view",
            channel: "console",
            created_at: "2026-02-17T12:00:00Z",
            updated_at: "2026-02-17T12:00:10Z",
            meta: {},
          },
        ]);
      }

      if (url.pathname === "/agent/process" && method === "POST") {
        processCalled = true;
        const sse = [
          `data: ${JSON.stringify({ type: "tool_call", step: 1, tool_call: { name: "view", input: { items: [{ path: viewedPath, start: 1, end: 20 }] } } })}`,
          `data: ${JSON.stringify({ type: "assistant_delta", step: 1, delta: "已查看文件" })}`,
          `data: ${JSON.stringify({ type: "completed", step: 1, reply: "已查看文件" })}`,
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

      if (url.pathname === "/chats/chat-view-1" && method === "GET") {
        const rawToolCall = JSON.stringify({
          type: "tool_call",
          step: 1,
          tool_call: {
            name: "view",
            input: {
              items: [{ path: viewedPath, start: 1, end: 20 }],
            },
          },
        });
        return jsonResponse({
          messages: [
            {
              id: "msg-user",
              role: "user",
              type: "message",
              content: [{ type: "text", text: "查看文件" }],
            },
            {
              id: "msg-assistant",
              role: "assistant",
              type: "message",
              content: [{ type: "text", text: "已查看文件" }],
              metadata: {
                tool_call_notices: [{ raw: rawToolCall }],
                tool_order: 1,
                text_order: 2,
              },
            },
          ],
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await mountWebApp();
    page.sendMessage("看一下这个文件");

    await waitFor(() => processCalled, 4000);

    await waitFor(() => {
      const assistant = page.lastAssistantMessage();
      if (!assistant) {
        return false;
      }
      const summary = assistant.querySelector<HTMLElement>(".tool-call-summary")?.textContent ?? "";
      return summary.includes(`查看（${viewedPath}）`);
    }, 4000);
    page.clickFirstChatItem();

    await waitFor(() => {
      const assistant = page.lastAssistantMessage();
      if (!assistant) {
        return false;
      }
      const summary = assistant.querySelector<HTMLElement>(".tool-call-summary")?.textContent ?? "";
      return summary.includes(`查看（${viewedPath}）`);
    }, 4000);
  });



  it("edit 工具调用文案展示文件路径", async () => {
    let processCalled = false;
    const editedPath = "/mnt/Files/CodeXR/apps/web/src/main.ts";

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
        if (!processCalled) {
          return jsonResponse([]);
        }
        return jsonResponse([
          {
            id: "chat-edit-1",
            name: "edit",
            session_id: "session-edit",
            user_id: "user-edit",
            channel: "console",
            created_at: "2026-02-17T12:10:00Z",
            updated_at: "2026-02-17T12:10:10Z",
            meta: {},
          },
        ]);
      }

      if (url.pathname === "/agent/process" && method === "POST") {
        processCalled = true;
        const sse = [
          `data: ${JSON.stringify({ type: "tool_call", step: 1, tool_call: { name: "edit", input: { items: [{ path: editedPath, start: 1, end: 2, text: "new text" }] } } })}`,
          `data: ${JSON.stringify({ type: "assistant_delta", step: 1, delta: "已编辑文件" })}`,
          `data: ${JSON.stringify({ type: "completed", step: 1, reply: "已编辑文件" })}`,
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

      if (url.pathname === "/chats/chat-edit-1" && method === "GET") {
        const rawToolCall = JSON.stringify({
          type: "tool_call",
          step: 1,
          tool_call: {
            name: "edit",
            input: {
              items: [{ path: editedPath, start: 1, end: 2, text: "new text" }],
            },
          },
        });
        return jsonResponse({
          messages: [
            {
              id: "msg-user",
              role: "user",
              type: "message",
              content: [{ type: "text", text: "编辑文件" }],
            },
            {
              id: "msg-assistant",
              role: "assistant",
              type: "message",
              content: [{ type: "text", text: "已编辑文件" }],
              metadata: {
                tool_call_notices: [{ raw: rawToolCall }],
                tool_order: 1,
                text_order: 2,
              },
            },
          ],
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await mountWebApp();
    page.sendMessage("改一下这个文件");

    await waitFor(() => processCalled, 4000);

    await waitFor(() => {
      const assistant = page.lastAssistantMessage();
      if (!assistant) {
        return false;
      }
      const summary = assistant.querySelector<HTMLElement>(".tool-call-summary")?.textContent ?? "";
      return summary.includes(`编辑（${editedPath}）`);
    }, 4000);
    page.clickFirstChatItem();

    await waitFor(() => {
      const assistant = page.lastAssistantMessage();
      if (!assistant) {
        return false;
      }
      const summary = assistant.querySelector<HTMLElement>(".tool-call-summary")?.textContent ?? "";
      return summary.includes(`编辑（${editedPath}）`);
    }, 4000);
  });
});
