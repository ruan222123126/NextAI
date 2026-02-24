import { describe, expect, it, vi } from "vitest";

import { jsonResponse, mountWebApp, useWebShellFlowFixture, waitFor } from "../../support/test-helpers";
import { WebShellFlowPage } from "../../support/web-shell-flow.page";

describe("web e2e: cron 面板场景 - 导航与保护", () => {
  useWebShellFlowFixture();
  const page = new WebShellFlowPage();



  it("cron 面板可返回聊天页并恢复会话列表可见", async () => {
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
        if (url.searchParams.get("channel") === "qq") {
          return jsonResponse([]);
        }
        return jsonResponse([
          {
            id: "chat-return-1",
            name: "Return target",
            session_id: "session-return-1",
            user_id: "demo-user",
            channel: "console",
            created_at: "2026-02-17T12:00:00Z",
            updated_at: "2026-02-17T12:00:10Z",
            meta: {},
          },
        ]);
      }

      if (url.pathname === "/chats/chat-return-1" && method === "GET") {
        return jsonResponse({
          messages: [
            {
              id: "msg-return-1",
              role: "assistant",
              type: "message",
              content: [{ type: "text", text: "return ok" }],
            },
          ],
        });
      }

      if (url.pathname === "/cron/jobs" && method === "GET") {
        return jsonResponse([]);
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await mountWebApp();

    await waitFor(() => page.queryOne<HTMLButtonElement>("#chat-list .chat-item-btn") !== null, 4000);

    const openCronButton = document.getElementById("chat-cron-toggle") as HTMLButtonElement | null;
    expect(openCronButton).not.toBeNull();
    openCronButton?.click();

    await waitFor(() => page.isPanelActive("cron"), 4000);

    const backToChatButton = document.getElementById("cron-chat-toggle") as HTMLButtonElement | null;
    expect(backToChatButton).not.toBeNull();
    backToChatButton?.click();

    await waitFor(() => page.isPanelActive("chat"), 4000);
    await waitFor(() => page.chatListText().includes("Return target"), 4000);
  });



  it("disables delete action for protected default cron job but keeps enable toggle editable", async () => {
    type CronJobPayload = {
      id: string;
      name: string;
      enabled: boolean;
      schedule: { type: string; cron: string; timezone?: string };
      task_type: "text" | "workflow";
      text?: string;
      dispatch: {
        type?: string;
        channel?: string;
        target: { user_id: string; session_id: string };
        mode?: string;
        meta?: Record<string, unknown>;
      };
      runtime: {
        max_concurrency?: number;
        timeout_seconds?: number;
        misfire_grace_seconds?: number;
      };
      meta?: Record<string, unknown>;
    };

    let updateCallCount = 0;
    let deleteCalled = false;
    let cronJobs: CronJobPayload[] = [
      {
        id: "cron-default",
        name: "你好文本任务",
        enabled: false,
        schedule: { type: "interval", cron: "60s" },
        task_type: "text",
        text: "你好",
        dispatch: {
          type: "channel",
          channel: "console",
          target: {
            user_id: "demo-user",
            session_id: "session-default",
          },
          mode: "",
          meta: {},
        },
        runtime: {
          max_concurrency: 1,
          timeout_seconds: 30,
          misfire_grace_seconds: 0,
        },
        meta: {
          system_default: true,
        },
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
        return jsonResponse([]);
      }

      if (url.pathname === "/cron/jobs" && method === "GET") {
        return jsonResponse(cronJobs);
      }

      const stateMatch = url.pathname.match(/^\/cron\/jobs\/([^/]+)\/state$/);
      if (stateMatch && method === "GET") {
        return jsonResponse({});
      }

      const updateMatch = url.pathname.match(/^\/cron\/jobs\/([^/]+)$/);
      if (updateMatch && method === "PUT") {
        updateCallCount += 1;
        const payload = JSON.parse(String(init?.body ?? "{}")) as CronJobPayload;
        cronJobs = cronJobs.map((job) => (job.id === payload.id ? payload : job));
        return jsonResponse(payload);
      }

      if (updateMatch && method === "DELETE") {
        deleteCalled = true;
        return jsonResponse({ deleted: true });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await mountWebApp();

    document.querySelector<HTMLButtonElement>('button[data-tab="cron"]')?.click();
    await waitFor(() => document.querySelector<HTMLButtonElement>('button[data-cron-delete="cron-default"]') !== null, 4000);

    const deleteButton = document.querySelector<HTMLButtonElement>('button[data-cron-delete="cron-default"]');
    expect(deleteButton).not.toBeNull();
    expect(deleteButton?.disabled).toBe(true);
    expect(deleteButton?.title).toContain("默认定时任务不可删除");
    deleteButton?.click();
    await new Promise((resolve) => setTimeout(resolve, 30));
    expect(confirmSpy).not.toHaveBeenCalled();
    expect(deleteCalled).toBe(false);

    const enabledToggle = document.querySelector<HTMLInputElement>('input[data-cron-toggle-enabled="cron-default"]');
    expect(enabledToggle).not.toBeNull();
    expect(enabledToggle?.checked).toBe(false);
    enabledToggle?.click();
    await waitFor(() => updateCallCount > 0, 4000);
    expect(cronJobs[0]?.enabled).toBe(true);

    confirmSpy.mockRestore();
  });
});
