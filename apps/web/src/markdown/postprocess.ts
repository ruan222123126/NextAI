const SAFE_LINK_PROTOCOLS = new Set(["http:", "https:", "mailto:", "tel:"]);

export function sanitizeRenderedFragment(root: ParentNode, doc: Document): void {
  const links = Array.from(root.querySelectorAll("a"));
  for (const link of links) {
    const rawHref = link.getAttribute("href") ?? "";
    const safeHref = sanitizeHrefCandidate(rawHref);
    if (safeHref === "") {
      link.replaceWith(doc.createTextNode(link.textContent ?? ""));
      continue;
    }
    link.setAttribute("href", safeHref);
    link.setAttribute("target", "_blank");
    link.setAttribute("rel", "noopener noreferrer nofollow");
  }

  const fencedCodeBlocks = Array.from(root.querySelectorAll("pre > code"));
  for (const codeNode of fencedCodeBlocks) {
    const code = codeNode.textContent ?? "";
    if (code.endsWith("\n")) {
      codeNode.textContent = code.slice(0, -1);
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
