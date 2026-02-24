import { escapeHtml } from "./utils.js";

export const INLINE_MATH_TOKEN = "MD_MATH_TOKEN_";
export const INLINE_MATH_TOKEN_SUFFIX = "_END_TOKEN";
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
export function maskMathSegments(input: string, mathSnippets: string[]): string {
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
