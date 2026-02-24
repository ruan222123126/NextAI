import { describe, expect, it, vi } from "vitest";

import { jsonResponse, mountWebApp, useWebShellFlowFixture, waitFor } from "../../support/test-helpers";
import { WebShellFlowPage } from "../../support/web-shell-flow.page";

describe("web e2e: cron 面板场景 - workflow 任务", () => {
  useWebShellFlowFixture();
  const page = new WebShellFlowPage();



  it("Cron 默认 workflow 模式可添加节点连线并提交", async () => {
    type CronWorkflowNodePayload = {
      id: string;
      type: "start" | "text_event" | "delay" | "if_event";
      text?: string;
      delay_seconds?: number;
      if_condition?: string;
    };
    type CronWorkflowEdgePayload = {
      id: string;
      source: string;
      target: string;
    };
    type CronJobPayload = {
      id: string;
      name: string;
      enabled: boolean;
      schedule: { type: string; cron: string; timezone?: string };
      task_type: "text" | "workflow";
      text?: string;
      workflow?: {
        version: "v1";
        nodes: CronWorkflowNodePayload[];
        edges: CronWorkflowEdgePayload[];
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
    };

    let createdPayload: CronJobPayload | null = null;
    let cronJobs: CronJobPayload[] = [];

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

      if (url.pathname === "/cron/jobs" && method === "POST") {
        createdPayload = JSON.parse(String(init?.body ?? "{}")) as CronJobPayload;
        cronJobs = [createdPayload];
        return jsonResponse(createdPayload);
      }

      const stateMatch = url.pathname.match(/^\/cron\/jobs\/([^/]+)\/state$/);
      if (stateMatch && method === "GET") {
        return jsonResponse({
          next_run_at: "2026-02-17T13:40:00Z",
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await mountWebApp();

    document.querySelector<HTMLButtonElement>('button[data-tab="cron"]')?.click();
    await waitFor(() => document.getElementById("cron-create-open-btn") !== null, 4000);

    const openCreateButton = document.getElementById("cron-create-open-btn") as HTMLButtonElement;
    const cronForm = document.getElementById("cron-create-form") as HTMLFormElement;
    const cronID = document.getElementById("cron-id") as HTMLInputElement;
    const cronName = document.getElementById("cron-name") as HTMLInputElement;
    const cronSessionID = document.getElementById("cron-session-id") as HTMLInputElement;
    const cronTaskType = document.getElementById("cron-task-type") as HTMLSelectElement;
    const cronWorkflowSection = document.getElementById("cron-workflow-section") as HTMLElement;
    const cronWorkflowFullscreenButton = document.getElementById("cron-workflow-fullscreen-btn") as HTMLButtonElement;
    const workflowNodesLayer = document.getElementById("cron-workflow-nodes") as HTMLElement;

    openCreateButton.click();
    expect(cronTaskType.value).toBe("workflow");
    expect(cronWorkflowSection.classList.contains("is-pseudo-fullscreen")).toBe(false);
    expect(cronWorkflowFullscreenButton.textContent ?? "").toContain("全屏");
    cronWorkflowFullscreenButton.click();
    expect(cronWorkflowSection.classList.contains("is-pseudo-fullscreen")).toBe(true);
    expect(cronWorkflowFullscreenButton.textContent ?? "").toContain("退出全屏");
    cronWorkflowFullscreenButton.click();
    expect(cronWorkflowSection.classList.contains("is-pseudo-fullscreen")).toBe(false);

    cronID.value = "job-workflow-create";
    cronName.value = "workflow-create";
    cronSessionID.value = "session-workflow-create";

    const openNodeEditorFromContextMenu = async (nodeID: string): Promise<void> => {
      const card = document.querySelector<HTMLElement>(`[data-cron-node-id="${nodeID}"]`);
      expect(card).not.toBeNull();
      card?.dispatchEvent(new MouseEvent("contextmenu", {
        bubbles: true,
        cancelable: true,
        button: 2,
        clientX: 120,
        clientY: 120,
      }));
      await waitFor(
        () => document.querySelector<HTMLButtonElement>(".cron-node-context-menu button[data-cron-node-menu-action='edit']") !== null,
        4000,
      );
      const editButton = document.querySelector<HTMLButtonElement>(
        ".cron-node-context-menu button[data-cron-node-menu-action='edit']",
      );
      expect(editButton).not.toBeNull();
      editButton?.click();
    };

    await openNodeEditorFromContextMenu("node-1");
    const node1TextInput = document.querySelector<HTMLTextAreaElement>("#cron-workflow-node-editor textarea");
    expect(node1TextInput).not.toBeNull();
    if (node1TextInput) {
      node1TextInput.value = "first message";
      node1TextInput.dispatchEvent(new Event("input", { bubbles: true }));
    }

    const addNodeFromCanvasContextMenu = async (
      action: "add-text" | "add-if" | "add-delay",
      clientX: number,
      clientY: number,
    ): Promise<void> => {
      workflowNodesLayer.dispatchEvent(new MouseEvent("contextmenu", {
        bubbles: true,
        cancelable: true,
        button: 2,
        clientX,
        clientY,
      }));
      await waitFor(() => {
        const menu = document.querySelector<HTMLElement>(".cron-node-context-menu");
        const actionButton = document.querySelector<HTMLButtonElement>(
          `.cron-node-context-menu button[data-cron-node-menu-action='${action}']`,
        );
        return Boolean(menu && !menu.classList.contains("is-hidden") && actionButton && !actionButton.hidden);
      }, 4000);
      const addButton = document.querySelector<HTMLButtonElement>(
        `.cron-node-context-menu button[data-cron-node-menu-action='${action}']`,
      );
      expect(addButton).not.toBeNull();
      addButton?.click();
    };

    await addNodeFromCanvasContextMenu("add-if", 560, 300);
    await waitFor(() => document.querySelector<HTMLElement>('[data-cron-node-id="node-2"]') !== null, 4000);

    const node2Card = document.querySelector<HTMLElement>('[data-cron-node-id="node-2"]');
    node2Card?.dispatchEvent(new MouseEvent("contextmenu", {
      bubbles: true,
      cancelable: true,
      button: 2,
      clientX: 132,
      clientY: 132,
    }));
    await waitFor(
      () => document.querySelector<HTMLButtonElement>(".cron-node-context-menu button[data-cron-node-menu-action='delete']") !== null,
      4000,
    );
    const node2DeleteMenuButton = document.querySelector<HTMLButtonElement>(
      ".cron-node-context-menu button[data-cron-node-menu-action='delete']",
    );
    expect(node2DeleteMenuButton?.disabled).toBe(false);
    const node2EditMenuButton = document.querySelector<HTMLButtonElement>(
      ".cron-node-context-menu button[data-cron-node-menu-action='edit']",
    );
    node2EditMenuButton?.click();
    const node2IfInput = document.querySelector<HTMLInputElement>("#cron-workflow-node-editor input[type='text'][placeholder='channel == console']");
    expect(node2IfInput).not.toBeNull();
    if (node2IfInput) {
      node2IfInput.value = "channel == console";
      node2IfInput.dispatchEvent(new Event("input", { bubbles: true }));
      node2IfInput.blur();
    }

    await addNodeFromCanvasContextMenu("add-delay", 720, 420);
    await waitFor(() => document.querySelector<HTMLElement>('[data-cron-node-id="node-3"]') !== null, 4000);

    const node1Out = document.querySelector<HTMLButtonElement>('[data-cron-node-out="node-1"]');
    node1Out?.click();
    const node2In = document.querySelector<HTMLButtonElement>('[data-cron-node-in="node-2"]');
    node2In?.click();

    const node2Out = document.querySelector<HTMLButtonElement>('[data-cron-node-out="node-2"]');
    node2Out?.click();
    const node3In = document.querySelector<HTMLButtonElement>('[data-cron-node-in="node-3"]');
    node3In?.click();

    await waitFor(
      () => document.querySelector<SVGPathElement>('[data-edge-id="edge-node-2-node-3"]') !== null,
      4000,
    );
    const node2ToNode3Edge = document.querySelector<SVGPathElement>('[data-edge-id="edge-node-2-node-3"]');
    node2ToNode3Edge?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "Delete", bubbles: true, cancelable: true }));
    await waitFor(
      () => document.querySelector<SVGPathElement>('[data-edge-id="edge-node-2-node-3"]') === null,
      4000,
    );

    const reconnectNode2Out = document.querySelector<HTMLButtonElement>('[data-cron-node-out="node-2"]');
    reconnectNode2Out?.click();
    const reconnectNode3In = document.querySelector<HTMLButtonElement>('[data-cron-node-in="node-3"]');
    reconnectNode3In?.click();
    await waitFor(
      () => document.querySelector<SVGPathElement>('[data-edge-id="edge-node-2-node-3"]') !== null,
      4000,
    );

    cronForm.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await waitFor(() => createdPayload !== null, 4000);

    expect(createdPayload?.task_type).toBe("workflow");
    expect(createdPayload?.text).toBeUndefined();
    expect(createdPayload?.workflow?.version).toBe("v1");
    expect(createdPayload?.workflow?.nodes.some((item) => item.id === "node-2" && item.type === "if_event")).toBe(true);
    expect(createdPayload?.workflow?.nodes.some((item) => item.id === "node-2" && item.if_condition === "channel == console")).toBe(true);
    expect(createdPayload?.workflow?.nodes.some((item) => item.id === "node-3" && item.type === "delay")).toBe(true);
    expect(
      createdPayload?.workflow?.edges.some((item) => item.source === "node-1" && item.target === "node-2"),
    ).toBe(true);
    expect(
      createdPayload?.workflow?.edges.some((item) => item.source === "node-2" && item.target === "node-3"),
    ).toBe(true);
    expect(
      createdPayload?.workflow?.nodes.some((item) => item.id === "node-1" && item.type === "text_event" && item.text === "first message"),
    ).toBe(true);
  });



  it("编辑 workflow 任务时可回显节点与最近执行明细", async () => {
    type CronJobPayload = {
      id: string;
      name: string;
      enabled: boolean;
      schedule: { type: string; cron: string; timezone?: string };
      task_type: "text" | "workflow";
      workflow?: {
        version: "v1";
        nodes: Array<{
          id: string;
          type: "start" | "text_event" | "delay" | "if_event";
          x: number;
          y: number;
          text?: string;
          delay_seconds?: number;
          if_condition?: string;
        }>;
        edges: Array<{ id: string; source: string; target: string }>;
      };
      dispatch: {
        type?: string;
        channel?: string;
        target: { user_id: string; session_id: string };
      };
      runtime: {
        max_concurrency?: number;
        timeout_seconds?: number;
        misfire_grace_seconds?: number;
      };
    };

    const cronJobs: CronJobPayload[] = [
      {
        id: "job-workflow-edit",
        name: "workflow-edit",
        enabled: true,
        schedule: {
          type: "interval",
          cron: "60s",
        },
        task_type: "workflow",
        workflow: {
          version: "v1",
          nodes: [
            { id: "start", type: "start", x: 80, y: 80 },
            { id: "node-1", type: "text_event", x: 360, y: 80, text: "alpha" },
            { id: "node-2", type: "delay", x: 640, y: 80, delay_seconds: 5 },
          ],
          edges: [
            { id: "edge-start-node-1", source: "start", target: "node-1" },
            { id: "edge-node-1-node-2", source: "node-1", target: "node-2" },
          ],
        },
        dispatch: {
          type: "channel",
          channel: "console",
          target: {
            user_id: "demo-user",
            session_id: "session-workflow-edit",
          },
        },
        runtime: {
          max_concurrency: 1,
          timeout_seconds: 30,
          misfire_grace_seconds: 0,
        },
      },
    ];

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
        return jsonResponse({
          next_run_at: "2026-02-17T14:00:00Z",
          last_execution: {
            run_id: "run-1",
            started_at: "2026-02-17T14:00:00Z",
            finished_at: "2026-02-17T14:00:08Z",
            had_failures: true,
            nodes: [
              {
                node_id: "node-1",
                node_type: "text_event",
                status: "failed",
                continue_on_error: true,
                started_at: "2026-02-17T14:00:01Z",
                finished_at: "2026-02-17T14:00:02Z",
                error: "dispatch failed",
              },
              {
                node_id: "node-2",
                node_type: "delay",
                status: "succeeded",
                continue_on_error: true,
                started_at: "2026-02-17T14:00:03Z",
                finished_at: "2026-02-17T14:00:08Z",
              },
            ],
          },
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await mountWebApp();

    document.querySelector<HTMLButtonElement>('button[data-tab="cron"]')?.click();
    await waitFor(() => document.querySelector<HTMLButtonElement>('button[data-cron-edit="job-workflow-edit"]') !== null, 4000);

    document.querySelector<HTMLButtonElement>('button[data-cron-edit="job-workflow-edit"]')?.click();
    await waitFor(() => document.getElementById("cron-create-modal")?.classList.contains("is-hidden") === false, 4000);

    const cronWorkbench = document.getElementById("cron-workbench") as HTMLElement;
    expect(cronWorkbench.dataset.cronView).toBe("editor");
    const cronTaskType = document.getElementById("cron-task-type") as HTMLSelectElement;
    expect(cronTaskType.value).toBe("workflow");
    expect(document.querySelector<HTMLElement>('[data-cron-node-id="node-2"]')).not.toBeNull();
    expect(document.querySelector<HTMLElement>("#cron-workflow-nodes")?.textContent ?? "").toContain("alpha");

    const executionListText = document.getElementById("cron-workflow-execution-list")?.textContent ?? "";
    expect(executionListText).toContain("node-1");
    expect(executionListText).toContain("文本事件");
    expect(executionListText).toContain("失败");
    expect(executionListText).toContain("dispatch failed");
    expect(executionListText).toContain("node-2");
    expect(executionListText).toContain("延时");
    expect(executionListText).toContain("成功");
  });
});
