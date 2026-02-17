import { Command, InvalidArgumentError } from "commander";
import { ApiClient } from "../client/api-client.js";
import { printResult } from "../io/output.js";
import { t } from "../i18n.js";

export function registerModelsCommand(program: Command, client: ApiClient): void {
  const models = program.command("models").description(t("cli.command.models"));

  models.command("list").action(async () => {
    printResult(await client.get("/models"));
  });

  models
    .command("config")
    .argument("<providerId>")
    .option("--api-key <apiKey>")
    .option("--base-url <baseUrl>")
    .option("--enabled <enabled>", "true|false", parseBooleanOption)
    .option("--timeout-ms <timeoutMS>", ">= 0", parseTimeoutMSOption)
    .option("--header <key:value>", "repeatable custom header", collectHeaderOption, {})
    .option("--model-alias <alias=model>", "repeatable model alias mapping", collectModelAliasOption, {})
    .action(
      async (
        providerId: string,
        opts: {
          apiKey?: string;
          baseUrl?: string;
          enabled?: boolean;
          timeoutMs?: number;
          header?: Record<string, string>;
          modelAlias?: Record<string, string>;
        },
      ) => {
        const payload: Record<string, unknown> = {};
        if (typeof opts.apiKey === "string") {
          payload.api_key = opts.apiKey;
        }
        if (typeof opts.baseUrl === "string") {
          payload.base_url = opts.baseUrl;
        }
        if (typeof opts.enabled === "boolean") {
          payload.enabled = opts.enabled;
        }
        if (typeof opts.timeoutMs === "number") {
          payload.timeout_ms = opts.timeoutMs;
        }
        if (opts.header && Object.keys(opts.header).length > 0) {
          payload.headers = opts.header;
        }
        if (opts.modelAlias && Object.keys(opts.modelAlias).length > 0) {
          payload.model_aliases = opts.modelAlias;
        }
        printResult(await client.put(`/models/${encodeURIComponent(providerId)}/config`, payload));
      },
    );

  models.command("active-get").action(async () => {
    printResult(await client.get("/models/active"));
  });

  models
    .command("active-set")
    .requiredOption("--provider-id <providerId>")
    .requiredOption("--model <model>")
    .action(async (opts: { providerId: string; model: string }) => {
      printResult(
        await client.put("/models/active", {
          provider_id: opts.providerId,
          model: opts.model,
        }),
      );
    });
}

function parseBooleanOption(raw: string): boolean {
  const normalized = raw.trim().toLowerCase();
  if (normalized === "true" || normalized === "1" || normalized === "yes" || normalized === "on") {
    return true;
  }
  if (normalized === "false" || normalized === "0" || normalized === "no" || normalized === "off") {
    return false;
  }
  throw new InvalidArgumentError("enabled must be true|false");
}

function parseTimeoutMSOption(raw: string): number {
  const parsed = Number.parseInt(raw.trim(), 10);
  if (Number.isNaN(parsed) || parsed < 0) {
    throw new InvalidArgumentError("timeout-ms must be an integer >= 0");
  }
  return parsed;
}

function collectHeaderOption(raw: string, previous: Record<string, string>): Record<string, string> {
  const [key, value] = parsePair(raw, ":", "header");
  return {
    ...previous,
    [key]: value,
  };
}

function collectModelAliasOption(raw: string, previous: Record<string, string>): Record<string, string> {
  const [key, value] = parsePair(raw, "=", "model-alias");
  return {
    ...previous,
    [key]: value,
  };
}

function parsePair(raw: string, separator: ":" | "=", label: "header" | "model-alias"): [string, string] {
  const trimmed = raw.trim();
  const index = trimmed.indexOf(separator);
  if (index <= 0 || index >= trimmed.length - 1) {
    throw new InvalidArgumentError(`${label} must be key${separator}value`);
  }
  const key = trimmed.slice(0, index).trim();
  const value = trimmed.slice(index + 1).trim();
  if (key === "" || value === "") {
    throw new InvalidArgumentError(`${label} must be key${separator}value`);
  }
  return [key, value];
}
