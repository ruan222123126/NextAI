# 本地开发指南

## 环境要求

- Go `1.22+`
- Node.js `22+`
- pnpm `10.23.0+`

## 初始化

```bash
pnpm install --recursive
```

## Gateway / CLI / Web 并行开发

1. 启动 Gateway

```bash
cd apps/gateway
go run ./cmd/gateway
```

2. 单独开发 CLI

```bash
cd apps/cli
pnpm build
node dist/index.js --help
# 多语言（可选）
node dist/index.js --locale en-US chats list
COPAW_LOCALE=en-US node dist/index.js chats list
```

3. 单独开发 Web

```bash
cd apps/web
pnpm build
# 打开 dist/index.html
# 顶栏 Language 可切换 zh-CN / en-US，自动写入 localStorage
```

## 常用验证命令

```bash
# Gateway 单测
cd apps/gateway && go test ./...

# CLI 单测
cd apps/cli && pnpm test

# 契约测试
cd tests/contract && pnpm test

# 冒烟 e2e
cd tests/smoke && pnpm test

# 全仓回归
cd /mnt/Files/copaw-next && pnpm -r test
```

## 开发约束

- 先改 contracts，再改实现。
- CLI 只能调 Gateway API，不得直写业务存储。
- 错误模型统一为 `{ error: { code, message, details } }`。
