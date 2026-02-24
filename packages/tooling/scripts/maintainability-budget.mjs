#!/usr/bin/env node

import { spawnSync } from "node:child_process";
import { appendFileSync, readFileSync, writeFileSync } from "node:fs";

const DEFAULT_WARN_THRESHOLD = 1200;
const DEFAULT_BLOCK_THRESHOLD = 1800;
const DEFAULT_TOP_COUNT = 12;

function printHelp() {
  console.log(`Usage: node packages/tooling/scripts/maintainability-budget.mjs [options]\n\nOptions:\n  --warn-threshold <n>   Warning threshold in lines (default: ${DEFAULT_WARN_THRESHOLD})\n  --block-threshold <n>  Blocking threshold in lines (default: ${DEFAULT_BLOCK_THRESHOLD})\n  --top <n>              Number of largest files to show in baseline (default: ${DEFAULT_TOP_COUNT})\n  --base-ref <ref>       Baseline git ref/sha for changed-file gate\n  --report-path <path>   Write markdown report to file\n  --json-path <path>     Write machine-readable report to file\n  --summary-path <path>  Append markdown report to CI step summary\n  --no-fail-on-blocking  Never exit non-zero even when blockers exist\n  --help                 Show this help\n`);
}

function parsePositiveInt(raw, flagName) {
  const value = Number.parseInt(raw, 10);
  if (!Number.isFinite(value) || value <= 0) {
    throw new Error(`${flagName} expects a positive integer, got: ${raw}`);
  }
  return value;
}

function parseArgs(argv) {
  const options = {
    warnThreshold: DEFAULT_WARN_THRESHOLD,
    blockThreshold: DEFAULT_BLOCK_THRESHOLD,
    topCount: DEFAULT_TOP_COUNT,
    baseRef: "",
    reportPath: "",
    jsonPath: "",
    summaryPath: process.env.GITHUB_STEP_SUMMARY ?? "",
    failOnBlocking: true,
  };

  for (let i = 2; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--") {
      continue;
    }
    if (arg === "--help") {
      printHelp();
      process.exit(0);
    }

    if (arg === "--warn-threshold") {
      options.warnThreshold = parsePositiveInt(argv[i + 1], arg);
      i += 1;
      continue;
    }
    if (arg.startsWith("--warn-threshold=")) {
      options.warnThreshold = parsePositiveInt(arg.slice("--warn-threshold=".length), "--warn-threshold");
      continue;
    }

    if (arg === "--block-threshold") {
      options.blockThreshold = parsePositiveInt(argv[i + 1], arg);
      i += 1;
      continue;
    }
    if (arg.startsWith("--block-threshold=")) {
      options.blockThreshold = parsePositiveInt(arg.slice("--block-threshold=".length), "--block-threshold");
      continue;
    }

    if (arg === "--top") {
      options.topCount = parsePositiveInt(argv[i + 1], arg);
      i += 1;
      continue;
    }
    if (arg.startsWith("--top=")) {
      options.topCount = parsePositiveInt(arg.slice("--top=".length), "--top");
      continue;
    }

    if (arg === "--base-ref") {
      options.baseRef = (argv[i + 1] ?? "").trim();
      i += 1;
      continue;
    }
    if (arg.startsWith("--base-ref=")) {
      options.baseRef = arg.slice("--base-ref=".length).trim();
      continue;
    }

    if (arg === "--report-path") {
      options.reportPath = (argv[i + 1] ?? "").trim();
      i += 1;
      continue;
    }
    if (arg.startsWith("--report-path=")) {
      options.reportPath = arg.slice("--report-path=".length).trim();
      continue;
    }

    if (arg === "--json-path") {
      options.jsonPath = (argv[i + 1] ?? "").trim();
      i += 1;
      continue;
    }
    if (arg.startsWith("--json-path=")) {
      options.jsonPath = arg.slice("--json-path=".length).trim();
      continue;
    }

    if (arg === "--summary-path") {
      options.summaryPath = (argv[i + 1] ?? "").trim();
      i += 1;
      continue;
    }
    if (arg.startsWith("--summary-path=")) {
      options.summaryPath = arg.slice("--summary-path=".length).trim();
      continue;
    }

    if (arg === "--no-fail-on-blocking") {
      options.failOnBlocking = false;
      continue;
    }

    throw new Error(`Unknown argument: ${arg}`);
  }

  if (options.blockThreshold <= options.warnThreshold) {
    throw new Error("--block-threshold must be greater than --warn-threshold");
  }

  return options;
}

