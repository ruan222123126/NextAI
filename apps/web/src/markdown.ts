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
const INLINE_MATH_TOKEN = "\u0000MDMATHTOKEN";
const INLINE_MATH_TOKEN_SUFFIX = "\u0000";
const SAFE_LINK_PROTOCOLS = new Set(["http:", "https:", "mailto:", "tel:"]);
const ALLOWED_INLINE_TAGS = new Set(["A", "BR", "CODE", "DEL", "EM", "SPAN", "STRONG", "SUB", "SUP"]);
const MATH_ALLOWED_CLASS_PATTERN = /^math-[a-z0-9-]+$/;

const MATH_FRAC_COMMANDS = new Set(["frac", "dfrac", "tfrac", "cfrac"]);
const MATH_TEXT_COMMANDS = new Set([
  "text",
  "textrm",
  "mathrm",
  "mathbf",
  "mathit",
  "mathsf",
  "mathtt",
  "operatorname",
  "mbox",
]);
const MATH_STYLE_COMMANDS = new Set([
  "displaystyle",
  "textstyle",
  "scriptstyle",
  "scriptscriptstyle",
  "limits",
  "nolimits",
]);
const MATH_SPACING_COMMANDS = new Set([
  "quad",
  "qquad",
  "enspace",
  "enskip",
  "thinspace",
  "medspace",
  "thickspace",
]);
const MATH_DECORATION_COMMANDS: Record<string, string> = {
  bar: "¯",
  overline: "¯",
  underline: "_",
  hat: "^",
  widehat: "^",
  tilde: "~",
  widetilde: "~",
  vec: "→",
  dot: "˙",
  ddot: "¨",
};
const LATEX_ESCAPED_CHAR_MAP: Record<string, string> = {
  "{": "{",
  "}": "}",
  "[": "[",
  "]": "]",
  "(": "(",
  ")": ")",
  "%": "%",
  "$": "$",
  "_": "_",
  "&": "&",
  "#": "#",
  ",": " ",
  ";": " ",
  ":": " ",
  "!": "",
};
const LATEX_SYMBOL_MAP: Record<string, string> = {
  alpha: "α",
  beta: "β",
  gamma: "γ",
  delta: "δ",
  epsilon: "ε",
  zeta: "ζ",
  eta: "η",
  theta: "θ",
  iota: "ι",
  kappa: "κ",
  lambda: "λ",
  mu: "μ",
  nu: "ν",
  xi: "ξ",
  omicron: "ο",
  pi: "π",
  rho: "ρ",
  sigma: "σ",
  tau: "τ",
  upsilon: "υ",
  phi: "φ",
  varphi: "φ",
  chi: "χ",
  psi: "ψ",
  omega: "ω",
  Gamma: "Γ",
  Delta: "Δ",
  Theta: "Θ",
  Lambda: "Λ",
  Xi: "Ξ",
  Pi: "Π",
  Sigma: "Σ",
  Upsilon: "Υ",
  Phi: "Φ",
  Psi: "Ψ",
  Omega: "Ω",
  times: "×",
  cdot: "·",
  div: "÷",
  pm: "±",
  mp: "∓",
  neq: "≠",
  ne: "≠",
  approx: "≈",
  sim: "∼",
  simeq: "≃",
  equiv: "≡",
  le: "≤",
  leq: "≤",
  ge: "≥",
  geq: "≥",
  lt: "<",
  gt: ">",
  infty: "∞",
  partial: "∂",
  nabla: "∇",
  forall: "∀",
  exists: "∃",
  in: "∈",
  notin: "∉",
  subset: "⊂",
  subseteq: "⊆",
  supset: "⊃",
  supseteq: "⊇",
  cup: "∪",
  cap: "∩",
  to: "→",
  leftarrow: "←",
  Rightarrow: "⇒",
  rightarrow: "→",
  Leftarrow: "⇐",
  leftrightarrow: "↔",
  Leftrightarrow: "⇔",
  mapsto: "↦",
  sum: "∑",
  prod: "∏",
  int: "∫",
  oint: "∮",
  cdots: "⋯",
  ldots: "…",
  vdots: "⋮",
  ddots: "⋱",
  angle: "∠",
  degree: "°",
  perp: "⊥",
  parallel: "∥",
  because: "∵",
  therefore: "∴",
  imag: "ℑ",
  real: "ℜ",
  Re: "ℜ",
  Im: "ℑ",
  hbar: "ℏ",
  ell: "ℓ",
  log: "log",
  ln: "ln",
  sin: "sin",
  cos: "cos",
  tan: "tan",
  cot: "cot",
  sec: "sec",
  csc: "csc",
};

