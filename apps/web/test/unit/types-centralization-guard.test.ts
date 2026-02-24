import { readdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const TEST_DIR = dirname(fileURLToPath(import.meta.url));
const MAIN_SRC_DIR = join(TEST_DIR, "../../src/main");
const TYPES_FILE = join(MAIN_SRC_DIR, "types.ts");

function collectExportedTypeNames(text: string): Set<string> {
  const names = new Set<string>();
  const regex = /^\s*export\s+(?:interface|type)\s+([A-Za-z0-9_]+)/gm;
  let match: RegExpExecArray | null = regex.exec(text);
  while (match) {
    const name = match[1]?.trim();
    if (name) {
      names.add(name);
    }
    match = regex.exec(text);
  }
  return names;
}

function collectDeclaredTypeNames(text: string): string[] {
  const names: string[] = [];
  const regex = /^\s*(?:export\s+)?(?:interface|type)\s+([A-Za-z0-9_]+)/gm;
  let match: RegExpExecArray | null = regex.exec(text);
  while (match) {
    const name = match[1]?.trim();
    if (name) {
      names.push(name);
    }
    match = regex.exec(text);
  }
  return names;
}

describe("types centralization guard", () => {
  it("disallows redeclaring centralized types in main/*-domain.ts", () => {
    const centralizedTypeNames = collectExportedTypeNames(readFileSync(TYPES_FILE, "utf8"));

    const domainFiles = readdirSync(MAIN_SRC_DIR)
      .filter((fileName) => fileName.endsWith("-domain.ts"))
      .sort();

    const violations: string[] = [];
    for (const fileName of domainFiles) {
      const absolutePath = join(MAIN_SRC_DIR, fileName);
      const localTypeNames = collectDeclaredTypeNames(readFileSync(absolutePath, "utf8"));
      const redeclared = localTypeNames.filter((name) => centralizedTypeNames.has(name));
      if (redeclared.length > 0) {
        violations.push(`${fileName}: ${redeclared.join(", ")}`);
      }
    }

    expect(
      violations,
      `Do not redeclare centralized types from src/main/types.ts in main/*-domain.ts:\n${violations.join("\n")}`,
    ).toEqual([]);
  });
});
