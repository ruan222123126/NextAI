import { describe, expect, it, vi } from "vitest";

import { jsonResponse, mountWebApp, useWebShellFlowFixture, waitFor } from "../../support/test-helpers";
import { WebShellFlowPage } from "../../support/web-shell-flow.page";

describe("web e2e: 会话列表与搜索场景 - 搜索与 QQ 聚合", () => {
  useWebShellFlowFixture();
  const page = new WebShellFlowPage();



  it("搜索会话时聚合 QQ 渠道历史，且 QQ 拉取不按 user_id 过滤", async () => {
    let consoleChatsRequested = false;
    let qqChatsRequested = false;

    window.localStorage.setItem(
      "nextai.web.chat.settings",
      JSON.stringify({
        apiBase: "http://127.0.0.1:8088",
        apiKey: "",
        userId: "demo-user",
        channel: "qq",
      }),
    );

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
        const channel = url.searchParams.get("channel");
        if (channel === "console") {
          consoleChatsRequested = true;
          expect(url.searchParams.get("user_id")).toBe("demo-user");
          return jsonResponse([
            {
              id: "chat-console-1",
              name: "Console chat",
              session_id: "session-console-1",
              user_id: "demo-user",
              channel: "console",
              created_at: "2026-02-17T12:59:00Z",
              updated_at: "2026-02-17T12:59:10Z",
              meta: {},
            },
          ]);
        }
        if (channel === "qq") {
          qqChatsRequested = true;
          expect(url.searchParams.has("user_id")).toBe(false);
          return jsonResponse([
            {
              id: "chat-qq-1",
              name: "QQ inbound",
              session_id: "qq:c2c:u-c2c",
              user_id: "u-c2c",
              channel: "qq",
              created_at: "2026-02-17T13:00:00Z",
              updated_at: "2026-02-17T13:00:10Z",
              meta: {},
            },
          ]);
        }
        throw new Error(`unexpected /chats channel: ${channel}`);
      }

      if (url.pathname === "/chats/chat-console-1" && method === "GET") {
        return jsonResponse({
          messages: [
            {
              id: "msg-console-user",
              role: "user",
              type: "message",
              content: [{ type: "text", text: "hello console" }],
            },
          ],
        });
      }

      if (url.pathname === "/chats/chat-qq-1" && method === "GET") {
        return jsonResponse({
          messages: [
            {
              id: "msg-qq-user",
              role: "user",
              type: "message",
              content: [{ type: "text", text: "hello qq" }],
            },
          ],
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await mountWebApp();

    await waitFor(() => consoleChatsRequested, 4000);
    await waitFor(() => qqChatsRequested, 4000);
    await waitFor(() => page.chatListText().includes("QQ inbound"), 4000);

    page.clickSearchToggle();

    await waitFor(() => {
      const text = document.querySelector<HTMLElement>("#search-chat-results")?.textContent ?? "";
      return text.includes("qq:c2c:u-c2c");
    }, 4000);
  });



  it("搜索页支持过滤会话并点击进入会话详情", async () => {
    let openedChatID = "";

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
            id: "chat-search-1",
            name: "Alpha one",
            session_id: "session-alpha-1",
            user_id: "demo-user",
            channel: "console",
            created_at: "2026-02-17T14:00:00Z",
            updated_at: "2026-02-17T14:00:10Z",
            meta: {},
          },
          {
            id: "chat-search-2",
            name: "Beta target",
            session_id: "session-alpha-2",
            user_id: "demo-user",
            channel: "console",
            created_at: "2026-02-17T14:01:00Z",
            updated_at: "2026-02-17T14:01:10Z",
            meta: {
              source: "cron",
              cron_job_id: "job-demo",
            },
          },
        ]);
      }

      if (url.pathname === "/chats/chat-search-1" && method === "GET") {
        openedChatID = "chat-search-1";
        return jsonResponse({
          messages: [
            {
              id: "msg-search-1",
              role: "assistant",
              type: "message",
              content: [{ type: "text", text: "alpha history" }],
            },
          ],
        });
      }

      if (url.pathname === "/chats/chat-search-2" && method === "GET") {
        openedChatID = "chat-search-2";
        return jsonResponse({
          messages: [
            {
              id: "msg-search-2",
              role: "assistant",
              type: "message",
              content: [{ type: "text", text: "beta history" }],
            },
          ],
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await mountWebApp();

    await waitFor(() => openedChatID !== "", 4000);

    page.clickSearchToggle();
    page.setSearchQuery("job-demo");

    await waitFor(() => {
      const buttons = page.searchResultButtons();
      return buttons.length === 1 && (buttons[0].textContent ?? "").includes("session-alpha-2");
    }, 4000);

    page.setSearchQuery("session-alpha-1");

    await waitFor(() => {
      const buttons = page.searchResultButtons();
      return buttons.length === 1 && (buttons[0].textContent ?? "").includes("session-alpha-1");
    }, 4000);

    const resultButton = page.queryOne<HTMLButtonElement>("#search-chat-results .search-result-btn");
    expect(resultButton).not.toBeNull();
    resultButton?.click();

    await waitFor(() => openedChatID === "chat-search-1", 4000);
    await waitFor(() => page.isPanelActive("chat"), 4000);
    expect(page.chatSessionText()).toContain("session-alpha-1");
  });
});
