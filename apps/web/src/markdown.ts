type MarkdownTableAlignment = "left" | "center" | "right" | "none";

interface MarkdownHeadingBlock {
  type: "heading";
  level: 1 | 2 | 3 | 4 | 5 | 6;
  text: string;
}

interface MarkdownParagraphBlock {
  type: "paragraph";
  text: string;
}

interface MarkdownCodeBlock {
  type: "code";
  language: string;
  code: string;
}

interface MarkdownListBlock {
  type: "list";
  ordered: boolean;
  items: string[];
}

interface MarkdownQuoteBlock {
  type: "quote";
  text: string;
}

interface MarkdownHrBlock {
  type: "hr";
}

interface MarkdownTableBlock {
  type: "table";
  headers: string[];
  alignments: MarkdownTableAlignment[];
  rows: string[][];
}

type MarkdownBlock =
  | MarkdownHeadingBlock
  | MarkdownParagraphBlock
  | MarkdownCodeBlock
  | MarkdownListBlock
  | MarkdownQuoteBlock
  | MarkdownHrBlock
  | MarkdownTableBlock;

interface MarkdownTableParseResult {
  block: MarkdownTableBlock;
  nextIndex: number;
}

const INLINE_CODE_TOKEN = "\u0000MDCODETOKEN";
const INLINE_CODE_TOKEN_SUFFIX = "\u0000";
const SAFE_LINK_PROTOCOLS = new Set(["http:", "https:", "mailto:", "tel:"]);
const ALLOWED_INLINE_TAGS = new Set(["A", "BR", "CODE", "DEL", "EM", "STRONG"]);

export function renderMarkdownToFragment(markdown: string, doc: Document): DocumentFragment {
  const fragment = doc.createDocumentFragment();
  const normalized = normalizeMarkdown(markdown);
  if (normalized.trim() === "") {
    return fragment;
  }
  const blocks = tokenizeMarkdownBlocks(normalized);
  for (const block of blocks) {
    fragment.appendChild(renderMarkdownBlock(block, doc));
  }
  return fragment;
}

function normalizeMarkdown(markdown: string): string {
  return markdown.replace(/\r\n?/g, "\n");
}

function tokenizeMarkdownBlocks(markdown: string): MarkdownBlock[] {
  const lines = markdown.split("\n");
  const blocks: MarkdownBlock[] = [];
  let index = 0;

  while (index < lines.length) {
    const line = lines[index] ?? "";
    if (line.trim() === "") {
      index += 1;
      continue;
    }

    if (/^\s*```/.test(line)) {
      const fenceInfo = line.trim().slice(3).trim();
      const codeLines: string[] = [];
      index += 1;
      while (index < lines.length && !/^\s*```/.test(lines[index] ?? "")) {
        codeLines.push(lines[index] ?? "");
        index += 1;
      }
      if (index < lines.length && /^\s*```/.test(lines[index] ?? "")) {
        index += 1;
      }
      blocks.push({
        type: "code",
        language: fenceInfo,
        code: codeLines.join("\n"),
      });
      continue;
    }

    const headingMatch = line.match(/^\s*(#{1,6})\s+(.*)$/);
    if (headingMatch) {
      blocks.push({
        type: "heading",
        level: headingMatch[1].length as 1 | 2 | 3 | 4 | 5 | 6,
        text: headingMatch[2].trim(),
      });
      index += 1;
      continue;
    }

    if (isMarkdownHorizontalRule(line)) {
      blocks.push({ type: "hr" });
      index += 1;
      continue;
    }

    if (isTableHeaderLine(line) && index + 1 < lines.length && isTableDividerLine(lines[index + 1] ?? "")) {
      const headerCells = splitTableRow(line);
      const dividerCells = splitTableRow(lines[index + 1] ?? "");
      if (headerCells.length >= 2 && headerCells.length === dividerCells.length) {
        const rows: string[][] = [];
        index += 2;
        while (index < lines.length) {
          const rowLine = lines[index] ?? "";
          if (rowLine.trim() === "" || !isTableHeaderLine(rowLine)) {
            break;
          }
          const rowCells = splitTableRow(rowLine);
          if (rowCells.length !== headerCells.length) {
            break;
          }
          rows.push(rowCells);
          index += 1;
        }
        blocks.push({
          type: "table",
          headers: headerCells,
          alignments: dividerCells.map(parseTableAlignment),
          rows,
        });
        continue;
      }
    }

    const alignedTable = parseAlignedTextTable(lines, index);
    if (alignedTable) {
      blocks.push(alignedTable.block);
      index = alignedTable.nextIndex;
      continue;
    }

    const unorderedMatch = line.match(/^\s*[-*+]\s+(.*)$/);
    if (unorderedMatch) {
      const items: string[] = [unorderedMatch[1]];
      index += 1;
      while (index < lines.length) {
        const nextLine = lines[index] ?? "";
        const nextMatch = nextLine.match(/^\s*[-*+]\s+(.*)$/);
        if (!nextMatch) {
          break;
        }
        items.push(nextMatch[1]);
        index += 1;
      }
      blocks.push({
        type: "list",
        ordered: false,
        items,
      });
      continue;
    }

    const orderedMatch = line.match(/^\s*\d+\.\s+(.*)$/);
    if (orderedMatch) {
      const items: string[] = [orderedMatch[1]];
      index += 1;
      while (index < lines.length) {
        const nextLine = lines[index] ?? "";
        const nextMatch = nextLine.match(/^\s*\d+\.\s+(.*)$/);
        if (!nextMatch) {
          break;
        }
        items.push(nextMatch[1]);
        index += 1;
      }
      blocks.push({
        type: "list",
        ordered: true,
        items,
      });
      continue;
    }

    if (/^\s*>\s?/.test(line)) {
      const quoteLines: string[] = [line.replace(/^\s*>\s?/, "")];
      index += 1;
      while (index < lines.length) {
        const nextLine = lines[index] ?? "";
        if (!/^\s*>\s?/.test(nextLine)) {
          break;
        }
        quoteLines.push(nextLine.replace(/^\s*>\s?/, ""));
        index += 1;
      }
      blocks.push({
        type: "quote",
        text: quoteLines.join("\n"),
      });
      continue;
    }

    const paragraphLines: string[] = [line];
    index += 1;
    while (index < lines.length) {
      const nextLine = lines[index] ?? "";
      if (
        nextLine.trim() === "" ||
        /^\s*```/.test(nextLine) ||
        /^\s*(#{1,6})\s+/.test(nextLine) ||
        isMarkdownHorizontalRule(nextLine) ||
        (/^\s*[-*+]\s+/.test(nextLine) && nextLine.trim() !== "") ||
        (/^\s*\d+\.\s+/.test(nextLine) && nextLine.trim() !== "") ||
        /^\s*>\s?/.test(nextLine) ||
        (isTableHeaderLine(nextLine) && index + 1 < lines.length && isTableDividerLine(lines[index + 1] ?? "")) ||
        isAlignedTextTableStart(lines, index)
      ) {
        break;
      }
      paragraphLines.push(nextLine);
      index += 1;
    }
    blocks.push({
      type: "paragraph",
      text: paragraphLines.join("\n"),
    });
  }

  return blocks;
}

