import { renderMarkdownHtml } from "./markdown/engine.js";
import { sanitizeRenderedFragment } from "./markdown/postprocess.js";
import { normalizeMarkdown } from "./markdown/utils.js";

export function renderMarkdownToFragment(markdown: string, doc: Document): DocumentFragment {
  const fragment = doc.createDocumentFragment();
  const normalized = normalizeMarkdown(markdown);
  if (normalized.trim() === "") {
    return fragment;
  }

  const html = renderMarkdownHtml(normalized);
  if (html.trim() === "") {
    return fragment;
  }

  const template = doc.createElement("template");
  template.innerHTML = html;
  sanitizeRenderedFragment(template.content, doc);
  fragment.appendChild(template.content.cloneNode(true));
  return fragment;
}
