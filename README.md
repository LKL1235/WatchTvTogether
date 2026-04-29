# WatchTogether Backend

WatchTogether 的后端服务仓库（Go + Gin + WebSocket）。

## 说明

本仓库已调整为前后端分离模式：
- 仅包含后端 API / WebSocket / 下载任务 / 媒体与海报静态文件服务
- 不再包含前端工程与前端部署配置
- 不再由 Go 服务托管 SPA 静态页面

## Quick Start

### 本地运行

```bash
go run ./cmd/server
```

默认读取 `config.yaml`，也支持环境变量覆盖（参考 `.env.example`）。

### Docker 开发模式

```bash
docker compose -f docker-compose.dev.yml up -d --build
```

### Docker 生产样例（Postgres + Redis）

```bash
docker compose up -d --build
```

## 核心接口

- `GET /healthz`
- `POST /api/auth/register`
- `POST /api/auth/login`
- `POST /api/auth/refresh`
- `POST /api/auth/logout`
- `GET /api/users/me`
- `POST /api/rooms`
- `GET /api/rooms`
- `GET /api/rooms/:roomId`
- `POST /api/rooms/:roomId/join`
- `DELETE /api/rooms/:roomId`
- `WebSocket /ws/room/:roomId`
- `GET /api/videos`
- `GET /api/videos/:id`
- `GET /static/videos/*`
- `GET /static/posters/*`

## CI/CD

- CI: Go 测试 + 后端构建（linux/amd64）
- CD: Tag 发布后端产物与 Docker 镜像

## 目录

- `cmd/server` 程序入口
- `internal/api` 路由与 handler
- `internal/store` 存储抽象与实现（sqlite/postgres）
- `internal/cache` 缓存抽象与实现（memory/redis）
- `internal/download` 下载任务
- `pkg` 通用工具
