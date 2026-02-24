import test from "node:test";
import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { readFile } from "node:fs/promises";

const execFileAsync = promisify(execFile);

test("sdk generated file stays in sync with openapi", async () => {
  const packageDir = new URL("../", import.meta.url);
  await execFileAsync("node", ["scripts/generate-from-openapi.mjs", "--check"], { cwd: packageDir });

  const generated = await readFile(new URL("../src/generated.ts", import.meta.url), "utf8");
  assert.match(generated, /export const OPENAPI_VERSION = "3\.0\.3"/);
});
