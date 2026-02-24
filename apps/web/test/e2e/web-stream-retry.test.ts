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

describe("web e2e: stream retry", () => {
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
});