interface MathStartCandidate {
  index: number;
  type: "double-dollar" | "single-dollar" | "bracket" | "paren";
}

interface MathSegmentParseResult {
  display: boolean;
  expression: string;
  nextIndex: number;
}

interface MathParseResult {
  html: string;
  nextIndex: number;
}

interface MathGroupParseResult {
  value: string;
  nextIndex: number;
}

interface MathRequiredGroupParseResult {
  raw: string;
  html: string;
  nextIndex: number;
}

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

function maskMathSegments(input: string, mathSnippets: string[]): string {
  let output = "";
  let cursor = 0;
  while (cursor < input.length) {
    const nextStart = findNextMathStart(input, cursor);
    if (!nextStart) {
      output += input.slice(cursor);
      break;
    }
    output += input.slice(cursor, nextStart.index);
    const segment = parseMathSegment(input, nextStart);
    if (!segment) {
      output += input[nextStart.index] ?? "";
      cursor = nextStart.index + 1;
      continue;
    }
    const mathHtml = renderMathExpressionToHtml(segment.expression, segment.display);
    if (mathHtml === "") {
      output += input.slice(nextStart.index, segment.nextIndex);
    } else {
      const token = `${INLINE_MATH_TOKEN}${mathSnippets.length}${INLINE_MATH_TOKEN_SUFFIX}`;
      mathSnippets.push(mathHtml);
      output += token;
    }
    cursor = segment.nextIndex;
  }
  return output;
}

function findNextMathStart(input: string, fromIndex: number): MathStartCandidate | null {
  for (let index = fromIndex; index < input.length; index += 1) {
    const ch = input[index] ?? "";
    if (ch === "$" && !isEscapedChar(input, index)) {
      if ((input[index + 1] ?? "") === "$") {
        return { index, type: "double-dollar" };
      }
      if (isSingleDollarMathStart(input, index)) {
        return { index, type: "single-dollar" };
      }
    }
    if (ch === "\\" && !isEscapedChar(input, index)) {
      const next = input[index + 1] ?? "";
      if (next === "[") {
        return { index, type: "bracket" };
      }
      if (next === "(") {
        return { index, type: "paren" };
      }
    }
  }
  return null;
}

function parseMathSegment(input: string, start: MathStartCandidate): MathSegmentParseResult | null {
  if (start.type === "double-dollar") {
    const openLength = 2;
    for (let cursor = start.index + openLength; cursor < input.length - 1; cursor += 1) {
      if ((input[cursor] ?? "") === "$" && (input[cursor + 1] ?? "") === "$" && !isEscapedChar(input, cursor)) {
        return {
          display: true,
          expression: input.slice(start.index + openLength, cursor),
          nextIndex: cursor + 2,
        };
      }
    }
    return null;
  }
  if (start.type === "single-dollar") {
    const openLength = 1;
    for (let cursor = start.index + openLength; cursor < input.length; cursor += 1) {
      const ch = input[cursor] ?? "";
      if (ch === "\n") {
        return null;
      }
      if (ch === "$" && !isEscapedChar(input, cursor)) {
        if (cursor === start.index + openLength) {
          return null;
        }
        return {
          display: false,
          expression: input.slice(start.index + openLength, cursor),
          nextIndex: cursor + 1,
        };
      }
    }
    return null;
  }
  if (start.type === "bracket") {
    const openLength = 2;
    for (let cursor = start.index + openLength; cursor < input.length - 1; cursor += 1) {
      if ((input[cursor] ?? "") === "\\" && (input[cursor + 1] ?? "") === "]" && !isEscapedChar(input, cursor)) {
        return {
          display: true,
          expression: input.slice(start.index + openLength, cursor),
          nextIndex: cursor + 2,
        };
      }
    }
    return null;
  }
  const openLength = 2;
  for (let cursor = start.index + openLength; cursor < input.length - 1; cursor += 1) {
    if ((input[cursor] ?? "") === "\\" && (input[cursor + 1] ?? "") === ")" && !isEscapedChar(input, cursor)) {
      return {
        display: false,
        expression: input.slice(start.index + openLength, cursor),
        nextIndex: cursor + 2,
      };
    }
  }
  return null;
}

