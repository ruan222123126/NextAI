import { Command } from "commander";
import { ApiClient } from "../client/api-client.js";
import { printResult } from "../io/output.js";

export function registerAppCommand(program: Command, client: ApiClient): void {
  program
    .command("app")
    .description("gateway app commands")
    .command("start")
    .description("print startup hint")
    .action(async () => {
      const health = await client.get<{ ok: boolean }>("/healthz");
      printResult({ connected: health.ok });
    });
}
