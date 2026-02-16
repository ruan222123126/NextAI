# 部署指南（systemd / docker）

## 环境变量

- `COPAW_HOST`（默认 `127.0.0.1`）
- `COPAW_PORT`（默认 `8088`）
- `COPAW_DATA_DIR`（默认 `.data`）
- `COPAW_API_KEY`（可选；设置后启用 API 鉴权）

## systemd 部署示例

1. 构建二进制

```bash
cd apps/gateway
go build -o /opt/copaw/bin/gateway ./cmd/gateway
```

2. 创建服务文件 `/etc/systemd/system/copaw-gateway.service`

```ini
[Unit]
Description=CoPaw Next Gateway
After=network.target

[Service]
Type=simple
Environment=COPAW_HOST=0.0.0.0
Environment=COPAW_PORT=8088
Environment=COPAW_DATA_DIR=/var/lib/copaw
# Environment=COPAW_API_KEY=change-me
ExecStart=/opt/copaw/bin/gateway
Restart=always
RestartSec=3
User=www-data
Group=www-data

[Install]
WantedBy=multi-user.target
```

3. 启动服务

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now copaw-gateway
sudo systemctl status copaw-gateway
```

## Docker 部署示例

Dockerfile（最小示例）：

```dockerfile
FROM golang:1.22 AS build
WORKDIR /src
COPY . .
RUN cd apps/gateway && go build -o /out/gateway ./cmd/gateway

FROM debian:bookworm-slim
COPY --from=build /out/gateway /usr/local/bin/gateway
ENV COPAW_HOST=0.0.0.0
ENV COPAW_PORT=8088
ENV COPAW_DATA_DIR=/data
EXPOSE 8088
CMD ["gateway"]
```

运行：

```bash
docker run -d \
  --name copaw-gateway \
  -p 8088:8088 \
  -v copaw-data:/data \
  -e COPAW_API_KEY=change-me \
  copaw-next/gateway:latest
```
