import MarkdownIt from "markdown-it";

import { transformAlignedTextTables } from "./aligned-table.js";
import { INLINE_MATH_TOKEN, INLINE_MATH_TOKEN_SUFFIX, maskMathSegments } from "./math.js";
import { escapeRegExpLiteral } from "./utils.js";

const markdownRenderer = new MarkdownIt({
  html: false,
  breaks: true,
  linkify: false,
});

const INLINE_CODE_MASK_TOKEN = "MD_CODE_MASK_";
const INLINE_CODE_MASK_TOKEN_SUFFIX = "_END_CODE";

export function renderMarkdownHtml(markdown: string): string {
  const withMarkdownTables = transformAlignedTextTables(markdown);
  const mathSnippets: string[] = [];
  const maskedMathMarkdown = maskMathOutsideCode(withMarkdownTables, mathSnippets);
  const html = markdownRenderer.render(maskedMathMarkdown);
  return restoreMaskedMath(html, mathSnippets);
}

function maskMathOutsideCode(markdown: string, mathSnippets: string[]): string {
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

    const textChunkLines: string[] = [];
    while (index < lines.length) {
      const chunkLine = lines[index] ?? "";
      if (/^\s*```/.test(chunkLine)) {
        break;
      }
      textChunkLines.push(chunkLine);
      index += 1;
    }
    output.push(maskMathInTextChunk(textChunkLines.join("\n"), mathSnippets));
  }

  return output.join("\n");
}

function maskMathInTextChunk(input: string, mathSnippets: string[]): string {
  const codeSnippets: string[] = [];
  const maskedCode = input.replace(/`([^`\n]+)`/g, (match) => {
    const token = `${INLINE_CODE_MASK_TOKEN}${codeSnippets.length}${INLINE_CODE_MASK_TOKEN_SUFFIX}`;
    codeSnippets.push(match);
    return token;
  });
  const maskedMath = maskMathSegments(maskedCode, mathSnippets);
  return restoreMaskedCode(maskedMath, codeSnippets);
}

function restoreMaskedCode(input: string, codeSnippets: string[]): string {
  const codeTokenPattern = new RegExp(
    `${escapeRegExpLiteral(INLINE_CODE_MASK_TOKEN)}(\\d+)${escapeRegExpLiteral(INLINE_CODE_MASK_TOKEN_SUFFIX)}`,
    "g",
  );
  return input.replace(codeTokenPattern, (_match, index) => {
    const tokenIndex = Number(index);
    if (!Number.isFinite(tokenIndex)) {
      return "";
    }
    return codeSnippets[tokenIndex] ?? "";
  });
}

function restoreMaskedMath(html: string, mathSnippets: string[]): string {
  const mathTokenPattern = new RegExp(
    `${escapeRegExpLiteral(INLINE_MATH_TOKEN)}(\\d+)${escapeRegExpLiteral(INLINE_MATH_TOKEN_SUFFIX)}`,
    "g",
  );
  return html.replace(mathTokenPattern, (_match, index) => {
    const tokenIndex = Number(index);
    if (!Number.isFinite(tokenIndex)) {
      return "";
    }
    return mathSnippets[tokenIndex] ?? "";
  });
}
