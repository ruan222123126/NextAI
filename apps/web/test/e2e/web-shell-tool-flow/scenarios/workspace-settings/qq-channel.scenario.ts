import { describe, expect, it, vi } from "vitest";

import { jsonResponse, mountWebApp, useWebShellFlowFixture, waitFor } from "../../support/test-helpers";
import { WebShellFlowPage } from "../../support/web-shell-flow.page";

describe("web e2e: workspace 与渠道配置场景 - QQ 渠道配置", () => {
  useWebShellFlowFixture();
  const page = new WebShellFlowPage();



  it("QQ 渠道切到沙箱环境后保存配置会写入沙箱 api_base", async () => {
    let qqConfigLoaded = false;
    let qqConfigSaved = false;
    let workspaceFilesRequested = false;
    let savedAPIBase = "";

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

      if (url.pathname === "/config/channels/qq" && method === "GET") {
        qqConfigLoaded = true;
        return jsonResponse({
          enabled: true,
          app_id: "102857552",
          client_secret: "secret-xxx",
          bot_prefix: "",
          target_type: "c2c",
          target_id: "",
          api_base: "https://api.sgroup.qq.com",
          token_url: "https://bots.qq.com/app/getAppAccessToken",
          timeout_seconds: 8,
        });
      }

      if (url.pathname === "/workspace/files" && method === "GET") {
        workspaceFilesRequested = true;
        return jsonResponse([]);
      }

      if (url.pathname === "/config/channels/qq" && method === "PUT") {
        qqConfigSaved = true;
        const payload = JSON.parse(String(init?.body ?? "{}")) as { api_base?: string };
        savedAPIBase = payload.api_base ?? "";
        return jsonResponse(payload);
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await mountWebApp();

    page.openSettings();
    page.openSettingsSection("channels");

    await waitFor(() => qqConfigLoaded, 4000);
    expect(document.getElementById("channels-level1-view")?.hasAttribute("hidden")).toBe(false);
    expect(document.getElementById("channels-level2-view")?.hasAttribute("hidden")).toBe(true);

    const qqChannelCard = document.querySelector<HTMLButtonElement>('button[data-channel-action="open"][data-channel-id="qq"]');
    expect(qqChannelCard).not.toBeNull();
    qqChannelCard?.click();

    await waitFor(() => document.getElementById("channels-level2-view")?.hasAttribute("hidden") === false, 4000);

    const envSelect = document.getElementById("qq-channel-api-env") as HTMLSelectElement;
    const qqForm = document.getElementById("qq-channel-form") as HTMLFormElement;
    expect(envSelect.value).toBe("production");

    envSelect.value = "sandbox";
    envSelect.dispatchEvent(new Event("change", { bubbles: true }));
    qqForm.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => qqConfigSaved, 4000);
    expect(savedAPIBase).toBe("https://sandbox.api.sgroup.qq.com");
    await waitFor(() => document.getElementById("channels-level1-view")?.hasAttribute("hidden") === false, 4000);
    expect(document.getElementById("channels-level2-view")?.hasAttribute("hidden")).toBe(true);
    expect(workspaceFilesRequested).toBe(false);
  });
});