function runGitWithEncoding(args, encoding, allowFailure = false) {
  const result = spawnSync("git", args, { encoding });
  if (result.status === 0) {
    return result.stdout;
  }
  if (allowFailure) {
    return null;
  }
  const stderr = typeof result.stderr === "string" ? result.stderr.trim() : "";
  throw new Error(`git ${args.join(" ")} failed: ${stderr || result.error?.message || "unknown error"}`);
}

function runGit(args, allowFailure = false) {
  return runGitWithEncoding(args, "utf8", allowFailure);
}

function runGitBuffer(args, allowFailure = false) {
  return runGitWithEncoding(args, null, allowFailure);
}

function parseNullSeparated(raw) {
  if (!raw) {
    return [];
  }
  return raw.split("\0").filter((part) => part.length > 0);
}

function isAllZeroSha(value) {
  return /^0+$/.test(value);
}

function gitRefToCommit(ref) {
  if (!ref) {
    return "";
  }
  const resolved = runGit(["rev-parse", "--verify", `${ref}^{commit}`], true);
  if (resolved === null) {
    return "";
  }
  return resolved.trim();
}

function resolveBaseRef(explicitRef) {
  const trimmed = explicitRef.trim();
  if (trimmed !== "" && !isAllZeroSha(trimmed)) {
    return gitRefToCommit(trimmed);
  }
  return gitRefToCommit("HEAD~1");
}

function containsNullByte(buffer) {
  const limit = Math.min(buffer.length, 8000);
  for (let i = 0; i < limit; i += 1) {
    if (buffer[i] === 0) {
      return true;
    }
  }
  return false;
}

function countLogicalLines(buffer) {
  if (buffer.length === 0) {
    return 0;
  }
  let lines = 0;
  for (let i = 0; i < buffer.length; i += 1) {
    if (buffer[i] === 10) {
      lines += 1;
    }
  }
  if (buffer[buffer.length - 1] !== 10) {
    lines += 1;
  }
  return lines;
}

function readCurrentLineCount(path) {
  let buffer;
  try {
    buffer = readFileSync(path);
  } catch {
    return null;
  }
  if (containsNullByte(buffer)) {
    return null;
  }
  return countLogicalLines(buffer);
}

function readLineCountAtRef(ref, path) {
  if (!ref) {
    return null;
  }
  const buffer = runGitBuffer(["show", `${ref}:${path}`], true);
  if (buffer === null || containsNullByte(buffer)) {
    return null;
  }
  return countLogicalLines(buffer);
}

function listTrackedFiles() {
  const raw = runGit(["ls-files", "-z"]);
  return parseNullSeparated(raw);
}

function getChangedPaths(baseRef) {
  if (!baseRef) {
    return [];
  }

  let raw = runGit(["diff", "--name-status", "--diff-filter=AMR", "-z", `${baseRef}...HEAD`], true);
  if (raw === null) {
    raw = runGit(["diff", "--name-status", "--diff-filter=AMR", "-z", `${baseRef}..HEAD`], true);
  }
  if (raw === null) {
    return [];
  }

  const fields = parseNullSeparated(raw);
  const changed = [];
  for (let i = 0; i < fields.length; ) {
    const status = fields[i] ?? "";
    i += 1;
    if (status === "") {
      continue;
    }
    if (status.startsWith("R") || status.startsWith("C")) {
      const fromPath = fields[i] ?? "";
      const toPath = fields[i + 1] ?? "";
      i += 2;
      if (toPath !== "") {
        changed.push({ status, path: toPath, fromPath });
      }
      continue;
    }

    const path = fields[i] ?? "";
    i += 1;
    if (path !== "") {
      changed.push({ status, path });
    }
  }

  const deduped = [];
  const seen = new Set();
  for (const item of changed) {
    if (seen.has(item.path)) {
      continue;
    }
    seen.add(item.path);
    deduped.push(item);
  }
  return deduped;
}

