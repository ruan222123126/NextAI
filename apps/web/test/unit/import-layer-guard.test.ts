import { readdirSync, readFileSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const TEST_DIR = dirname(fileURLToPath(import.meta.url));
const MAIN_SRC_DIR = join(TEST_DIR, "../../src/main");

function collectImportSpecifiers(source: string): string[] {
  const specifiers: string[] = [];
  const regex = /^\s*import(?:\s+type)?(?:[\s\S]*?\s+from\s+)?["']([^"']+)["'];?/gm;
  let match: RegExpExecArray | null = regex.exec(source);
  while (match) {
    const specifier = match[1]?.trim();
    if (specifier) {
      specifiers.push(specifier);
    }
    match = regex.exec(source);
  }
  return specifiers;
}

function normalizeTargetRelativePath(fileName: string, specifier: string): string | null {
  if (!specifier.startsWith(".")) {
    return null;
  }
  const sourcePath = join(MAIN_SRC_DIR, fileName);
  const normalizedSpecifier = specifier.endsWith(".js") ? `${specifier.slice(0, -3)}.ts` : specifier;
  const resolvedPath = resolve(dirname(sourcePath), normalizedSpecifier);
  return relative(MAIN_SRC_DIR, resolvedPath).replaceAll("\\", "/");
}

describe("main import layer guard", () => {
  it("disallows cross-layer imports in main/*-domain.ts and main/*-feature.ts", () => {
    const files = readdirSync(MAIN_SRC_DIR)
      .filter((name) => name.endsWith(".ts"))
      .sort();

    const violations: string[] = [];

    for (const fileName of files) {
      const isDomain = fileName.endsWith("-domain.ts");
      const isFeature = fileName.endsWith("-feature.ts");
      if (!isDomain && !isFeature) {
        continue;
      }

      const source = readFileSync(join(MAIN_SRC_DIR, fileName), "utf8");
      const specifiers = collectImportSpecifiers(source);

      for (const specifier of specifiers) {
        const target = normalizeTargetRelativePath(fileName, specifier);
        if (!target) {
          continue;
        }

        if (target.startsWith("../")) {
          violations.push(`${fileName}: disallow cross-layer relative import "${specifier}"`);
          continue;
        }

        if (isDomain && target.endsWith("-feature.ts")) {
          violations.push(`${fileName}: domain must not import feature module "${specifier}"`);
          continue;
        }

        if (isFeature && target.endsWith("-feature.ts") && target !== "feature-contract.ts") {
          violations.push(`${fileName}: feature must not import peer feature module "${specifier}"`);
        }
      }
    }

    expect(
      violations,
      `main layer import guard violations:\n${violations.join("\n")}`,
    ).toEqual([]);
  });
});
