import { Command } from "commander";
import { ApiClient } from "../client/api-client.js";
import { printResult } from "../io/output.js";
import { t } from "../i18n.js";

export function registerChannelsCommand(program: Command, client: ApiClient): void {
  const channels = program.command("channels").description(t("cli.command.channels"));

  channels.command("list").action(async () => {
    printResult(await client.get("/config/channels"));
  });

  channels.command("types").action(async () => {
    printResult(await client.get("/config/channels/types"));
  });

  channels.command("get").argument("<name>").action(async (name: string) => {
    printResult(await client.get(`/config/channels/${encodeURIComponent(name)}`));
  });

  channels
    .command("set")
    .argument("<name>")
    .requiredOption("--body <json>")
    .action(async (name: string, opts: { body: string }) => {
      printResult(await client.put(`/config/channels/${encodeURIComponent(name)}`, JSON.parse(opts.body)));
    });
}
