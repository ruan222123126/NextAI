#!/usr/bin/env node
import { Command } from "commander";
import { ApiClient } from "./client/api-client.js";
import { registerAppCommand } from "./commands/app.js";
import { registerChatsCommand } from "./commands/chats.js";
import { registerCronCommand } from "./commands/cron.js";
import { registerModelsCommand } from "./commands/models.js";
import { registerEnvsCommand } from "./commands/envs.js";
import { registerSkillsCommand } from "./commands/skills.js";
import { registerWorkspaceCommand } from "./commands/workspace.js";
import { registerChannelsCommand } from "./commands/channels.js";
import { printError, setOutputJSONMode } from "./io/output.js";

const program = new Command();
const client = new ApiClient();

program.name("copaw").description("CoPaw Next CLI").version("0.1.0");
program.option("--json", "机器可读 JSON 输出（紧凑格式）");
program.hook("preAction", (thisCommand) => {
  const enabled = Boolean(thisCommand.optsWithGlobals().json);
  setOutputJSONMode(enabled);
});

registerAppCommand(program, client);
registerChatsCommand(program, client);
registerCronCommand(program, client);
registerModelsCommand(program, client);
registerEnvsCommand(program, client);
registerSkillsCommand(program, client);
registerWorkspaceCommand(program, client);
registerChannelsCommand(program, client);

program.parseAsync(["node", "copaw", ...rewriteLegacyBodyFlag(process.argv.slice(2))]).catch((err) => {
  printError(err);
  process.exit(1);
});

function rewriteLegacyBodyFlag(argv: string[]): string[] {
  const rewritten = [...argv];
  const commandIndex = rewritten.findIndex((token) => token === "cron" || token === "env" || token === "channels");
  if (commandIndex < 0 || commandIndex+1 >= rewritten.length) {
    return rewritten;
  }

  const commandKey = `${rewritten[commandIndex]} ${rewritten[commandIndex + 1]}`;
  const commandsWithLegacyJSON = new Set(["cron create", "cron update", "env set", "channels set"]);
  if (!commandsWithLegacyJSON.has(commandKey)) {
    return rewritten;
  }

  for (let i = commandIndex + 2; i < rewritten.length; i += 1) {
    const token = rewritten[i];
    if (token === "--json" && i + 1 < rewritten.length && looksLikeJSONValue(rewritten[i + 1])) {
      rewritten[i] = "--body";
      return rewritten;
    }
    if (token.startsWith("--json=")) {
      const value = token.slice("--json=".length);
      if (!looksLikeJSONValue(value)) {
        continue;
      }
      rewritten[i] = `--body=${value}`;
      return rewritten;
    }
  }
  return rewritten;
}

function looksLikeJSONValue(raw: string): boolean {
  const text = raw.trim();
  if (text.length < 2) {
    return false;
  }
  const first = text[0];
  const last = text[text.length - 1];
  return (first === "{" && last === "}") || (first === "[" && last === "]");
}
