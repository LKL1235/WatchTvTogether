# WatchTogether Backend

WatchTogether 的后端服务仓库（Go + Gin + Ably realtime）。

## 说明

本仓库已调整为前后端分离模式：
- 仅包含后端 API / 下载任务 / 媒体与海报静态文件服务
- 不再包含前端工程与前端部署配置
- 不再由 Go 服务托管 SPA 静态页面
- 房间实时同步统一使用 Ably，后端不再提供 `/ws/room/:roomId`

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
- `POST /api/auth/register/code`（发送注册邮箱验证码）
- `POST /api/auth/register`（`email`、`username`、`password`、`code`、可选 `nickname`/`avatar_url`）
- `POST /api/auth/password/reset/code`
- `POST /api/auth/password/reset`（`email`、`code`、`new_password`，成功后使 refresh token 失效）
- `POST /api/auth/login`（请求体：`login` 为邮箱或用户名，`password`）
- `POST /api/auth/refresh`
- `POST /api/auth/logout`
- `GET /api/users/me`（含 `email`）
- `POST /api/rooms`
- `GET /api/rooms`
- `GET /api/rooms/:roomId`
- `POST /api/rooms/:roomId/join`
- `POST /api/rooms/:roomId/snapshot`
- `POST /api/rooms/:roomId/control`
- `POST /api/ably/token`（返回 Ably 用 JWT：`token`、`expires_at` RFC3339）
- `GET /api/rooms/:roomId/state`
- `POST /api/rooms/:roomId/kick/:uid`
- `DELETE /api/rooms/:roomId`
- `GET /api/videos`
- `GET /api/videos/:id`
- `GET /static/videos/*`
- `GET /static/posters/*`

### 错误码补充

- `RATE_LIMITED`（HTTP 429）：验证码发送过频、每日上限、IP 限流等；部分响应带 `Retry-After` 头（秒）。
- 验证码相关文案见 API 错误 `message`（过期、错误、尝试过多等）。

## CI/CD

- CI: Go 测试 + 后端构建（linux/amd64）
- CD: Tag 发布后端产物与 Docker 镜像

## 目录

- `cmd/server` 程序入口
- `internal/api` 路由与 handler
- `internal/store` 存储抽象与实现（postgres）
- `internal/cache` 缓存抽象与实现（memory/redis）
- `internal/realtime` Ably JWT（客户端）与房间消息发布（REST）
- `internal/download` 下载任务
- `pkg` 通用工具
