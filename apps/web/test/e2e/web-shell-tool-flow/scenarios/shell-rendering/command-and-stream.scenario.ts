import { describe, expect, it, vi } from "vitest";

import { jsonResponse, mountWebApp, useWebShellFlowFixture, waitFor } from "../../support/test-helpers";
import { WebShellFlowPage } from "../../support/web-shell-flow.page";

describe("web e2e: shell/tool 渲染场景 - 命令与流式", () => {
  useWebShellFlowFixture();
  const page = new WebShellFlowPage();



  it("发送 /shell 命令时会携带 tool 调用参数", async () => {
    let processCalled = false;
    let sessionID = "";
    let userID = "";
    let channel = "";
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
            id: "chat-shell-1",
            name: "shell",
            session_id: sessionID,
            user_id: userID,
            channel,
            created_at: "2026-02-16T12:00:00Z",
            updated_at: "2026-02-16T12:00:10Z",
            meta: {},
          },
        ]);
      }

      if (url.pathname === "/agent/process" && method === "POST") {
        processCalled = true;
        const payload = JSON.parse(String(init?.body ?? "{}")) as {
          session_id?: string;
          user_id?: string;
          channel?: string;
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
        capturedCommand = payload.biz_params?.tool?.items?.[0]?.command ?? "";
        expect(payload.biz_params?.tool?.name).toBe("shell");

        const encoder = new TextEncoder();
        const stream = new ReadableStream<Uint8Array>({
          start(controller) {
            const firstChunk = [
              `data: ${JSON.stringify({ type: "step_started", step: 1 })}`,
              `data: ${JSON.stringify({ type: "tool_call", step: 1, tool_call: { name: "shell", input: { command: capturedCommand } } })}`,
              "",
            ].join("\n\n");
            controller.enqueue(encoder.encode(firstChunk));
            setTimeout(() => {
              const secondChunk = [
                `data: ${JSON.stringify({ type: "tool_result", step: 1, tool_result: { name: "shell", ok: true, summary: "Listed files in packages\\nListed files in apps\\nRead README.md" } })}`,
                `data: ${JSON.stringify({ type: "assistant_delta", step: 1, delta: "shell done" })}`,
                `data: ${JSON.stringify({ type: "completed", step: 1, reply: "shell done" })}`,
                "data: [DONE]",
                "",
              ].join("\n\n");
              controller.enqueue(encoder.encode(secondChunk));
              controller.close();
            }, 80);
          },
        });
        return new Response(stream, {
          status: 200,
          headers: {
            "content-type": "text/event-stream",
          },
        });
      }

      if (url.pathname === "/chats/chat-shell-1" && method === "GET") {
        const rawToolCall = JSON.stringify({
          type: "tool_call",
          step: 1,
          tool_call: {
            name: "shell",
            input: {
              command: capturedCommand,
            },
          },
        });
        return jsonResponse({
          messages: [
            {
              id: "msg-user",
              role: "user",
              type: "message",
              content: [{ type: "text", text: "/shell printf hello" }],
            },
            {
              id: "msg-assistant",
              role: "assistant",
              type: "message",
              content: [{ type: "text", text: "shell done" }],
              metadata: {
                tool_call_notices: [{ raw: rawToolCall }],
                tool_order: 2,
                text_order: 4,
              },
            },
          ],
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await mountWebApp();
    page.sendMessage("/shell printf hello");

    await waitFor(() => processCalled, 4000);
    expect(sessionID).toMatch(/^[a-z0-9]{6}$/);
    expect(capturedCommand).toBe("printf hello");

    await waitFor(() => {
      const messages = page.assistantMessages();
      return messages.some(
        (item) => {
          const summary = item.querySelector<HTMLElement>(".tool-call-summary")?.textContent ?? "";
          return summary.includes("执行命令：printf hello");
        },
      );
    }, 4000);

    await waitFor(() => {
      const messages = page.assistantMessages();
      return messages.some(
        (item) => {
          const summary = item.querySelector<HTMLElement>(".tool-call-summary")?.textContent ?? "";
          const firstClass = item.firstElementChild?.className ?? "";
          return (
            item.textContent?.includes("shell done") &&
            summary.includes("已浏览 1 个文件，2 个列表") &&
            !summary.includes("printf hello") &&
            firstClass === "tool-call-list"
          );
        },
      );
    }, 4000);
    page.clickFirstChatItem();

    await waitFor(() => {
      const assistant = page.lastAssistantMessage();
      if (!assistant) {
        return false;
      }
      const firstClass = assistant.firstElementChild?.className ?? "";
      const summary = assistant.querySelector<HTMLElement>(".tool-call-summary")?.textContent ?? "";
      return (
        firstClass === "tool-call-list" &&
        summary.includes("执行命令：printf hello")
      );
    }, 4000);
  });



  it("多行 shell 命令摘要只显示省略号", async () => {
    let processCalled = false;
    let capturedCommand = "";
    const multiLineCommand = "cat > /tmp/search_test_fix.go << 'EOF'\npackage plugin\nEOF";

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
        processCalled = true;
        const payload = JSON.parse(String(init?.body ?? "{}")) as {
          biz_params?: {
            tool?: {
              items?: Array<{ command?: string }>;
            };
          };
        };
        capturedCommand = payload.biz_params?.tool?.items?.[0]?.command ?? "";
        const sse = [
          `data: ${JSON.stringify({ type: "tool_call", step: 1, tool_call: { name: "shell", input: { command: capturedCommand } } })}`,
          `data: ${JSON.stringify({ type: "assistant_delta", step: 1, delta: "已执行" })}`,
          `data: ${JSON.stringify({ type: "completed", step: 1, reply: "已执行" })}`,
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
    page.sendMessage(`/shell ${multiLineCommand}`);

    await waitFor(() => processCalled, 4000);
    expect(capturedCommand).toContain("\n");

    await waitFor(() => {
      const summary = page.lastAssistantMessage()?.querySelector<HTMLElement>(".tool-call-summary")
        ?.textContent ?? "";
      return summary.includes("执行命令：...");
    }, 4000);

    const summary = page.lastAssistantMessage()?.querySelector<HTMLElement>(".tool-call-summary")
      ?.textContent ?? "";
    expect(summary).toContain("执行命令：...");
    expect(summary).not.toContain("package plugin");
  });



  it("流式回复期间按阶段切换思考动画", async () => {
    let processCalled = false;
    let emitFirstDelta: (() => void) | null = null;
    let emitToolCall: (() => void) | null = null;
    let emitSecondDelta: (() => void) | null = null;
    let finishStream: (() => void) | null = null;

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
        processCalled = true;
        const encoder = new TextEncoder();
        const stream = new ReadableStream<Uint8Array>({
          start(controller) {
            const firstChunk = [`data: ${JSON.stringify({ type: "step_started", step: 1 })}`, ""].join("\n\n");
            controller.enqueue(encoder.encode(firstChunk));
            emitFirstDelta = () => {
              const deltaChunk = [`data: ${JSON.stringify({ type: "assistant_delta", step: 1, delta: "先说一句" })}`, ""].join(
                "\n\n",
              );
              controller.enqueue(encoder.encode(deltaChunk));
            };
            emitToolCall = () => {
              const toolChunk = [
                `data: ${JSON.stringify({ type: "tool_call", step: 1, tool_call: { name: "shell", input: { command: "echo 1" } } })}`,
                `data: ${JSON.stringify({ type: "tool_result", step: 1, tool_result: { name: "shell", ok: true, summary: "done" } })}`,
                "",
              ].join("\n\n");
              controller.enqueue(encoder.encode(toolChunk));
            };
            emitSecondDelta = () => {
              const deltaChunk = [`data: ${JSON.stringify({ type: "assistant_delta", step: 1, delta: "再补一句" })}`, ""].join(
                "\n\n",
              );
              controller.enqueue(encoder.encode(deltaChunk));
            };
            finishStream = () => {
              const finalChunk = [`data: ${JSON.stringify({ type: "completed", step: 1, reply: "先说一句再补一句" })}`, "data: [DONE]", ""].join(
                "\n\n",
              );
              controller.enqueue(encoder.encode(finalChunk));
              controller.close();
            };
          },
        });
        return new Response(stream, {
          status: 200,
          headers: {
            "content-type": "text/event-stream",
          },
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await mountWebApp();
    page.sendMessage("测试思考动画");

    await waitFor(() => processCalled, 4000);
    await waitFor(() => {
      const thinkingIndicator = page.thinkingIndicator();
      return Boolean(thinkingIndicator && !thinkingIndicator.hidden);
    }, 4000);
    const thinkingIndicator = page.thinkingIndicator() as HTMLElement;
    expect(thinkingIndicator.textContent ?? "").toContain("正在思考");
    expect(thinkingIndicator.parentElement).toBe(document.getElementById("message-list"));
    const pendingAssistantText = page.assistantMessages().map((item) => (item.textContent ?? "").trim());
    expect(pendingAssistantText).not.toContain("...");

    await waitFor(() => typeof emitFirstDelta === "function", 4000);
    emitFirstDelta?.();

    await waitFor(() => {
      const indicator = page.thinkingIndicator();
      return !indicator || indicator.hidden;
    }, 4000);

    await waitFor(() => typeof emitToolCall === "function", 4000);
    emitToolCall?.();

    await waitFor(() => {
      const indicator = page.thinkingIndicator();
      return Boolean(indicator && !indicator.hidden);
    }, 4000);

    await waitFor(() => typeof emitSecondDelta === "function", 4000);
    emitSecondDelta?.();

    await waitFor(() => {
      const indicator = page.thinkingIndicator();
      return !indicator || indicator.hidden;
    }, 4000);

    await waitFor(() => typeof finishStream === "function", 4000);
    finishStream?.();

    await waitFor(() => {
      const assistants = page.assistantMessages();
      const assistant = assistants[assistants.length - 1];
      const text = assistant?.textContent ?? "";
      return text.includes("先说一句") && text.includes("再补一句");
    }, 4000);
  });



  it("按输出顺序渲染：文本和工具调用交错时保持时间线顺序", async () => {
    let processCalled = false;
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
        return jsonResponse([]);
      }

      if (url.pathname === "/agent/process" && method === "POST") {
        processCalled = true;
        const payload = JSON.parse(String(init?.body ?? "{}")) as {
          biz_params?: {
            tool?: {
              items?: Array<{ command?: string }>;
            };
          };
        };
        capturedCommand = payload.biz_params?.tool?.items?.[0]?.command ?? "";
        const sse = [
          `data: ${JSON.stringify({ type: "assistant_delta", step: 1, delta: "先返回文本" })}`,
          `data: ${JSON.stringify({ type: "tool_call", step: 1, tool_call: { name: "shell", input: { command: capturedCommand } } })}`,
          `data: ${JSON.stringify({ type: "assistant_delta", step: 1, delta: "再返回文本" })}`,
          `data: ${JSON.stringify({ type: "completed", step: 1, reply: "先返回文本再返回文本" })}`,
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
    page.sendMessage("/shell printf hello");

    await waitFor(() => processCalled, 4000);
    expect(capturedCommand).toBe("printf hello");

    await waitFor(() => {
      const assistant = page.lastAssistantMessage();
      if (!assistant) {
        return false;
      }
      const summary = assistant.querySelector<HTMLElement>(".tool-call-summary")?.textContent ?? "";
      const text = assistant.textContent ?? "";
      const firstTextIndex = text.indexOf("先返回文本");
      const toolNoticeIndex = text.indexOf("执行命令：printf hello");
      const secondTextIndex = text.indexOf("再返回文本");
      return (
        summary.includes("执行命令：printf hello") &&
        firstTextIndex >= 0 &&
        toolNoticeIndex > firstTextIndex &&
        secondTextIndex > toolNoticeIndex
      );
    }, 4000);
  });
});