function buildSnapshot(paths, warnThreshold, blockThreshold) {
  const entries = [];
  let totalLines = 0;

  for (const path of paths) {
    const lines = readCurrentLineCount(path);
    if (lines === null) {
      continue;
    }
    totalLines += lines;
    entries.push({ path, lines });
  }

  entries.sort((a, b) => {
    if (b.lines !== a.lines) {
      return b.lines - a.lines;
    }
    return a.path.localeCompare(b.path);
  });

  const warnFiles = entries.filter((entry) => entry.lines > warnThreshold);
  const blockFiles = entries.filter((entry) => entry.lines > blockThreshold);

  return {
    entries,
    totalLines,
    warnFiles,
    blockFiles,
    maxFile: entries[0] ?? null,
  };
}

function evaluateChangedFileGate(changed, snapshotByPath, baseRef, warnThreshold, blockThreshold) {
  const warnings = [];
  const blockers = [];
  const legacyTouched = [];
  let consideredTextFiles = 0;

  for (const item of changed) {
    const currentLines = snapshotByPath.get(item.path);
    if (typeof currentLines !== "number") {
      continue;
    }

    consideredTextFiles += 1;
    let baseLines = readLineCountAtRef(baseRef, item.path);
    if (
      baseLines === null &&
      typeof item.fromPath === "string" &&
      item.fromPath !== "" &&
      (item.status.startsWith("R") || item.status.startsWith("C"))
    ) {
      baseLines = readLineCountAtRef(baseRef, item.fromPath);
    }
    const delta = typeof baseLines === "number" ? currentLines - baseLines : null;
    const payload = {
      path: item.path,
      status: item.status,
      baseLines,
      currentLines,
      delta,
    };

    if (currentLines > blockThreshold) {
      const legacyNoGrowth =
        typeof baseLines === "number" && baseLines > blockThreshold && currentLines <= baseLines;
      if (legacyNoGrowth) {
        legacyTouched.push({ ...payload, severity: "block" });
      } else {
        blockers.push(payload);
      }
      continue;
    }

    if (currentLines > warnThreshold) {
      const legacyNoGrowth =
        typeof baseLines === "number" && baseLines > warnThreshold && currentLines <= baseLines;
      if (legacyNoGrowth) {
        legacyTouched.push({ ...payload, severity: "warn" });
      } else {
        warnings.push(payload);
      }
    }
  }

  warnings.sort((a, b) => b.currentLines - a.currentLines || a.path.localeCompare(b.path));
  blockers.sort((a, b) => b.currentLines - a.currentLines || a.path.localeCompare(b.path));
  legacyTouched.sort((a, b) => b.currentLines - a.currentLines || a.path.localeCompare(b.path));

  return {
    consideredTextFiles,
    warnings,
    blockers,
    legacyTouched,
  };
}

function formatNumber(value) {
  return value.toLocaleString("en-US");
}

function formatMaybeNumber(value) {
  return typeof value === "number" ? formatNumber(value) : "n/a";
}

function formatDelta(value) {
  if (typeof value !== "number") {
    return "n/a";
  }
  const prefix = value > 0 ? "+" : "";
  return `${prefix}${formatNumber(value)}`;
}

function shortSha(sha) {
  if (!sha) {
    return "n/a";
  }
  return sha.slice(0, 12);
}

function escapePipe(value) {
  return value.replaceAll("|", "\\|");
}

function pushFileTable(lines, title, files) {
  if (files.length === 0) {
    return;
  }
  lines.push(`### ${title}`);
  lines.push("");
  lines.push("| 文件 | 基线行数 | 当前行数 | Delta | 变更类型 |",
  );
  lines.push("| --- | ---: | ---: | ---: | --- |",
  );
  for (const item of files) {
    lines.push(
      `| \`${escapePipe(item.path)}\` | ${formatMaybeNumber(item.baseLines)} | ${formatNumber(item.currentLines)} | ${formatDelta(item.delta)} | ${item.status} |`,
    );
  }
  lines.push("");
}

