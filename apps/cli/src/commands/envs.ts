import { Command } from "commander";
import { ApiClient } from "../client/api-client.js";
import { printResult } from "../io/output.js";

export function registerEnvsCommand(program: Command, client: ApiClient): void {
  const env = program.command("env").description("environment variables");

  env.command("list").action(async () => {
    printResult(await client.get("/envs"));
  });

  env.command("set").requiredOption("--body <json>").action(async (opts: { body: string }) => {
    printResult(await client.put("/envs", JSON.parse(opts.body)));
  });

  env.command("delete").argument("<key>").action(async (key: string) => {
    printResult(await client.delete(`/envs/${encodeURIComponent(key)}`));
  });
}
