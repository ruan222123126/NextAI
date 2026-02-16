import { Command } from "commander";
import { ApiClient } from "../client/api-client.js";
import { printResult } from "../io/output.js";

export function registerModelsCommand(program: Command, client: ApiClient): void {
  const models = program.command("models").description("models/providers");

  models.command("list").action(async () => {
    printResult(await client.get("/models"));
  });

  models
    .command("config")
    .argument("<providerId>")
    .option("--api-key <apiKey>")
    .option("--base-url <baseUrl>")
    .action(async (providerId: string, opts: { apiKey?: string; baseUrl?: string }) => {
      printResult(
        await client.put(`/models/${encodeURIComponent(providerId)}/config`, {
          api_key: opts.apiKey,
          base_url: opts.baseUrl,
        }),
      );
    });

  models.command("active-get").action(async () => {
    printResult(await client.get("/models/active"));
  });

  models
    .command("active-set")
    .requiredOption("--provider-id <providerId>")
    .requiredOption("--model <model>")
    .action(async (opts: { providerId: string; model: string }) => {
      printResult(await client.put("/models/active", { provider_id: opts.providerId, model: opts.model }));
    });
}
