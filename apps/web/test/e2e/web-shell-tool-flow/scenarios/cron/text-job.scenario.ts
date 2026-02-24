import { describe, expect, it, vi } from "vitest";

import { jsonResponse, mountWebApp, useWebShellFlowFixture, waitFor } from "../../support/test-helpers";
import { WebShellFlowPage } from "../../support/web-shell-flow.page";

describe("web e2e: cron 面板场景 - text 任务", () => {
  useWebShellFlowFixture();
  const page = new WebShellFlowPage();



  it("Cron text 模式任务创建后支持编辑和删除", async () => {
    type CronJobPayload = {
      id: string;
      name: string;
      enabled: boolean;
      schedule: { type: string; cron: string; timezone?: string };
      task_type: "text" | "workflow";
      text?: string;
      workflow?: {
        version: "v1";
        nodes: Array<Record<string, unknown>>;
        edges: Array<Record<string, unknown>>;
      };
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

    let cronJobs: CronJobPayload[] = [];
    let chatsGetCount = 0;
    let createCalled = false;
    let updateCallCount = 0;
    let deleteCalled = false;
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
        chatsGetCount += 1;
        return jsonResponse([]);
      }

      if (url.pathname === "/cron/jobs" && method === "GET") {
        return jsonResponse(cronJobs);
      }

      if (url.pathname === "/cron/jobs" && method === "POST") {
        createCalled = true;
        const payload = JSON.parse(String(init?.body ?? "{}")) as CronJobPayload;
        cronJobs = [payload];
        return jsonResponse(payload);
      }

      const stateMatch = url.pathname.match(/^\/cron\/jobs\/([^/]+)\/state$/);
      if (stateMatch && method === "GET") {
        return jsonResponse({
          next_run_at: "2026-02-17T13:40:00Z",
        });
      }

      const jobMatch = url.pathname.match(/^\/cron\/jobs\/([^/]+)$/);
      const runMatch = url.pathname.match(/^\/cron\/jobs\/([^/]+)\/run$/);
      if (runMatch && method === "POST") {
        return jsonResponse({ started: true });
      }
      if (jobMatch && method === "PUT") {
        updateCallCount += 1;
        const payload = JSON.parse(String(init?.body ?? "{}")) as CronJobPayload;
        cronJobs = cronJobs.map((item) => (item.id === payload.id ? payload : item));
        return jsonResponse(payload);
      }
      if (jobMatch && method === "DELETE") {
        deleteCalled = true;
        const jobID = decodeURIComponent(jobMatch[1] ?? "");
        const before = cronJobs.length;
        cronJobs = cronJobs.filter((item) => item.id !== jobID);
        return jsonResponse({ deleted: before !== cronJobs.length });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await mountWebApp();

    const cronTabButton = document.querySelector<HTMLButtonElement>('button[data-tab="cron"]');
    cronTabButton?.click();

    await waitFor(() => document.getElementById("cron-create-open-btn") !== null, 4000);

    const openCreateButton = document.getElementById("cron-create-open-btn") as HTMLButtonElement;
    const cronWorkbench = document.getElementById("cron-workbench") as HTMLElement;
    const cronForm = document.getElementById("cron-create-form") as HTMLFormElement;
    const cronID = document.getElementById("cron-id") as HTMLInputElement;
    const cronName = document.getElementById("cron-name") as HTMLInputElement;
    const cronInterval = document.getElementById("cron-interval") as HTMLInputElement;
    const cronSessionID = document.getElementById("cron-session-id") as HTMLInputElement;
    const cronTaskType = document.getElementById("cron-task-type") as HTMLSelectElement;
    const cronText = document.getElementById("cron-text") as HTMLTextAreaElement;
    const cronSubmit = document.getElementById("cron-submit-btn") as HTMLButtonElement;
    const cronTaskTypeContainer = cronTaskType.closest<HTMLDivElement>(".custom-select-container");

    expect(cronWorkbench.dataset.cronView).toBe("jobs");
    expect(cronTaskTypeContainer).not.toBeNull();
    expect(cronTaskTypeContainer?.querySelector<HTMLInputElement>(".options-search-input")).toBeNull();
    openCreateButton.click();
    expect(cronWorkbench.dataset.cronView).toBe("editor");
    expect(openCreateButton.hidden).toBe(true);
    cronTaskType.value = "text";
    cronTaskType.dispatchEvent(new Event("change", { bubbles: true }));
    cronID.value = "job-demo";
    cronName.value = "初始任务";
    cronInterval.value = "60s";
    cronSessionID.value = "session-demo";
    cronText.value = "hello cron";
    cronForm.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => createCalled, 4000);
    await waitFor(() => document.querySelector<HTMLButtonElement>('button[data-cron-edit="job-demo"]') !== null, 4000);
    expect(cronJobs[0]?.task_type).toBe("text");
    expect(cronJobs[0]?.text).toBe("hello cron");
    expect(cronJobs[0]?.dispatch.channel).toBe("console");
    expect(cronJobs[0]?.dispatch.target.user_id).toBe("demo-user");
    const enabledToggle = document.querySelector<HTMLInputElement>('input[data-cron-toggle-enabled="job-demo"]');
    expect(enabledToggle).not.toBeNull();
    expect(enabledToggle?.checked).toBe(true);
    const updatesBeforeToggle = updateCallCount;
    enabledToggle?.click();
    await waitFor(() => updateCallCount > updatesBeforeToggle, 4000);
    expect(cronJobs[0]?.enabled).toBe(false);

    cronJobs = cronJobs.map((job) => ({
      ...job,
      dispatch: {
        ...job.dispatch,
        channel: "qq",
        target: {
          ...job.dispatch.target,
          user_id: "legacy-user",
        },
      },
    }));
    const refreshCronButton = document.getElementById("refresh-cron") as HTMLButtonElement;
    refreshCronButton.click();
    await waitFor(() => document.querySelector<HTMLButtonElement>('button[data-cron-edit="job-demo"]') !== null, 4000);

    const editButton = document.querySelector<HTMLButtonElement>('button[data-cron-edit="job-demo"]');
    editButton?.click();
    expect(cronID.readOnly).toBe(true);
    expect(cronSubmit.textContent ?? "").toContain("PUT /cron/jobs/{job_id}");
    const updatesBeforeEdit = updateCallCount;

    cronName.value = "已更新任务";
    cronForm.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => updateCallCount > updatesBeforeEdit, 4000);
    expect(cronJobs[0]?.name).toBe("已更新任务");
    expect(cronJobs[0]?.dispatch.channel).toBe("console");
    expect(cronJobs[0]?.dispatch.target.user_id).toBe("demo-user");

    const runButton = document.querySelector<HTMLButtonElement>('button[data-cron-run="job-demo"]');
    const chatsCountBeforeRun = chatsGetCount;
    runButton?.click();
    await waitFor(() => chatsGetCount > chatsCountBeforeRun, 4000);

    const deleteButton = document.querySelector<HTMLButtonElement>('button[data-cron-delete="job-demo"]');
    deleteButton?.click();

    await waitFor(() => deleteCalled, 4000);
    expect(confirmSpy).toHaveBeenCalled();
    await waitFor(() => document.querySelector<HTMLButtonElement>('button[data-cron-edit="job-demo"]') === null, 4000);
    confirmSpy.mockRestore();
  });
});
