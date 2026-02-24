import { describe, expect, it, vi } from "vitest";

import { jsonResponse, mountWebApp, useWebShellFlowFixture, waitFor } from "../../support/test-helpers";
import { WebShellFlowPage } from "../../support/web-shell-flow.page";

describe("web e2e: 会话列表与搜索场景 - 删除与切换", () => {
  useWebShellFlowFixture();
  const page = new WebShellFlowPage();



  it("会话列表支持删除，并在删除当前会话后自动切到下一会话", async () => {
    let deleteCalled = false;
    let chats = [
      {
        id: "chat-delete-1",
        name: "Delete target",
        session_id: "session-delete-1",
        user_id: "demo-user",
        channel: "console",
        created_at: "2026-02-17T13:00:00Z",
        updated_at: "2026-02-17T13:00:10Z",
        meta: {},
      },
      {
        id: "chat-delete-2",
        name: "Keep target",
        session_id: "session-delete-2",
        user_id: "demo-user",
        channel: "console",
        created_at: "2026-02-17T13:01:00Z",
        updated_at: "2026-02-17T13:01:10Z",
        meta: {},
      },
    ];
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);

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

      if (url.pathname === "/chats/chat-delete-1" && method === "GET") {
        return jsonResponse({
          messages: [
            {
              id: "msg-delete-1",
              role: "assistant",
              type: "message",
              content: [{ type: "text", text: "第一条会话历史" }],
            },
          ],
        });
      }

      if (url.pathname === "/chats/chat-delete-2" && method === "GET") {
        return jsonResponse({
          messages: [
            {
              id: "msg-delete-2",
              role: "assistant",
              type: "message",
              content: [{ type: "text", text: "第二条会话历史" }],
            },
          ],
        });
      }

      if (url.pathname === "/chats/chat-delete-1" && method === "DELETE") {
        deleteCalled = true;
        chats = chats.filter((chat) => chat.id !== "chat-delete-1");
        return jsonResponse({ deleted: true });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await mountWebApp();

    await waitFor(() => document.querySelectorAll("#chat-list .chat-delete-btn").length === 2, 4000);

    const deleteListItem = Array.from(document.querySelectorAll<HTMLLIElement>("#chat-list .chat-list-item")).find((item) =>
      (item.textContent ?? "").includes("Delete target"),
    );
    const deleteButton = deleteListItem?.querySelector<HTMLButtonElement>(".chat-delete-btn") ?? null;
    expect(deleteButton).not.toBeNull();
    deleteButton?.click();

    await waitFor(() => deleteCalled, 4000);
    await waitFor(() => page.queryAll<HTMLButtonElement>("#chat-list .chat-item-btn").length === 1, 4000);

    expect(confirmSpy).toHaveBeenCalledWith("确认删除会话 session-delete-1？该操作不可恢复。");
    expect(page.chatTitleText().includes("Keep target")).toBe(true);
    expect(page.statusLineText().includes("已删除会话：session-delete-1")).toBe(true);

    confirmSpy.mockRestore();
  });
});
