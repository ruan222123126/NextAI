import { Command } from "commander";
import { ApiClient } from "../client/api-client.js";
import { printResult } from "../io/output.js";
import { t } from "../i18n.js";

export function registerCronCommand(program: Command, client: ApiClient): void {
  const cron = program.command("cron").description(t("cli.command.cron"));

  cron.command("list").action(async () => {
    printResult(await client.get("/cron/jobs"));
  });

  cron.command("create").requiredOption("--body <json>").action(async (opts: { body: string }) => {
    printResult(await client.post("/cron/jobs", JSON.parse(opts.body)));
  });

  cron
    .command("update")
    .argument("<jobId>")
    .requiredOption("--body <json>")
    .action(async (jobId: string, opts: { body: string }) => {
      printResult(await client.put(`/cron/jobs/${encodeURIComponent(jobId)}`, JSON.parse(opts.body)));
    });

  cron.command("delete").argument("<jobId>").action(async (jobId: string) => {
    printResult(await client.delete(`/cron/jobs/${encodeURIComponent(jobId)}`));
  });

  cron.command("pause").argument("<jobId>").action(async (jobId: string) => {
    printResult(await client.post(`/cron/jobs/${encodeURIComponent(jobId)}/pause`));
  });

  cron.command("resume").argument("<jobId>").action(async (jobId: string) => {
    printResult(await client.post(`/cron/jobs/${encodeURIComponent(jobId)}/resume`));
  });

  cron.command("run").argument("<jobId>").action(async (jobId: string) => {
    printResult(await client.post(`/cron/jobs/${encodeURIComponent(jobId)}/run`));
  });

  cron.command("state").argument("<jobId>").action(async (jobId: string) => {
    printResult(await client.get(`/cron/jobs/${encodeURIComponent(jobId)}/state`));
  });
}
