import { describe, expect, it, vi } from "vitest";

import { jsonResponse, mountWebApp, useWebShellFlowFixture, waitFor } from "../support/test-helpers";
import { WebShellFlowPage } from "../support/web-shell-flow.page";

const PLAN_CHAT_ID = "chat-plan-e2e";
const PLAN_SESSION_ID = "session-plan-e2e";
const EXEC_CHAT_ID = "chat-plan-exec-e2e";
const EXEC_SESSION_ID = "session-plan-exec-e2e";

function buildPlanReadyToolResultOutput(): string {
  return JSON.stringify({
    accepted: true,
    plan_state: {
      chat_id: PLAN_CHAT_ID,
      plan_mode_enabled: true,
      plan_mode_state: "planning_ready",
      clarify_asked_count: 0,
      clarify_max_count: 5,
      clarify_unresolved: [],
      plan_spec: {
        goal: "做一下计划",
        scope_in: ["核心路径"],
        scope_out: ["扩展需求"],
        constraints: ["保持最小改动"],
        assumptions: [],
        tasks: [
          {
            id: "plan-1",
            title: "任务一",
            description: "描述",
            depends_on: [],
            status: "pending",
            deliverables: ["文档"],
            verification: ["检查"],
          },
        ],
        acceptance_criteria: ["可执行"],
        risks: [],
        summary_for_execution: "摘要",
        revision: 1,
        updated_at: "2026-02-25T03:55:00Z",
      },
    },
  });
}

function buildPlanOutputSSE(): string {
  const output = buildPlanReadyToolResultOutput();
  return [
    `data: ${JSON.stringify({ type: "step_started", step: 1 })}`,
    `data: ${JSON.stringify({ type: "tool_call", step: 1, tool_call: { name: "output_plan", input: { goal: "做一下计划" } } })}`,
    `data: ${JSON.stringify({ type: "tool_result", step: 1, tool_result: { name: "output_plan", ok: true, output } })}`,
    `data: ${JSON.stringify({ type: "completed", step: 1, reply: "计划已输出" })}`,
    "data: [DONE]",
    "",
  ].join("\n\n");
}

function buildExecutionSSE(): string {
  return [
    `data: ${JSON.stringify({ type: "step_started", step: 1 })}`,
    `data: ${JSON.stringify({ type: "assistant_delta", step: 1, delta: "开始执行任务一" })}`,
    `data: ${JSON.stringify({ type: "completed", step: 1, reply: "开始执行任务一" })}`,
    "data: [DONE]",
    "",
  ].join("\n\n");
}

function parseRequestBody<T>(init?: RequestInit): T {
  return JSON.parse(String(init?.body ?? "{}")) as T;
}

