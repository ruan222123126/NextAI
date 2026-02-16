import { Command } from "commander";
import { ApiClient } from "../client/api-client.js";
import { printResult } from "../io/output.js";
import { t } from "../i18n.js";

export function registerAppCommand(program: Command, client: ApiClient): void {
  program
    .command("app")
    .description(t("cli.command.app"))
    .command("start")
    .description(t("cli.command.app.start"))
    .action(async () => {
      const health = await client.get<{ ok: boolean }>("/healthz");
      printResult({ connected: health.ok });
    });
}
