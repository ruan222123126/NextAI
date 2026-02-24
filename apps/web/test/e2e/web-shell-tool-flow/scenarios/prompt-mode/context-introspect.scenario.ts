import { describe, expect, it, vi } from "vitest";

import { jsonResponse, mountWebApp, useWebShellFlowFixture, waitFor } from "../../support/test-helpers";
import { WebShellFlowPage } from "../../support/web-shell-flow.page";

describe("web e2e: prompt 模板与模式场景 - context introspect", () => {
  useWebShellFlowFixture();
  const page = new WebShellFlowPage();



  it("可在设置中切换 prompt context introspect 开关", async () => {
    let systemLayersRequested = 0;
    let workspacePromptFileReads = 0;

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/runtime-config" && method === "GET") {
        return jsonResponse({
          features: {
            prompt_templates: false,
            prompt_context_introspect: false,
          },
        });
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

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/agent/system-layers" && method === "GET") {
        systemLayersRequested += 1;
        return jsonResponse({
          version: "v1",
          layers: [],
          estimated_tokens_total: 123,
        });
      }

      if (url.pathname.startsWith("/workspace/files/") && method === "GET") {
        workspacePromptFileReads += 1;
        return jsonResponse({ content: "" });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await mountWebApp();

    await waitFor(() => workspacePromptFileReads >= 2, 4000);

    const toggle = document.getElementById("feature-prompt-context-introspect") as HTMLInputElement;
    expect(toggle.checked).toBe(false);

    toggle.checked = true;
    toggle.dispatchEvent(new Event("change", { bubbles: true }));

    await waitFor(() => window.localStorage.getItem("nextai.feature.prompt_context_introspect") === "true", 4000);
    await new Promise((resolve) => setTimeout(resolve, 50));

    toggle.checked = false;
    toggle.dispatchEvent(new Event("change", { bubbles: true }));
    await waitFor(() => window.localStorage.getItem("nextai.feature.prompt_context_introspect") === "false", 4000);
    await new Promise((resolve) => setTimeout(resolve, 50));

    toggle.checked = true;
    toggle.dispatchEvent(new Event("change", { bubbles: true }));
    await waitFor(() => window.localStorage.getItem("nextai.feature.prompt_context_introspect") === "true", 4000);
    await waitFor(() => systemLayersRequested > 0, 4000);
    expect(toggle.checked).toBe(true);
  });
});