describe("web e2e: plan 模式场景", () => {
  useWebShellFlowFixture();
  const page = new WebShellFlowPage();

  it("output_plan 弹窗选择暂不执行时，保留在规划会话继续修改", async () => {
    let toggleCalls = 0;
    let processCalls = 0;
    let executeCalls = 0;

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
            id: PLAN_CHAT_ID,
            name: "Plan E2E",
            session_id: PLAN_SESSION_ID,
            user_id: "demo-user",
            channel: "console",
            created_at: "2026-02-25T03:00:00Z",
            updated_at: "2026-02-25T03:00:00Z",
            meta: {},
          },
        ]);
      }

      if (url.pathname === `/chats/${PLAN_CHAT_ID}` && method === "GET") {
        return jsonResponse({ messages: [] });
      }

      if (url.pathname === "/agent/plan/toggle" && method === "POST") {
        toggleCalls += 1;
        return jsonResponse({
          chat_id: PLAN_CHAT_ID,
          plan_mode_enabled: true,
          plan_mode_state: "planning_intake",
          clarify_asked_count: 0,
          clarify_max_count: 5,
          clarify_unresolved: [],
        });
      }

      if (url.pathname === "/agent/process" && method === "POST") {
        processCalls += 1;
        return new Response(buildPlanOutputSSE(), {
          status: 200,
          headers: {
            "content-type": "text/event-stream",
          },
        });
      }

      if (url.pathname === "/agent/plan/execute" && method === "POST") {
        executeCalls += 1;
        return jsonResponse({ execution_session_id: EXEC_SESSION_ID });
      }

      if (url.pathname.startsWith("/workspace/files/") && method === "GET") {
        return jsonResponse({ content: "" });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await mountWebApp();

    page.setPlanMode(true);
    await waitFor(() => toggleCalls > 0, 4000);

    page.sendMessage("做一下计划");
    await waitFor(() => processCalls > 0, 4000);

    await waitFor(() => {
      const modal = document.getElementById("request-user-input-modal") as HTMLElement | null;
      return modal !== null && !modal.classList.contains("is-hidden");
    }, 4000);

    const holdOption = Array.from(
      document.querySelectorAll<HTMLButtonElement>("#request-user-input-modal-options .request-user-input-modal-option-btn"),
    ).find((button) => (button.textContent ?? "").includes("先不执行"));
    expect(holdOption).not.toBeUndefined();
    holdOption?.click();

    const submit = document.getElementById("request-user-input-submit-btn") as HTMLButtonElement;
    submit.click();

    await waitFor(() => page.planStageBadgeText().includes("待执行"), 4000);
    expect(executeCalls).toBe(0);
  });

  it("output_plan 弹窗选择立即执行时，会创建新会话并前台开始执行", async () => {
    let toggleCalls = 0;
    let planningProcessCalls = 0;
    let executionProcessCalls = 0;
    let executeCalls = 0;
    let executionChatCreated = false;

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
        const chats = [
          {
            id: PLAN_CHAT_ID,
            name: "Plan E2E",
            session_id: PLAN_SESSION_ID,
            user_id: "demo-user",
            channel: "console",
            created_at: "2026-02-25T03:00:00Z",
            updated_at: "2026-02-25T03:00:00Z",
            meta: {},
          },
        ];
        if (executionChatCreated) {
          chats.unshift({
            id: EXEC_CHAT_ID,
            name: "Plan E2E · Execute",
            session_id: EXEC_SESSION_ID,
            user_id: "demo-user",
            channel: "console",
            created_at: "2026-02-25T03:00:10Z",
            updated_at: "2026-02-25T03:00:10Z",
            meta: {},
          });
        }
        return jsonResponse(chats);
      }

      if (url.pathname === `/chats/${PLAN_CHAT_ID}` && method === "GET") {
        return jsonResponse({ messages: [] });
      }

      if (url.pathname === `/chats/${EXEC_CHAT_ID}` && method === "GET") {
        return jsonResponse({
          messages: [
            {
              id: "msg-seed",
              role: "user",
              type: "message",
              content: [{ type: "text", text: "计划结构化数据" }],
            },
          ],
        });
      }

      if (url.pathname === "/agent/plan/toggle" && method === "POST") {
        toggleCalls += 1;
        return jsonResponse({
          chat_id: PLAN_CHAT_ID,
          plan_mode_enabled: true,
          plan_mode_state: "planning_intake",
          clarify_asked_count: 0,
          clarify_max_count: 5,
          clarify_unresolved: [],
        });
      }

      if (url.pathname === "/agent/plan/execute" && method === "POST") {
        executeCalls += 1;
        executionChatCreated = true;
        return jsonResponse({ execution_session_id: EXEC_SESSION_ID });
      }

      if (url.pathname === "/agent/process" && method === "POST") {
        const payload = parseRequestBody<{
          session_id?: string;
          input?: Array<{ content?: Array<{ text?: string }> }>;
        }>(init);
        if (payload.session_id === PLAN_SESSION_ID) {
          planningProcessCalls += 1;
          return new Response(buildPlanOutputSSE(), {
            status: 200,
            headers: {
              "content-type": "text/event-stream",
            },
          });
        }
        if (payload.session_id === EXEC_SESSION_ID) {
          executionProcessCalls += 1;
          const kickoffText = payload.input?.[0]?.content?.[0]?.text ?? "";
          expect(kickoffText).toContain("计划结构化数据");
          return new Response(buildExecutionSSE(), {
            status: 200,
            headers: {
              "content-type": "text/event-stream",
            },
          });
        }
      }

      if (url.pathname.startsWith("/workspace/files/") && method === "GET") {
        return jsonResponse({ content: "" });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await mountWebApp();

    page.setPlanMode(true);
    await waitFor(() => toggleCalls > 0, 4000);

    page.sendMessage("做一下计划");
    await waitFor(() => planningProcessCalls > 0, 4000);

    await waitFor(() => {
      const modal = document.getElementById("request-user-input-modal") as HTMLElement | null;
      return modal !== null && !modal.classList.contains("is-hidden");
    }, 4000);

    const executeOption = Array.from(
      document.querySelectorAll<HTMLButtonElement>("#request-user-input-modal-options .request-user-input-modal-option-btn"),
    ).find((button) => (button.textContent ?? "").includes("立即执行"));
    expect(executeOption).not.toBeUndefined();
    executeOption?.click();

    const submit = document.getElementById("request-user-input-submit-btn") as HTMLButtonElement;
    submit.click();

    await waitFor(() => executeCalls > 0, 4000);
    await waitFor(() => executionProcessCalls > 0, 4000);
    await waitFor(() => page.chatSessionText().includes(EXEC_SESSION_ID), 4000);
    await waitFor(() => page.assistantMessages().some((node) => (node.textContent ?? "").includes("开始执行任务一")), 4000);

    expect(planningProcessCalls).toBe(1);
  });
});