function renderMarkdownBlock(block: MarkdownBlock, doc: Document): HTMLElement {
  if (block.type === "heading") {
    const heading = doc.createElement(`h${block.level}`);
    appendInlineMarkdown(heading, block.text, doc);
    return heading;
  }
  if (block.type === "paragraph") {
    const paragraph = doc.createElement("p");
    appendInlineMarkdown(paragraph, block.text, doc);
    return paragraph;
  }
  if (block.type === "code") {
    const pre = doc.createElement("pre");
    const code = doc.createElement("code");
    code.textContent = block.code;
    if (block.language !== "") {
      code.className = `language-${normalizeClassToken(block.language)}`;
    }
    pre.appendChild(code);
    return pre;
  }
  if (block.type === "list") {
    const list = doc.createElement(block.ordered ? "ol" : "ul");
    for (const itemText of block.items) {
      const item = doc.createElement("li");
      appendInlineMarkdown(item, itemText, doc);
      list.appendChild(item);
    }
    return list;
  }
  if (block.type === "quote") {
    const quote = doc.createElement("blockquote");
    const paragraph = doc.createElement("p");
    appendInlineMarkdown(paragraph, block.text, doc);
    quote.appendChild(paragraph);
    return quote;
  }
  if (block.type === "hr") {
    return doc.createElement("hr");
  }
  const table = doc.createElement("table");
  const thead = doc.createElement("thead");
  const headRow = doc.createElement("tr");
  for (let i = 0; i < block.headers.length; i += 1) {
    const cell = doc.createElement("th");
    applyCellAlignment(cell, block.alignments[i] ?? "none");
    appendInlineMarkdown(cell, block.headers[i] ?? "", doc);
    headRow.appendChild(cell);
  }
  thead.appendChild(headRow);
  table.appendChild(thead);

  const tbody = doc.createElement("tbody");
  for (const row of block.rows) {
    const tr = doc.createElement("tr");
    for (let i = 0; i < block.headers.length; i += 1) {
      const cell = doc.createElement("td");
      applyCellAlignment(cell, block.alignments[i] ?? "none");
      appendInlineMarkdown(cell, row[i] ?? "", doc);
      tr.appendChild(cell);
    }
    tbody.appendChild(tr);
  }
  table.appendChild(tbody);
  return table;
}