function buildMarkdownReport(options, resolvedBaseRef, baseline, gate) {
  const lines = [];
  const budgetResult = gate.blockers.length > 0 ? "FAIL" : "PASS";

  lines.push("## Maintainability Budget Gate");
  lines.push("");
  lines.push(`- 阈值：告警 > ${options.warnThreshold} 行，阻断 > ${options.blockThreshold} 行`);
  lines.push(`- 基线提交：\`${shortSha(resolvedBaseRef)}\``);
  lines.push(`- 结论：**${budgetResult}**`);
  lines.push("");

  lines.push("### 基线快照");
  lines.push("");
  lines.push("| 指标 | 值 |",
  );
  lines.push("| --- | ---: |",
  );
  lines.push(`| 文本文件总数 | ${formatNumber(baseline.entries.length)} |`);
  lines.push(`| 文本总行数 | ${formatNumber(baseline.totalLines)} |`);
  lines.push(`| >${options.warnThreshold} 行文件数 | ${formatNumber(baseline.warnFiles.length)} |`);
  lines.push(`| >${options.blockThreshold} 行文件数 | ${formatNumber(baseline.blockFiles.length)} |`);
  if (baseline.maxFile) {
    lines.push(`| 最大文件 | \`${escapePipe(baseline.maxFile.path)}\` (${formatNumber(baseline.maxFile.lines)} 行) |`);
  }
  lines.push("");

  lines.push(`### Top ${options.topCount} 文件体积基线`);
  lines.push("");
  lines.push("| 排名 | 文件 | 行数 |",
  );
  lines.push("| ---: | --- | ---: |",
  );
  baseline.entries.slice(0, options.topCount).forEach((entry, index) => {
    lines.push(`| ${index + 1} | \`${escapePipe(entry.path)}\` | ${formatNumber(entry.lines)} |`);
  });
  lines.push("");

  lines.push("### 门禁判定（仅变更文件）");
  lines.push("");
  lines.push(`- 参与判定的变更文本文件：${formatNumber(gate.consideredTextFiles)}`);
  lines.push(`- 新增告警文件：${formatNumber(gate.warnings.length)}`);
  lines.push(`- 新增阻断文件：${formatNumber(gate.blockers.length)}`);
  lines.push(`- 历史超限但未增长：${formatNumber(gate.legacyTouched.length)}`);
  lines.push("");

  pushFileTable(lines, "新增阻断文件", gate.blockers);
  pushFileTable(lines, "新增告警文件", gate.warnings);
  pushFileTable(lines, "历史超限但未增长文件（豁免）", gate.legacyTouched);

  if (gate.consideredTextFiles === 0) {
    lines.push("变更集中没有可统计行数的文本文件，门禁仅输出基线。",
    );
    lines.push("");
  }

  return lines.join("\n");
}

function maybeWriteReport(path, content) {
  if (!path) {
    return;
  }
  writeFileSync(path, `${content}\n`, "utf8");
}

function maybeAppendSummary(path, content) {
  if (!path) {
    return;
  }
  appendFileSync(path, `${content}\n`, "utf8");
}

function main() {
  const options = parseArgs(process.argv);
  const resolvedBaseRef = resolveBaseRef(options.baseRef);
  const trackedFiles = listTrackedFiles();
  const baseline = buildSnapshot(trackedFiles, options.warnThreshold, options.blockThreshold);
  const snapshotByPath = new Map(baseline.entries.map((entry) => [entry.path, entry.lines]));
  const changed = getChangedPaths(resolvedBaseRef);
  const gate = evaluateChangedFileGate(
    changed,
    snapshotByPath,
    resolvedBaseRef,
    options.warnThreshold,
    options.blockThreshold,
  );

  const report = buildMarkdownReport(options, resolvedBaseRef, baseline, gate);

  const jsonPayload = {
    generatedAt: new Date().toISOString(),
    thresholds: {
      warn: options.warnThreshold,
      block: options.blockThreshold,
    },
    baseRef: resolvedBaseRef,
    baseline: {
      textFileCount: baseline.entries.length,
      totalLines: baseline.totalLines,
      overWarnCount: baseline.warnFiles.length,
      overBlockCount: baseline.blockFiles.length,
      maxFile: baseline.maxFile,
      topFiles: baseline.entries.slice(0, options.topCount),
    },
    gate,
    result: gate.blockers.length > 0 ? "fail" : "pass",
  };

  console.log(report);
  maybeWriteReport(options.reportPath, report);
  maybeAppendSummary(options.summaryPath, report);

  if (options.jsonPath) {
    writeFileSync(options.jsonPath, `${JSON.stringify(jsonPayload, null, 2)}\n`, "utf8");
  }

  if (options.failOnBlocking && gate.blockers.length > 0) {
    process.exitCode = 1;
  }
}

main();