function renderMathExpressionToHtml(expression: string, display: boolean): string {
  const trimmed = expression.trim();
  if (trimmed === "") {
    return "";
  }
  const body = renderMathInlineHtml(trimmed);
  if (body.trim() === "") {
    return "";
  }
  const className = display ? "math-display" : "math-inline";
  return `<span class="${className}">${body}</span>`;
}

function renderMathInlineHtml(input: string): string {
  const parsed = parseMathSequence(input, 0);
  if (parsed.nextIndex >= input.length) {
    return parsed.html;
  }
  return `${parsed.html}${escapeHtml(input.slice(parsed.nextIndex))}`;
}

function parseMathSequence(input: string, startIndex: number, stopChar?: string): MathParseResult {
  const parts: string[] = [];
  let cursor = startIndex;
  while (cursor < input.length) {
    const ch = input[cursor] ?? "";
    if (stopChar && ch === stopChar) {
      return { html: parts.join(""), nextIndex: cursor + 1 };
    }
    if (/\s/.test(ch)) {
      cursor += 1;
      while (cursor < input.length && /\s/.test(input[cursor] ?? "")) {
        cursor += 1;
      }
      if (parts.length === 0 || parts[parts.length - 1] === " ") {
        continue;
      }
      parts.push(" ");
      continue;
    }

    const atom = parseMathAtom(input, cursor);
    if (!atom) {
      parts.push(escapeHtml(ch));
      cursor += 1;
      continue;
    }
    cursor = atom.nextIndex;

    let base = atom.html;
    let superscript = "";
    let subscript = "";
    while (cursor < input.length) {
      cursor = skipMathWhitespace(input, cursor);
      const marker = input[cursor] ?? "";
      if (marker !== "^" && marker !== "_") {
        break;
      }
      const script = parseMathScript(input, cursor + 1);
      if (marker === "^") {
        superscript = script.html;
      } else {
        subscript = script.html;
      }
      cursor = script.nextIndex;
    }
    if (superscript !== "" || subscript !== "") {
      base = `<span class="math-script">${base}${subscript === "" ? "" : `<sub>${subscript}</sub>`}${superscript === "" ? "" : `<sup>${superscript}</sup>`}</span>`;
    }
    parts.push(base);
  }
  return { html: parts.join(""), nextIndex: cursor };
}

function parseMathAtom(input: string, startIndex: number): MathParseResult | null {
  const ch = input[startIndex] ?? "";
  if (ch === "}") {
    return null;
  }
  if (ch === "{") {
    const group = parseDelimitedGroup(input, startIndex, "{", "}");
    if (!group) {
      return {
        html: escapeHtml(ch),
        nextIndex: startIndex + 1,
      };
    }
    return {
      html: renderMathInlineHtml(group.value),
      nextIndex: group.nextIndex,
    };
  }
  if (ch === "\\") {
    return parseMathCommand(input, startIndex);
  }
  return {
    html: escapeHtml(ch),
    nextIndex: startIndex + 1,
  };
}