function applyCellAlignment(cell: HTMLTableCellElement, alignment: MarkdownTableAlignment): void {
  if (alignment === "left") {
    cell.style.textAlign = "left";
    return;
  }
  if (alignment === "center") {
    cell.style.textAlign = "center";
    return;
  }
  if (alignment === "right") {
    cell.style.textAlign = "right";
  }
}

function appendInlineMarkdown(container: HTMLElement, input: string, doc: Document): void {
  const template = doc.createElement("template");
  template.innerHTML = buildInlineHtml(input);
  sanitizeInlineTree(template.content, doc);
  container.appendChild(template.content.cloneNode(true));
}

function buildInlineHtml(input: string): string {
  const codeSnippets: string[] = [];
  const masked = input.replace(/`([^`\n]+)`/g, (_match, code) => {
    const token = `${INLINE_CODE_TOKEN}${codeSnippets.length}${INLINE_CODE_TOKEN_SUFFIX}`;
    codeSnippets.push(`<code>${escapeHtml(code)}</code>`);
    return token;
  });

  let html = escapeHtml(masked);
  html = html.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (_match, text, href) => {
    const safeHref = sanitizeHrefCandidate(href);
    const label = text;
    if (safeHref === "") {
      return label;
    }
    return `<a href="${escapeHtmlAttribute(safeHref)}">${label}</a>`;
  });
  html = html.replace(/\*\*([\s\S]+?)\*\*/g, "<strong>$1</strong>");
  html = html.replace(/__([\s\S]+?)__/g, "<strong>$1</strong>");
  html = html.replace(/~~([\s\S]+?)~~/g, "<del>$1</del>");
  html = html.replace(/(^|[^*])\*([^*\n]+)\*(?!\*)/g, "$1<em>$2</em>");
  html = html.replace(/(^|[^_])_([^_\n]+)_(?!_)/g, "$1<em>$2</em>");
  html = html.replace(/\n/g, "<br>");
  const codeTokenPattern = new RegExp(`${escapeRegExpLiteral(INLINE_CODE_TOKEN)}(\\d+)${escapeRegExpLiteral(INLINE_CODE_TOKEN_SUFFIX)}`, "g");
  html = html.replace(codeTokenPattern, (_match, index) => {
    const tokenIndex = Number(index);
    if (!Number.isFinite(tokenIndex)) {
      return "";
    }
    return codeSnippets[tokenIndex] ?? "";
  });
  return html;
}

function escapeRegExpLiteral(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function sanitizeInlineTree(root: ParentNode, doc: Document): void {
  const elements = Array.from(root.querySelectorAll("*"));
  for (const element of elements) {
    const tag = element.tagName.toUpperCase();
    if (!ALLOWED_INLINE_TAGS.has(tag)) {
      element.replaceWith(doc.createTextNode(element.textContent ?? ""));
      continue;
    }
    const rawHref = tag === "A" ? element.getAttribute("href") ?? "" : "";
    for (const attribute of Array.from(element.attributes)) {
      element.removeAttribute(attribute.name);
    }
    if (tag === "A") {
      const href = sanitizeHrefCandidate(rawHref);
      if (href === "") {
        element.replaceWith(doc.createTextNode(element.textContent ?? ""));
        continue;
      }
      element.setAttribute("href", href);
      element.setAttribute("target", "_blank");
      element.setAttribute("rel", "noopener noreferrer nofollow");
    }
  }
}

function sanitizeHrefCandidate(raw: string): string {
  const value = raw.trim();
  if (value === "") {
    return "";
  }
  if (value.startsWith("#") || value.startsWith("/") || value.startsWith("./") || value.startsWith("../")) {
    return value;
  }
  try {
    const parsed = new URL(value, "https://nextai.local");
    if (!SAFE_LINK_PROTOCOLS.has(parsed.protocol)) {
      return "";
    }
    return value;
  } catch {
    return "";
  }
}

function escapeHtml(input: string): string {
  return input
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function escapeHtmlAttribute(input: string): string {
  return escapeHtml(input).replaceAll("`", "&#96;");
}

function normalizeClassToken(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9_-]+/g, "");
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

function parseTableAlignment(cell: string): MarkdownTableAlignment {
  const trimmed = cell.trim();
  if (/^:-{3,}:$/.test(trimmed)) {
    return "center";
  }
  if (/^-{3,}:$/.test(trimmed)) {
    return "right";
  }
  if (/^:-{3,}$/.test(trimmed)) {
    return "left";
  }
  return "none";
}

function parseAlignedTextTable(lines: string[], startIndex: number): MarkdownTableParseResult | null {
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
    block: {
      type: "table",
      headers,
      alignments: new Array(headers.length).fill("left"),
      rows,
    },
    nextIndex: index,
  };
}

function isAlignedTextTableStart(lines: string[], startIndex: number): boolean {
  return parseAlignedTextTable(lines, startIndex) !== null;
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
