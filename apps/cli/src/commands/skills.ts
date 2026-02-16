import { Command } from "commander";
import { ApiClient } from "../client/api-client.js";
import { printResult } from "../io/output.js";
import { t } from "../i18n.js";

export function registerSkillsCommand(program: Command, client: ApiClient): void {
  const skills = program.command("skills").description(t("cli.command.skills"));

  skills.command("list").action(async () => {
    printResult(await client.get("/skills"));
  });

  skills.command("create").requiredOption("--name <name>").requiredOption("--content <content>").action(async (opts: { name: string; content: string }) => {
    printResult(await client.post("/skills", { name: opts.name, content: opts.content }));
  });

  skills.command("enable").argument("<name>").action(async (name: string) => {
    printResult(await client.post(`/skills/${encodeURIComponent(name)}/enable`));
  });

  skills.command("disable").argument("<name>").action(async (name: string) => {
    printResult(await client.post(`/skills/${encodeURIComponent(name)}/disable`));
  });

  skills.command("delete").argument("<name>").action(async (name: string) => {
    printResult(await client.delete(`/skills/${encodeURIComponent(name)}`));
  });
}