function parseMathCommand(input: string, startIndex: number): MathParseResult {
  const immediate = input[startIndex + 1] ?? "";
  if (immediate === "") {
    return { html: "\\", nextIndex: startIndex + 1 };
  }
  if (!/[A-Za-z]/.test(immediate)) {
    if (immediate === " ") {
      return { html: " ", nextIndex: startIndex + 2 };
    }
    const literal = LATEX_ESCAPED_CHAR_MAP[immediate];
    if (literal !== undefined) {
      return {
        html: escapeHtml(literal),
        nextIndex: startIndex + 2,
      };
    }
    return {
      html: escapeHtml(immediate),
      nextIndex: startIndex + 2,
    };
  }

  let cursor = startIndex + 1;
  while (cursor < input.length && /[A-Za-z]/.test(input[cursor] ?? "")) {
    cursor += 1;
  }
  const command = input.slice(startIndex + 1, cursor);
  if (command === "left" || command === "right") {
    const delimiterIndex = skipMathWhitespace(input, cursor);
    if (delimiterIndex < input.length) {
      return { html: "", nextIndex: delimiterIndex + 1 };
    }
    return { html: "", nextIndex: cursor };
  }
  if (MATH_STYLE_COMMANDS.has(command)) {
    return { html: "", nextIndex: cursor };
  }
  if (MATH_SPACING_COMMANDS.has(command)) {
    return { html: " ", nextIndex: cursor };
  }
  if (MATH_FRAC_COMMANDS.has(command)) {
    const numerator = parseMathRequiredGroup(input, cursor);
    if (!numerator) {
      return { html: escapeHtml(`\\${command}`), nextIndex: cursor };
    }
    const denominator = parseMathRequiredGroup(input, numerator.nextIndex);
    if (!denominator) {
      return {
        html: `${escapeHtml(`\\${command}`)}{${numerator.html}}`,
        nextIndex: numerator.nextIndex,
      };
    }
    return {
      html: `<span class="math-frac"><span class="math-frac-num">${numerator.html}</span><span class="math-frac-den">${denominator.html}</span></span>`,
      nextIndex: denominator.nextIndex,
    };
  }
  if (command === "sqrt") {
    let workingIndex = cursor;
    let rootHtml = "";
    const optionalRoot = parseDelimitedGroup(input, skipMathWhitespace(input, workingIndex), "[", "]");
    if (optionalRoot) {
      rootHtml = `<sup class="math-root">${renderMathInlineHtml(optionalRoot.value)}</sup>`;
      workingIndex = optionalRoot.nextIndex;
    }
    const radicand = parseMathRequiredGroup(input, workingIndex);
    if (!radicand) {
      return { html: escapeHtml(`\\${command}`), nextIndex: cursor };
    }
    return {
      html: `<span class="math-sqrt">${rootHtml}<span class="math-sqrt-sign">√</span><span class="math-sqrt-body">${radicand.html}</span></span>`,
      nextIndex: radicand.nextIndex,
    };
  }
  if (command === "boxed" || command === "fbox") {
    const body = parseMathRequiredGroup(input, cursor);
    if (!body) {
      return { html: escapeHtml(`\\${command}`), nextIndex: cursor };
    }
    return {
      html: `<span class="math-boxed">${body.html}</span>`,
      nextIndex: body.nextIndex,
    };
  }
  const decoration = MATH_DECORATION_COMMANDS[command];
  if (decoration !== undefined) {
    const body = parseMathRequiredGroup(input, cursor);
    if (!body) {
      return { html: escapeHtml(`\\${command}`), nextIndex: cursor };
    }
    return {
      html: `<span class="math-decoration">${escapeHtml(decoration)}${body.html}</span>`,
      nextIndex: body.nextIndex,
    };
  }
  if (MATH_TEXT_COMMANDS.has(command)) {
    const body = parseMathRequiredGroup(input, cursor);
    if (!body) {
      return { html: escapeHtml(`\\${command}`), nextIndex: cursor };
    }
    return {
      html: `<span class="math-text">${body.html}</span>`,
      nextIndex: body.nextIndex,
    };
  }
  const symbol = LATEX_SYMBOL_MAP[command];
  if (symbol !== undefined) {
    return {
      html: symbol,
      nextIndex: cursor,
    };
  }
  return {
    html: escapeHtml(`\\${command}`),
    nextIndex: cursor,
  };
}

