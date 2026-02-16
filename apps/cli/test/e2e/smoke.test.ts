import { describe, expect, it, vi } from "vitest";
import { Command } from "commander";
import { mkdtemp, rm, readFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";

import { ApiClient } from "../../src/client/api-client.js";
import { registerAppCommand } from "../../src/commands/app.js";
import { registerChatsCommand } from "../../src/commands/chats.js";
import { registerCronCommand } from "../../src/commands/cron.js";
import { registerModelsCommand } from "../../src/commands/models.js";
import { registerEnvsCommand } from "../../src/commands/envs.js";
import { registerSkillsCommand } from "../../src/commands/skills.js";
import { registerWorkspaceCommand } from "../../src/commands/workspace.js";
import { registerChannelsCommand } from "../../src/commands/channels.js";
import { printError, setOutputJSONMode } from "../../src/io/output.js";

function buildProgram(client: ApiClient): Command {
  const program = new Command();
  program.exitOverride();
  program.name("copaw").option("--json");
  program.hook("preAction", (thisCommand) => {
    setOutputJSONMode(Boolean(thisCommand.optsWithGlobals().json));
  });

  registerAppCommand(program, client);
  registerChatsCommand(program, client);
  registerCronCommand(program, client);
  registerModelsCommand(program, client);
  registerEnvsCommand(program, client);
  registerSkillsCommand(program, client);
  registerWorkspaceCommand(program, client);
  registerChannelsCommand(program, client);
  return program;
}

async function runCLI(argv: string[], fetchImpl: (url: string, init?: RequestInit) => Promise<Response>) {
  vi.stubGlobal("fetch", vi.fn(fetchImpl));
  setOutputJSONMode(false);

  const logs: string[] = [];
  const errors: string[] = [];
  const logSpy = vi.spyOn(console, "log").mockImplementation((...args) => {
    logs.push(args.join(" "));
  });
  const errSpy = vi.spyOn(console, "error").mockImplementation((...args) => {
    errors.push(args.join(" "));
  });

  const client = new ApiClient("http://127.0.0.1:8088");
  const program = buildProgram(client);
  let exitCode = 0;
  try {
    await program.parseAsync(["node", "copaw", ...argv]);
  } catch (err) {
    exitCode = 1;
    printError(err);
  } finally {
    logSpy.mockRestore();
    errSpy.mockRestore();
    vi.unstubAllGlobals();
  }

  return { logs, errors, exitCode };
}

describe("cli e2e", () => {
  it("supports pretty and compact output with global --json", async () => {
    const pretty = await runCLI(["chats", "list"], async () => {
      return new Response(JSON.stringify([{ id: "chat-1" }]), { status: 200 });
    });
    expect(pretty.exitCode).toBe(0);
    expect(pretty.logs[0]).toContain("\n  ");

    const compact = await runCLI(["--json", "chats", "list"], async () => {
      return new Response(JSON.stringify([{ id: "chat-1" }]), { status: 200 });
    });
    expect(compact.exitCode).toBe(0);
    expect(compact.logs[0]).toBe('[{"id":"chat-1"}]');
  });

  it("classifies gateway error by error.code", async () => {
    const result = await runCLI(["chats", "get", "missing"], async () => {
      return new Response(JSON.stringify({ error: { code: "not_found", message: "chat not found" } }), { status: 404 });
    });
    expect(result.exitCode).toBe(1);
    expect(result.errors[0]).toContain("[not_found] chat not found");
    expect(result.errors[1]).toContain("hint:");
  });

  it("covers main command success paths with mocked gateway", async () => {
    const calls: Array<{ method: string; url: string }> = [];
    const run = async (argv: string[]) =>
      runCLI(argv, async (url, init) => {
        calls.push({ method: (init?.method ?? "GET").toUpperCase(), url: String(url) });
        if (String(url).endsWith("/workspace/download")) {
          return new Response(new Uint8Array([1, 2, 3]), { status: 200 });
        }
        return new Response(JSON.stringify({ ok: true }), { status: 200 });
      });

    const outDir = await mkdtemp(join(tmpdir(), "copaw-cli-e2e-"));
    const outFile = join(outDir, "workspace.zip");
    try {
      expect((await run(["app", "start"])).exitCode).toBe(0);
      expect((await run(["chats", "list", "--user-id", "u1", "--channel", "console"])).exitCode).toBe(0);
      expect((await run(["chats", "create", "--session-id", "s1", "--user-id", "u1"])).exitCode).toBe(0);
      expect(
        (
          await run([
            "cron",
            "create",
            "--body",
            "{\"id\":\"j1\",\"name\":\"j1\",\"enabled\":true,\"schedule\":{\"type\":\"interval\",\"cron\":\"1s\"},\"task_type\":\"text\",\"dispatch\":{\"target\":{\"user_id\":\"u1\",\"session_id\":\"s1\"}},\"runtime\":{}}",
          ])
        ).exitCode,
      ).toBe(0);
      expect((await run(["models", "list"])).exitCode).toBe(0);
      expect((await run(["env", "set", "--body", "{\"A\":\"1\"}"])).exitCode).toBe(0);
      expect((await run(["skills", "list"])).exitCode).toBe(0);
      expect((await run(["channels", "set", "console", "--body", "{\"enabled\":true}"])).exitCode).toBe(0);
      expect((await run(["workspace", "download", "--out", outFile])).exitCode).toBe(0);

      const downloaded = await readFile(outFile);
      expect(downloaded.length).toBe(3);
    } finally {
      await rm(outDir, { recursive: true, force: true });
    }

    expect(calls.some((v) => v.method === "GET" && v.url.endsWith("/healthz"))).toBe(true);
    expect(calls.some((v) => v.method === "PUT" && v.url.includes("/config/channels/console"))).toBe(true);
    expect(calls.some((v) => v.method === "POST" && v.url.endsWith("/cron/jobs"))).toBe(true);
  });

  it("covers models alias/custom-provider config with chat chain", async () => {
    const calls: Array<{ method: string; url: string; body: string }> = [];
    const run = async (argv: string[]) =>
      runCLI(argv, async (url, init) => {
        const method = (init?.method ?? "GET").toUpperCase();
        const body = typeof init?.body === "string" ? init.body : "";
        calls.push({ method, url: String(url), body });
        if (String(url).endsWith("/agent/process")) {
          return new Response(JSON.stringify({ output: [{ role: "assistant", content: [{ type: "text", text: "ok" }] }] }), {
            status: 200,
          });
        }
        return new Response(JSON.stringify({ ok: true }), { status: 200 });
      });

    expect(
      (
        await run([
          "models",
          "config",
          "openai",
          "--api-key",
          "sk-openai",
          "--base-url",
          "http://127.0.0.1:19001/v1",
          "--enabled",
          "true",
          "--timeout-ms",
          "32000",
          "--header",
          "X-Tenant:team-a",
          "--model-alias",
          "fast=gpt-4o-mini",
          "--model-alias",
          "reasoning=gpt-4.1-mini",
        ])
      ).exitCode,
    ).toBe(0);
    expect((await run(["models", "active-set", "--provider-id", "openai", "--model", "fast"])).exitCode).toBe(0);
    expect(
      (
        await run([
          "chats",
          "send",
          "--chat-session",
          "s-alias",
          "--user-id",
          "u1",
          "--channel",
          "console",
          "--message",
          "hello alias",
        ])
      ).exitCode,
    ).toBe(0);

    expect(
      (
        await run([
          "models",
          "config",
          "custom-openai",
          "--api-key",
          "sk-custom",
          "--base-url",
          "http://127.0.0.1:19002/v1",
          "--enabled",
          "true",
          "--timeout-ms",
          "15000",
          "--header",
          "X-Workspace:lab",
        ])
      ).exitCode,
    ).toBe(0);
    expect((await run(["models", "active-set", "--provider-id", "custom-openai", "--model", "my-custom-model"])).exitCode).toBe(0);
    expect(
      (
        await run([
          "chats",
          "send",
          "--chat-session",
          "s-custom",
          "--user-id",
          "u1",
          "--channel",
          "console",
          "--message",
          "hello custom",
        ])
      ).exitCode,
    ).toBe(0);

    const openaiConfig = calls.find((call) => call.method === "PUT" && call.url.endsWith("/models/openai/config"));
    expect(openaiConfig).toBeDefined();
    const openaiConfigBody = JSON.parse(openaiConfig?.body ?? "{}");
    expect(openaiConfigBody).toMatchObject({
      api_key: "sk-openai",
      base_url: "http://127.0.0.1:19001/v1",
      enabled: true,
      timeout_ms: 32000,
      headers: { "X-Tenant": "team-a" },
      model_aliases: {
        fast: "gpt-4o-mini",
        reasoning: "gpt-4.1-mini",
      },
    });

    const customConfig = calls.find((call) => call.method === "PUT" && call.url.endsWith("/models/custom-openai/config"));
    expect(customConfig).toBeDefined();
    const customConfigBody = JSON.parse(customConfig?.body ?? "{}");
    expect(customConfigBody).toMatchObject({
      api_key: "sk-custom",
      base_url: "http://127.0.0.1:19002/v1",
      enabled: true,
      timeout_ms: 15000,
      headers: { "X-Workspace": "lab" },
    });

    const activeCalls = calls.filter((call) => call.method === "PUT" && call.url.endsWith("/models/active"));
    expect(activeCalls).toHaveLength(2);
    expect(JSON.parse(activeCalls[0]?.body ?? "{}")).toMatchObject({ provider_id: "openai", model: "fast" });
    expect(JSON.parse(activeCalls[1]?.body ?? "{}")).toMatchObject({ provider_id: "custom-openai", model: "my-custom-model" });

    const processCalls = calls.filter((call) => call.method === "POST" && call.url.endsWith("/agent/process"));
    expect(processCalls).toHaveLength(2);
    expect(JSON.parse(processCalls[0]?.body ?? "{}")).toMatchObject({ session_id: "s-alias", user_id: "u1", channel: "console" });
    expect(JSON.parse(processCalls[1]?.body ?? "{}")).toMatchObject({ session_id: "s-custom", user_id: "u1", channel: "console" });
  });
});
