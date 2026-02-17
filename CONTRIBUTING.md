# Contributing

## 分支策略

- `main`：受保护分支，只接受通过评审和 CI 的合并。
- 功能开发：从 `main` 切 `feat/<topic>`。
- 修复开发：从 `main` 切 `fix/<topic>`。
- 紧急修复：从 `main` 切 `hotfix/<topic>`，合并后必须补全测试与文档。
- 单个 PR 只做一件事，禁止把不相关变更混在同一个 PR。

## 提交流程

1. 先更新契约（`packages/contracts/openapi/openapi.yaml`），再改实现。
2. 本地至少执行：
   - `cd apps/gateway && go test ./...`
   - `cd apps/cli && pnpm test`
   - `cd tests/contract && pnpm test`
3. 若改动影响端到端链路，再执行：
   - `cd tests/smoke && pnpm test`

## 提交规范

使用 Conventional Commits：

- `feat:` 新功能
- `fix:` 缺陷修复
- `refactor:` 重构（不改行为）
- `test:` 测试补充/调整
- `docs:` 文档改动
- `chore:` 工程杂项

示例：

- `feat(gateway): add api key auth middleware`
- `fix(cli): classify gateway errors by error.code`

## Pull Request 模板

每个 PR 描述必须包含以下四段：

```markdown
## 变更说明
- 

## 测试说明
- [ ] `go test ./...`
- [ ] `pnpm -r test`
- [ ] 其他（如 smoke/live）

## 回滚说明
- 回滚方式：
- 数据兼容性：

## 风险与影响面
- 
```

## 代码评审要求

- 至少 1 位评审通过。
- CI 必须通过（`ci-fast` 或 `ci-full`）。
- 变更 contracts 时必须附带 contract tests 结果。
