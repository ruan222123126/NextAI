import { JSDOM } from "jsdom";
import { describe, expect, it } from "vitest";

import { renderMarkdownToFragment } from "../../src/markdown";

function render(markdown: string): HTMLElement {
  const dom = new JSDOM("<!doctype html><html><body></body></html>");
  const doc = dom.window.document;
  const root = doc.createElement("div");
  root.appendChild(renderMarkdownToFragment(markdown, doc));
  return root;
}

describe("renderMarkdownToFragment", () => {
  it("supports common markdown blocks and inline formats", () => {
    const root = render(
      [
        "## 标题",
        "",
        "这是 **加粗**、*斜体* 和 `code`。",
        "",
        "| 列1 | 列2 |",
        "| :--- | ---: |",
        "| a | 1 |",
      ].join("\n"),
    );

    expect(root.querySelector("h2")?.textContent).toBe("标题");
    expect(root.querySelector("strong")?.textContent).toBe("加粗");
    expect(root.querySelector("em")?.textContent).toBe("斜体");
    expect(root.querySelector("code")?.textContent).toBe("code");
    expect(root.querySelector("table")).not.toBeNull();
    expect((root.querySelector("th") as HTMLElement | null)?.style.textAlign).toBe("left");
    expect((root.querySelector("td") as HTMLElement | null)?.style.textAlign).toBe("left");
  });

  it("renders aligned plain text rows as a table with borders", () => {
    const root = render(
      [
        "核心规范：",
        "",
        "类别    要点",
        "架构    Monorepo；Gateway 用 Go，CLI/Web 用 TS；CLI 只调用 Gateway API",
        "开发流程    先写 contracts → 再写实现；小步提交",
        "代码规范    Go: go test + gofmt；TS: tsc + vitest；统一错误模型",
      ].join("\n"),
    );

    const table = root.querySelector("table");
    expect(table).not.toBeNull();
    expect(Array.from(root.querySelectorAll("th")).map((cell) => cell.textContent)).toEqual(["类别", "要点"]);
    expect(root.querySelectorAll("tbody tr")).toHaveLength(3);
    expect((root.querySelector("th") as HTMLElement | null)?.style.textAlign).toBe("left");
  });

  it("does not treat two aligned rows as a table block", () => {
    const root = render(
      [
        "这一段  只是普通文本",
        "第二行  也是普通说明",
      ].join("\n"),
    );

    expect(root.querySelector("table")).toBeNull();
    expect(root.querySelector("p")?.textContent).toContain("这一段");
  });

  it("renders inline code in list items without placeholder leakage", () => {
    const root = render(
      [
        "- `apps/` - 应用目录",
        "- `packages/` - 共享包",
      ].join("\n"),
    );

    const listCodeNodes = Array.from(root.querySelectorAll("li code"));
    expect(listCodeNodes).toHaveLength(2);
    expect(listCodeNodes[0]?.textContent).toBe("apps/");
    expect(listCodeNodes[1]?.textContent).toBe("packages/");
    expect(root.textContent ?? "").not.toContain("MDCODE");
    expect(root.textContent ?? "").not.toContain("MD_CODE");
  });

  it("sanitizes unsafe content and keeps safe links", () => {
    const root = render(
      [
        "[safe](https://example.com/path)",
        "",
        "[unsafe](javascript:alert(1))",
        "",
        "<script>alert('x')</script>",
      ].join("\n"),
    );

    const links = Array.from(root.querySelectorAll("a"));
    expect(links).toHaveLength(1);
    expect(links[0]?.getAttribute("href")).toBe("https://example.com/path");
    expect(links[0]?.getAttribute("target")).toBe("_blank");
    expect(links[0]?.getAttribute("rel")).toContain("noopener");
    expect(root.querySelector("script")).toBeNull();
    expect(root.textContent ?? "").toContain("<script>alert('x')</script>");
    expect(root.textContent ?? "").toContain("unsafe");
  });

  it("renders common LaTeX math notation with structured math markup", () => {
    const root = render(
      [
        "\\[",
        "E=BLv",
        "\\]",
        "",
        "I=\\frac{E}{R+r}=\\frac{BLv}{R+r}",
        "",
        "行内 \\(\\frac{a+b}{c}\\) 结束",
        "",
        "结论：\\(\\Rightarrow \\boxed{A \\times B}\\)",
        "",
        "`\\frac{x}{y}` 应保持不变",
        "",
        "```",
        "\\frac{m}{n}",
        "```",
      ].join("\n"),
    );

    const displayMathNodes = Array.from(root.querySelectorAll(".math-display"));
    expect(displayMathNodes).toHaveLength(1);
    expect(displayMathNodes[0]?.textContent?.replace(/\s+/g, "")).toContain("E=BLv");

    const inlineMathNodes = Array.from(root.querySelectorAll(".math-inline"));
    expect(inlineMathNodes.length).toBeGreaterThanOrEqual(2);

    const fracNodes = Array.from(root.querySelectorAll(".math-frac"));
    expect(fracNodes.length).toBeGreaterThanOrEqual(3);

    const boxedNode = root.querySelector(".math-boxed");
    expect(boxedNode).not.toBeNull();
    expect(boxedNode?.textContent?.replace(/\s+/g, "")).toContain("A×B");
    expect(root.textContent ?? "").toContain("⇒");

    const codeNodes = Array.from(root.querySelectorAll("code")).map((node) => node.textContent ?? "");
    expect(codeNodes).toContain("\\frac{x}{y}");
    expect(codeNodes).toContain("\\frac{m}{n}");
    expect(root.textContent ?? "").not.toContain("\\Rightarrow");
    expect(root.textContent ?? "").not.toContain("\\boxed");
  });

  it("supports dollar-delimited inline and block math", () => {
    const root = render(
      [
        "速度公式：$v=\\frac{s}{t}$",
        "",
        "$$",
        "F=ma",
        "$$",
      ].join("\n"),
    );

    expect(root.querySelectorAll(".math-inline")).toHaveLength(1);
    expect(root.querySelectorAll(".math-display")).toHaveLength(1);
    expect((root.textContent ?? "").replace(/\s+/g, "")).toContain("F=ma");
  });
});
