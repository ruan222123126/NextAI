import { describe, expect, it, vi } from "vitest";

import { jsonResponse, mountWebApp, useWebShellFlowFixture, waitFor } from "../../support/test-helpers";
import { WebShellFlowPage } from "../../support/web-shell-flow.page";

describe("web e2e: prompt 模板与模式场景 - 模板展开", () => {
  useWebShellFlowFixture();
  const page = new WebShellFlowPage();



  it("开启 prompt 模板后，/prompts 命令会先展开再发送", async () => {
    window.localStorage.setItem("nextai.feature.prompt_templates", "true");
    let processCalled = false;
    let sessionID = "";
    let userID = "";
    let channel = "";
    let expandedText = "";
    let capturedCommand = "";

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
            id: "chat-template-1",
            name: "template",
            session_id: sessionID,
            user_id: userID,
            channel,
            created_at: "2026-02-16T12:00:00Z",
            updated_at: "2026-02-16T12:00:10Z",
            meta: {},
          },
        ]);
      }

      if (url.pathname === `/workspace/files/${encodeURIComponent("prompts/quick-task.md")}` && method === "GET") {
        return jsonResponse({ content: "/shell $CMD" });
      }

      if (url.pathname.startsWith("/workspace/files/") && method === "GET") {
        return jsonResponse({ content: "" });
      }

      if (url.pathname === "/agent/process" && method === "POST") {
        processCalled = true;
        const payload = JSON.parse(String(init?.body ?? "{}")) as {
          session_id?: string;
          user_id?: string;
          channel?: string;
          input?: Array<{
            content?: Array<{ text?: string }>;
          }>;
          biz_params?: {
            tool?: {
              name?: string;
              items?: Array<{ command?: string }>;
            };
          };
        };
        sessionID = payload.session_id ?? "";
        userID = payload.user_id ?? "";
        channel = payload.channel ?? "";
        expandedText = payload.input?.[0]?.content?.[0]?.text ?? "";
        capturedCommand = payload.biz_params?.tool?.items?.[0]?.command ?? "";
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

      if (url.pathname === "/chats/chat-template-1" && method === "GET") {
        return jsonResponse({
          messages: [
            {
              id: "msg-user",
              role: "user",
              type: "message",
              content: [{ type: "text", text: expandedText }],
            },
            {
              id: "msg-assistant",
              role: "assistant",
              type: "message",
              content: [{ type: "text", text: "ok" }],
            },
          ],
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await mountWebApp();
    page.sendMessage("/prompts:quick-task CMD=printf_hello");

    await waitFor(() => processCalled, 4000);
    expect(expandedText).toBe("/shell printf_hello");
    expect(capturedCommand).toBe("printf_hello");
  });



  it("check-fix 模板会按候选路径回退加载", async () => {
    window.localStorage.setItem("nextai.feature.prompt_templates", "true");
    let processCalled = false;
    let sessionID = "";
    let userID = "";
    let channel = "";
    let expandedText = "";
    const requestedTemplatePaths: string[] = [];
    const fallbackTemplatePaths = new Set([
      "prompts/check-fix.md",
      "prompt/check-fix.md",
    ]);

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
            id: "chat-check-fix-1",
            name: "check-fix",
            session_id: sessionID,
            user_id: userID,
            channel,
            created_at: "2026-02-16T12:00:00Z",
            updated_at: "2026-02-16T12:00:10Z",
            meta: {},
          },
        ]);
      }

      if (url.pathname.startsWith("/workspace/files/") && method === "GET") {
        const workspacePath = decodeURIComponent(url.pathname.slice("/workspace/files/".length));
        if (fallbackTemplatePaths.has(workspacePath)) {
          requestedTemplatePaths.push(workspacePath);
          if (workspacePath === "prompt/check-fix.md") {
            return jsonResponse({ content: "# 修复影响检查" });
          }
          return jsonResponse({ error: { code: "not_found", message: "not found" } }, 404);
        }
        return jsonResponse({ content: "" });
      }

      if (url.pathname === "/agent/process" && method === "POST") {
        processCalled = true;
        const payload = JSON.parse(String(init?.body ?? "{}")) as {
          session_id?: string;
          user_id?: string;
          channel?: string;
          input?: Array<{
            content?: Array<{ text?: string }>;
          }>;
        };
        sessionID = payload.session_id ?? "";
        userID = payload.user_id ?? "";
        channel = payload.channel ?? "";
        expandedText = payload.input?.[0]?.content?.[0]?.text ?? "";
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

      if (url.pathname === "/chats/chat-check-fix-1" && method === "GET") {
        return jsonResponse({
          messages: [
            {
              id: "msg-user",
              role: "user",
              type: "message",
              content: [{ type: "text", text: expandedText }],
            },
            {
              id: "msg-assistant",
              role: "assistant",
              type: "message",
              content: [{ type: "text", text: "ok" }],
            },
          ],
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await mountWebApp();
    page.sendMessage("/prompts:check-fix");

    await waitFor(() => processCalled, 4000);
    expect(expandedText).toContain("修复影响检查");
    expect(requestedTemplatePaths).toEqual([
      "prompts/check-fix.md",
      "prompt/check-fix.md",
    ]);
  });



  it("prompt 模板缺少必填参数时会阻断发送", async () => {
    window.localStorage.setItem("nextai.feature.prompt_templates", "true");
    let processCalled = false;

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

      if (url.pathname === `/workspace/files/${encodeURIComponent("prompts/quick-task.md")}` && method === "GET") {
        return jsonResponse({ content: "hello $NAME" });
      }

      if (url.pathname.startsWith("/workspace/files/") && method === "GET") {
        return jsonResponse({ content: "" });
      }

      if (url.pathname === "/agent/process" && method === "POST") {
        processCalled = true;
        return new Response("unexpected", { status: 500 });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await mountWebApp();
    page.sendMessage("/prompts:quick-task");

    await waitFor(() => page.statusLineText().includes("missing prompt arguments"), 4000);
    expect(processCalled).toBe(false);
  });
});
