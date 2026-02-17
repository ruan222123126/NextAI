import { mkdir, readFile, writeFile } from "node:fs/promises";
import { parse } from "yaml";

const checkMode = process.argv.includes("--check");
const specPath = new URL("../../../packages/contracts/openapi/openapi.yaml", import.meta.url);
const outPath = new URL("../src/generated.ts", import.meta.url);

const raw = await readFile(specPath, "utf8");
const spec = parse(raw);
const paths = spec?.paths ?? {};

const httpMethods = new Set(["get", "post", "put", "delete", "patch", "head", "options"]);
const sortedPathEntries = Object.entries(paths).sort((a, b) => a[0].localeCompare(b[0]));

const pathUnion =
  sortedPathEntries.length === 0
    ? "never"
    : sortedPathEntries.map(([path]) => JSON.stringify(path)).join(" | ");

const operationLines = sortedPathEntries.map(([path, definition]) => {
  const methods = Object.keys(definition ?? {})
    .map((v) => v.toLowerCase())
    .filter((v) => httpMethods.has(v))
    .sort();
  const methodUnion = methods.length === 0 ? "never" : methods.map((v) => JSON.stringify(v)).join(" | ");
  return `  ${JSON.stringify(path)}: ${methodUnion};`;
});

const generated = `/* eslint-disable */
// This file is auto-generated from packages/contracts/openapi/openapi.yaml.
// Do not edit manually.

export const OPENAPI_VERSION = ${JSON.stringify(spec?.openapi ?? "")} as const;

export type APIPath = ${pathUnion};

export type APIMethodByPath = {
${operationLines.join("\n")}
};

export interface APIErrorEnvelope {
  error: {
    code: string;
    message: string;
    details?: unknown;
  };
}
`;

let current = "";
try {
  current = await readFile(outPath, "utf8");
} catch {
  current = "";
}

if (current === generated) {
  console.log("sdk generated file is up to date");
  process.exit(0);
}

if (checkMode) {
  console.error("sdk generated file is outdated; run: pnpm --dir packages/sdk-ts run generate");
  process.exit(1);
}

await mkdir(new URL("../src/", import.meta.url), { recursive: true });
await writeFile(outPath, generated);
console.log("sdk generated file updated");
