import { readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

describe("web shell smoke", () => {
  it("contains all control tabs and panel roots", () => {
    const html = readFileSync(join(process.cwd(), "src/index.html"), "utf8");

    expect(html).toContain('data-tab="chat"');
    expect(html).toContain('data-tab="cron"');

    expect(html).toContain('id="panel-chat"');
    expect(html).toContain('id="composer"');
    expect(html).toContain('id="message-input"');
    expect(html).toContain('id="send-btn"');
    expect(html).toContain('id="composer-token-estimate"');
    expect(html).toContain('id="chat-search-toggle"');
    expect(html).toContain('id="search-modal"');
    expect(html).toContain('id="search-chat-input"');
    expect(html).toContain('id="search-chat-results"');

    expect(html).toContain('data-settings-section="models"');
    expect(html).toContain('data-settings-section-panel="models"');
    expect(html).toContain('data-settings-section="channels"');
    expect(html).toContain('data-settings-section-panel="channels"');
    expect(html).toContain('data-settings-section="workspace"');
    expect(html).toContain('data-settings-section-panel="workspace"');

    expect(html).toContain('id="workspace-files-body"');
    expect(html).toContain('id="workspace-editor-form"');
    expect(html).toContain('id="workspace-file-content"');
    expect(html).toContain('id="workspace-import-form"');
    expect(html).toContain('id="panel-cron"');
    expect(html).toContain('id="chat-cron-toggle"');
    expect(html).toContain('id="cron-chat-toggle"');
    expect(html).toContain('id="cron-task-type"');
    expect(html).toContain('id="cron-workflow-viewport"');
    expect(html).toContain('id="cron-workflow-canvas"');
    expect(html).toContain('id="cron-workflow-edges"');
    expect(html).toContain('id="cron-workflow-nodes"');
    expect(html).toContain('id="cron-reset-workflow"');
    expect(html).toContain('id="cron-workflow-fullscreen-btn"');
    expect(html).toContain('id="cron-workflow-node-editor"');
    expect(html).toContain('id="cron-workflow-execution-list"');
  });
});
