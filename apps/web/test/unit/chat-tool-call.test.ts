import { describe, expect, it } from "vitest";

import { createChatToolCallHelpers } from "../../src/main/chat-tool-call";

function createTranslator(): (key: string, params?: Record<string, string | number | boolean>) => string {
  const messages: Record<string, string> = {
    "chat.toolCallNotice": "调用任务 '{{target}}'",
    "chat.toolCallEdit": "编辑",
    "chat.toolCallEditPath": "编辑（{{path}}）",
    "chat.toolCallWrite": "写入",
    "chat.toolCallWritePath": "写入（{{path}}）",
    "chat.toolCallActionBrowse": "已浏览",
    "chat.toolCallSummaryFileCount": "{{count}} 个文件",
    "chat.toolCallSummaryListCount": "{{count}} 个列表",
    "chat.toolCallSummaryLineRange": "行 {{range}}",
    "chat.toolCallOutputPending": "等待执行输出...",
    "chat.toolCallOutputUnavailable": "暂无执行输出",
    "chat.toolCallUnknown": "unknown_tool",
    "common.ellipsis": "...",
  };
  return (key: string, params?: Record<string, string | number | boolean>) => {
    const template = messages[key] ?? key;
    if (!params) {
      return template;
    }
    return template.replace(/\{\{\s*(\w+)\s*\}\}/g, (_full, token: string) => {
      const value = params[token];
      return value === undefined ? "" : String(value);
    });
  };
}

describe("chat tool call helpers", () => {
  it("将 edit [1-1] replaced 0 line(s) 识别为写入摘要", () => {
    const t = createTranslator();
    const helpers = createChatToolCallHelpers({ t });
    const path = "/mnt/Files/test (Copy)/kanban.html";

    const summary = helpers.resolveToolCallSummaryLine({
      summary: `编辑（${path}）`,
      raw: `edit ${path} [1-1] replaced 0 line(s).`,
      toolName: "edit",
      outputReady: true,
    });

    expect(summary).toBe(`写入（${path}）`);
  });

  it("普通 edit 替换仍显示编辑摘要", () => {
    const t = createTranslator();
    const helpers = createChatToolCallHelpers({ t });
    const path = "/mnt/Files/NextAI/README.md";

    const summary = helpers.resolveToolCallSummaryLine({
      summary: `编辑（${path}）`,
      raw: `edit ${path} [4-6] replaced 2 line(s).`,
      toolName: "edit",
      outputReady: true,
    });

    expect(summary).toBe(`编辑（${path}）`);
  });

  it("浏览文件摘要返回行号范围小字", () => {
    const t = createTranslator();
    const helpers = createChatToolCallHelpers({ t });
    const path = "/mnt/Files/NextAI/AGENTS.md";

    const summary = helpers.resolveToolCallSummaryLine({
      summary: `查看（${path}）`,
      raw: `view ${path} [1-100]\n1: # AGENTS`,
      toolName: "view",
      outputReady: true,
    });
    const meta = helpers.resolveToolCallSummaryMeta({
      summary: `查看（${path}）`,
      raw: `view ${path} [1-100]\n1: # AGENTS`,
      toolName: "view",
      outputReady: true,
    });

    expect(summary).toBe("已浏览 1 个文件");
    expect(meta).toBe("行 1-100");
  });

  it("浏览摘要优先显示实际回退后的行号范围", () => {
    const t = createTranslator();
    const helpers = createChatToolCallHelpers({ t });
    const path = "/mnt/Files/NextAI/README.md";

    const meta = helpers.resolveToolCallSummaryMeta({
      summary: `查看（${path}）`,
      raw: `view ${path} [1-20] (fallback from requested [1-100], total=70)\n1: # README`,
      toolName: "view",
      outputReady: true,
    });

    expect(meta).toBe("行 1-20");
  });

  it("浏览多个文件时行号范围小字最多展示三段", () => {
    const t = createTranslator();
    const helpers = createChatToolCallHelpers({ t });

    const meta = helpers.resolveToolCallSummaryMeta({
      summary: "查看多个文件",
      raw: [
        "view /tmp/a.md [1-10]",
        "view /tmp/b.md [20-30]",
        "view /tmp/c.md [40-50]",
        "view /tmp/d.md [60-70]",
      ].join("\n"),
      toolName: "view",
      outputReady: true,
    });

    expect(meta).toBe("行 1-10, 20-30, 40-50 ...");
  });
});
