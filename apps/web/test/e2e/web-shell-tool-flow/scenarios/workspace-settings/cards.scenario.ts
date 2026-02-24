import { describe, expect, it, vi } from "vitest";

import { jsonResponse, mountWebApp, useWebShellFlowFixture, waitFor } from "../../support/test-helpers";
import { WebShellFlowPage } from "../../support/web-shell-flow.page";

describe("web e2e: workspace 与渠道配置场景 - 卡片与目录树", () => {
  useWebShellFlowFixture();
  const page = new WebShellFlowPage();

  it("工作区仅展示 config 与 prompt 卡片", async () => {
    const filePaths = [
      "config/channels.json",
      "prompts/ai-tools.md",
      "docs/ai/custom-guide.md",
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

      if (url.pathname === "/workspace/files" && method === "GET") {
        return jsonResponse({
          files: filePaths.map((path) => ({ path, kind: "config", size: path.length })),
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await mountWebApp();

    page.openSettings();
    page.openSettingsSection("workspace");

    await waitFor(() => document.querySelector<HTMLButtonElement>('button[data-workspace-action="open-config"]') !== null, 4000);
    expect(document.querySelector<HTMLButtonElement>('button[data-workspace-action="open-config"]')).not.toBeNull();
    expect(document.querySelector<HTMLButtonElement>('button[data-workspace-action="open-prompt"]')).not.toBeNull();
    expect(document.querySelector<HTMLButtonElement>('button[data-workspace-action="open-codex"]')).toBeNull();
    expect(document.getElementById("workspace-level2-codex-view")).toBeNull();
    const workspaceCards = document.querySelectorAll<HTMLButtonElement>('button[data-workspace-action^="open-"]');
    expect(workspaceCards.length).toBe(2);
  });

  it("工作区卡片应支持启用和禁用", async () => {
    const configPath = "config/channels.json";
    const promptPath = "prompts/ai-tools.md";

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

      if (url.pathname === "/workspace/files" && method === "GET") {
        return jsonResponse({
          files: [
            { path: configPath, kind: "config", size: 128 },
            { path: promptPath, kind: "config", size: 256 },
          ],
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await mountWebApp();

    page.openSettings();
    page.openSettingsSection("workspace");

    await waitFor(() => document.querySelector<HTMLButtonElement>('button[data-workspace-action="open-config"]') !== null, 4000);

    const disableButton = document.querySelector<HTMLButtonElement>('button[data-workspace-toggle-card="config"]');
    expect(disableButton).not.toBeNull();
    expect(disableButton?.textContent ?? "").toContain("禁用");
    disableButton?.click();

    await waitFor(() => {
      const openConfigButton = document.querySelector<HTMLButtonElement>('button[data-workspace-action="open-config"]');
      return openConfigButton?.disabled === true;
    }, 4000);

    const persistedAfterDisable = window.localStorage.getItem("nextai.web.chat.settings");
    expect(persistedAfterDisable).not.toBeNull();
    const parsedAfterDisable = JSON.parse(String(persistedAfterDisable)) as {
      workspaceCardEnabled?: { config?: boolean };
    };
    expect(parsedAfterDisable.workspaceCardEnabled?.config).toBe(false);

    const disabledOpenConfigButton = document.querySelector<HTMLButtonElement>('button[data-workspace-action="open-config"]');
    disabledOpenConfigButton?.click();
    expect(document.getElementById("workspace-level2-config-view")?.hasAttribute("hidden")).toBe(true);

    const enableButton = document.querySelector<HTMLButtonElement>('button[data-workspace-toggle-card="config"]');
    expect(enableButton).not.toBeNull();
    expect(enableButton?.textContent ?? "").toContain("启用");
    enableButton?.click();

    await waitFor(() => {
      const openConfigButton = document.querySelector<HTMLButtonElement>('button[data-workspace-action="open-config"]');
      return openConfigButton?.disabled === false;
    }, 4000);
    await waitFor(() => {
      const disableAgainButton = document.querySelector<HTMLButtonElement>('button[data-workspace-toggle-card="config"]');
      return (disableAgainButton?.textContent ?? "").includes("禁用");
    }, 4000);
    const persistedAfterEnable = window.localStorage.getItem("nextai.web.chat.settings");
    expect(persistedAfterEnable).not.toBeNull();
    const parsedAfterEnable = JSON.parse(String(persistedAfterEnable)) as {
      workspaceCardEnabled?: { config?: boolean };
    };
    expect(parsedAfterEnable.workspaceCardEnabled?.config).toBe(true);

    const enabledOpenConfigButton = document.querySelector<HTMLButtonElement>('button[data-workspace-action="open-config"]');
    enabledOpenConfigButton?.click();
    await waitFor(() => document.getElementById("workspace-level2-config-view")?.hasAttribute("hidden") === false, 4000);
  });
});
