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

describe("web e2e: chat switch no flicker", () => {
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
    document.documentElement.innerHTML = "<head></head><body></body>";
  });

  it("switches chat without remounting chat list items", async () => {
    const openedChatIDs: string[] = [];

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
            id: "chat-switch-1",
            name: "Alpha",
            session_id: "session-switch-1",
            user_id: "demo-user",
            channel: "console",
            created_at: "2026-02-20T11:00:00Z",
            updated_at: "2026-02-20T11:00:10Z",
            meta: {},
          },
          {
            id: "chat-switch-2",
            name: "Beta",
            session_id: "session-switch-2",
            user_id: "demo-user",
            channel: "console",
            created_at: "2026-02-20T11:01:00Z",
            updated_at: "2026-02-20T11:01:10Z",
            meta: {},
          },
        ]);
      }

      if (url.pathname === "/chats/chat-switch-1" && method === "GET") {
        openedChatIDs.push("chat-switch-1");
        return jsonResponse({
          messages: [
            {
              id: "msg-alpha",
              role: "assistant",
              type: "message",
              content: [{ type: "text", text: "alpha history" }],
            },
          ],
        });
      }

      if (url.pathname === "/chats/chat-switch-2" && method === "GET") {
        openedChatIDs.push("chat-switch-2");
        return jsonResponse({
          messages: [
            {
              id: "msg-beta",
              role: "assistant",
              type: "message",
              content: [{ type: "text", text: "beta history" }],
            },
          ],
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    await waitFor(() => openedChatIDs.length > 0, 4000);

    const initialChatID = openedChatIDs[0] ?? "";
    const targetChatID = initialChatID === "chat-switch-1" ? "chat-switch-2" : "chat-switch-1";
    const targetSessionID = targetChatID === "chat-switch-1" ? "session-switch-1" : "session-switch-2";
    const targetMessage = targetChatID === "chat-switch-1" ? "alpha history" : "beta history";

    const targetButton = document.querySelector<HTMLButtonElement>(
      `#chat-list .chat-item-btn[data-chat-id="${targetChatID}"]`,
    );
    expect(targetButton).not.toBeNull();

    targetButton?.click();

    await waitFor(() => openedChatIDs[openedChatIDs.length - 1] === targetChatID, 4000);
    await waitFor(() => (document.getElementById("chat-session")?.textContent ?? "").includes(targetSessionID), 4000);
    await waitFor(
      () => (document.querySelector<HTMLLIElement>("#message-list .message.assistant")?.textContent ?? "").includes(targetMessage),
      4000,
    );

    const targetButtonAfter = document.querySelector<HTMLButtonElement>(
      `#chat-list .chat-item-btn[data-chat-id="${targetChatID}"]`,
    );
    expect(targetButtonAfter).toBe(targetButton);

    const activeButtons = Array.from(document.querySelectorAll<HTMLButtonElement>("#chat-list .chat-item-btn.active"));
    expect(activeButtons).toHaveLength(1);
    expect(activeButtons[0]?.dataset.chatId).toBe(targetChatID);

    const latestAssistantMessage = document.querySelector<HTMLLIElement>("#message-list .message.assistant");
    expect(latestAssistantMessage?.textContent ?? "").toContain(targetMessage);
    expect(latestAssistantMessage?.classList.contains("no-anim")).toBe(true);
  });
});
