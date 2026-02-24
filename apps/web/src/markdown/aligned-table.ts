interface ParsedAlignedTable {
  headers: string[];
  rows: string[][];
  nextIndex: number;
}

export function transformAlignedTextTables(markdown: string): string {
  const lines = markdown.split("\n");
  const output: string[] = [];
  let index = 0;
  let inCodeFence = false;

  while (index < lines.length) {
    const line = lines[index] ?? "";
    if (/^\s*```/.test(line)) {
      inCodeFence = !inCodeFence;
      output.push(line);
      index += 1;
      continue;
    }
    if (inCodeFence) {
      output.push(line);
      index += 1;
      continue;
    }

    const alignedTable = parseAlignedTextTable(lines, index);
    if (!alignedTable) {
      output.push(line);
      index += 1;
      continue;
    }

    output.push(...serializeAlignedTableAsMarkdownTable(alignedTable.headers, alignedTable.rows));
    index = alignedTable.nextIndex;
  }

  return output.join("\n");
}

function parseAlignedTextTable(lines: string[], startIndex: number): ParsedAlignedTable | null {
  if (startIndex + 2 >= lines.length) {
    return null;
  }
  const headerLine = lines[startIndex] ?? "";
  if (isMarkdownBlockStarter(headerLine, startIndex, lines)) {
    return null;
  }

  const headers = splitAlignedTableRow(headerLine);
  if (headers.length < 2) {
    return null;
  }

  const rows: string[][] = [];
  let index = startIndex + 1;
  while (index < lines.length) {
    const rowLine = lines[index] ?? "";
    if (rowLine.trim() === "") {
      break;
    }
    if (isMarkdownBlockStarter(rowLine, index, lines)) {
      break;
    }
    const cells = splitAlignedTableRow(rowLine);
    if (cells.length !== headers.length) {
      break;
    }
    rows.push(cells);
    index += 1;
  }

  if (rows.length < 2) {
    return null;
  }

  return {
    headers,
    rows,
    nextIndex: index,
  };
}

function serializeAlignedTableAsMarkdownTable(headers: string[], rows: string[][]): string[] {
  const tableLines = [`| ${headers.map(escapeTableCell).join(" | ")} |`, `| ${headers.map(() => ":---").join(" | ")} |`];
  for (const row of rows) {
    tableLines.push(`| ${row.map(escapeTableCell).join(" | ")} |`);
  }
  return tableLines;
}

function escapeTableCell(value: string): string {
  return value.replace(/\|/g, "\\|");
}

function isMarkdownHorizontalRule(line: string): boolean {
  return /^\s{0,3}([-*_])(\s*\1){2,}\s*$/.test(line);
}

function isTableHeaderLine(line: string): boolean {
  return line.includes("|");
}

function isTableDividerLine(line: string): boolean {
  const cells = splitTableRow(line);
  if (cells.length < 2) {
    return false;
  }
  return cells.every((cell) => /^:?-{3,}:?$/.test(cell.trim()));
}

function splitTableRow(line: string): string[] {
  const trimmed = line.trim();
  const inner = trimmed.replace(/^\|/, "").replace(/\|$/, "");
  return inner.split("|").map((cell) => cell.trim());
}

function splitAlignedTableRow(line: string): string[] {
  const trimmed = line.trim();
  if (trimmed === "" || !/(?:\t+|[ \u3000]{2,})/.test(trimmed)) {
    return [];
  }
  const cells = trimmed.split(/(?:\t+|[ \u3000]{2,})/g).map((cell) => cell.trim());
  if (cells.some((cell) => cell === "")) {
    return [];
  }
  return cells;
}

function isMarkdownBlockStarter(line: string, index: number, lines: string[]): boolean {
  if (
    /^\s*```/.test(line) ||
    /^\s*(#{1,6})\s+/.test(line) ||
    isMarkdownHorizontalRule(line) ||
    /^\s*[-*+]\s+/.test(line) ||
    /^\s*\d+\.\s+/.test(line) ||
    /^\s*>\s?/.test(line)
  ) {
    return true;
  }
  return isTableHeaderLine(line) && index + 1 < lines.length && isTableDividerLine(lines[index + 1] ?? "");
}
