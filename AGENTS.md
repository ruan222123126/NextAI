# AGENTS

## 项目概览
NextAI - 个人 AI 助手控制平面（Gateway + CLI + Web + TUI），v1 聚焦 nextai-local。

## 技术栈
- Gateway: Go
- CLI/Web/TUI: TypeScript
- CLI 只调 Gateway API，不直接写业务存储

## 开发规范
- 先写 contracts，再写实现
- 小步提交，单 PR 单主题
- 统一错误模型：`{ error: { code, message, details } }`
- 语义化版本 `v1.0.0-rc.x`

## 测试要求
- Go: `go test` + `gofmt`
- TS: `tsc` + `vitest`
- 核心闭环必须有 e2e

## 安全
- 密钥只从 env/secret store 取
- 路径穿越校验，最小权限

## AI 接手流程
1. 先读 `docs/TODO.md` 和 `/home/ruan/.codex/handoff/latest.md`
2. 按未完成项顺序执行
3. 任务结束后更新 `docs/TODO.md`