function parseMathScript(input: string, startIndex: number): MathParseResult {
  const cursor = skipMathWhitespace(input, startIndex);
  if (cursor >= input.length) {
    return { html: "", nextIndex: cursor };
  }
  const ch = input[cursor] ?? "";
  if (ch === "{") {
    const group = parseDelimitedGroup(input, cursor, "{", "}");
    if (!group) {
      return {
        html: escapeHtml(ch),
        nextIndex: cursor + 1,
      };
    }
    return {
      html: renderMathInlineHtml(group.value),
      nextIndex: group.nextIndex,
    };
  }
  const atom = parseMathAtom(input, cursor);
  if (!atom) {
    return {
      html: escapeHtml(ch),
      nextIndex: cursor + 1,
    };
  }
  return atom;
}

function parseMathRequiredGroup(input: string, startIndex: number): MathRequiredGroupParseResult | null {
  const group = parseDelimitedGroup(input, skipMathWhitespace(input, startIndex), "{", "}");
  if (!group) {
    return null;
  }
  return {
    raw: group.value,
    html: renderMathInlineHtml(group.value),
    nextIndex: group.nextIndex,
  };
}

function parseDelimitedGroup(
  input: string,
  startIndex: number,
  openChar: "{" | "[",
  closeChar: "}" | "]",
): MathGroupParseResult | null {
  if (input[startIndex] !== openChar) {
    return null;
  }
  let depth = 0;
  for (let index = startIndex; index < input.length; index += 1) {
    const ch = input[index] ?? "";
    if (ch === "\\" && index + 1 < input.length) {
      index += 1;
      continue;
    }
    if (ch === openChar) {
      depth += 1;
      continue;
    }
    if (ch !== closeChar) {
      continue;
    }
    depth -= 1;
    if (depth === 0) {
      return {
        value: input.slice(startIndex + 1, index),
        nextIndex: index + 1,
      };
    }
  }
  return null;
}

function skipMathWhitespace(input: string, startIndex: number): number {
  let cursor = startIndex;
  while (cursor < input.length && /\s/.test(input[cursor] ?? "")) {
    cursor += 1;
  }
  return cursor;
}

function isEscapedChar(input: string, index: number): boolean {
  let slashCount = 0;
  for (let cursor = index - 1; cursor >= 0; cursor -= 1) {
    if ((input[cursor] ?? "") !== "\\") {
      break;
    }
    slashCount += 1;
  }
  return slashCount % 2 === 1;
}

function isSingleDollarMathStart(input: string, index: number): boolean {
  const prev = index > 0 ? input[index - 1] ?? "" : "";
  const next = input[index + 1] ?? "";
  if (next === "" || next === "$" || /\s/.test(next)) {
    return false;
  }
  if (/[0-9A-Za-z]/.test(prev)) {
    return false;
  }
  return true;
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
  const maskedCode = input.replace(/`([^`\n]+)`/g, (_match, code) => {
    const token = `${INLINE_CODE_TOKEN}${codeSnippets.length}${INLINE_CODE_TOKEN_SUFFIX}`;
    codeSnippets.push(`<code>${escapeHtml(code)}</code>`);
    return token;
  });
  const mathSnippets: string[] = [];
  const masked = maskMathSegments(maskedCode, mathSnippets);

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
  const mathTokenPattern = new RegExp(`${escapeRegExpLiteral(INLINE_MATH_TOKEN)}(\\d+)${escapeRegExpLiteral(INLINE_MATH_TOKEN_SUFFIX)}`, "g");
  html = html.replace(mathTokenPattern, (_match, index) => {
    const tokenIndex = Number(index);
    if (!Number.isFinite(tokenIndex)) {
      return "";
    }
    return mathSnippets[tokenIndex] ?? "";
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
    const rawClass = element.getAttribute("class") ?? "";
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
      continue;
    }
    const safeClasses = sanitizeMathClassList(rawClass);
    if (safeClasses !== "") {
      element.setAttribute("class", safeClasses);
    }
  }
}

function sanitizeMathClassList(rawClassNames: string): string {
  const classes = rawClassNames
    .split(/\s+/g)
    .map((item) => item.trim())
    .filter((item) => item !== "" && MATH_ALLOWED_CLASS_PATTERN.test(item));
  if (classes.length === 0) {
    return "";
  }
  return classes.join(" ");
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
