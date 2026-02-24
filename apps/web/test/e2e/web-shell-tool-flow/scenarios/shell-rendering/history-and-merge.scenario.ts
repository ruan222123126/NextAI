import { describe, expect, it, vi } from "vitest";

import { jsonResponse, mountWebApp, useWebShellFlowFixture, waitFor } from "../../support/test-helpers";
import { WebShellFlowPage } from "../../support/web-shell-flow.page";

describe("web e2e: shell/tool 渲染场景 - 历史回放与结果合并", () => {
  useWebShellFlowFixture();
  const page = new WebShellFlowPage();



  it("历史回放仅有 tool_call 且助手正文存在时显示暂无执行输出", async () => {
    const viewedPath = "/mnt/Files/NextAI/AGENTS.md";

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
        return jsonResponse([
          {
            id: "chat-history-only-1",
            name: "history-only",
            session_id: "session-history-only",
            user_id: "user-history-only",
            channel: "console",
            created_at: "2026-02-22T18:00:00Z",
            updated_at: "2026-02-22T18:00:10Z",
            meta: {},
          },
        ]);
      }

      if (url.pathname === "/chats/chat-history-only-1" && method === "GET") {
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

    await waitFor(() => {
      const assistant = page.lastAssistantMessage();
      if (!assistant) {
        return false;
      }
      const summary = assistant.querySelector<HTMLElement>(".tool-call-summary")?.textContent ?? "";
      return summary.includes(`查看（${viewedPath}）`);
    }, 4000);

    const persistedAssistant = page.lastAssistantMessage();
    const persistedDetail = persistedAssistant?.querySelector<HTMLElement>(".tool-call-expand-preview")?.textContent ?? "";
    expect(persistedDetail).toContain("暂无执行输出");
    expect(persistedDetail).not.toContain("等待执行输出");
  });



  it("view 工具结果会复用同一条提示，不重复渲染工具提示", async () => {
    let processCalled = false;
    const viewedPath = "/mnt/Files/NextAI/AGENTS.md";
    const viewSummary = `view ${viewedPath} [1-20] (fallback from requested [1-100], total=70)\n1: # AGENTS.md`;

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
            id: "chat-view-result-1",
            name: "view-result",
            session_id: "session-view-result",
            user_id: "user-view-result",
            channel: "console",
            created_at: "2026-02-22T18:00:00Z",
            updated_at: "2026-02-22T18:00:10Z",
            meta: {},
          },
        ]);
      }

      if (url.pathname === "/agent/process" && method === "POST") {
        processCalled = true;
        const sse = [
          `data: ${JSON.stringify({ type: "tool_call", step: 1, tool_call: { name: "view", input: { items: [{ path: viewedPath, start: 1, end: 20 }] } } })}`,
          `data: ${JSON.stringify({ type: "tool_result", step: 1, tool_result: { name: "view", ok: true, summary: viewSummary } })}`,
          `data: ${JSON.stringify({ type: "assistant_delta", step: 1, delta: "查看完成" })}`,
          `data: ${JSON.stringify({ type: "completed", step: 1, reply: "查看完成" })}`,
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
    page.sendMessage("看一下 AGENTS.md");

    await waitFor(() => processCalled, 4000);

    await waitFor(() => {
      const assistant = page.lastAssistantMessage();
      if (!assistant) {
        return false;
      }
      const noticeCount = assistant.querySelectorAll(".tool-call-entry").length;
      const summary = assistant.querySelector<HTMLElement>(".tool-call-summary")?.textContent ?? "";
      const summaryMeta = assistant.querySelector<HTMLElement>(".tool-call-summary-meta")?.textContent ?? "";
      return (
        noticeCount === 1 &&
        summary.includes("已浏览 1 个文件") &&
        summaryMeta.includes("行 1-20") &&
        !summary.includes(`查看（${viewedPath}）`)
      );
    }, 4000);
  });
});
