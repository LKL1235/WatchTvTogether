# WatchTogether Backend

WatchTogether 的后端服务仓库（Go + Gin + Ably realtime）。

## 演示提示

- 演示用户必须包含 `email` 字段，否则无法完成登录。
- 登录时请使用邮箱作为登录账号，并填写对应密码。

## 开发提示

- 本项目运行依赖 **PostgreSQL** 和 **Redis**：PostgreSQL 用于持久化存储，Redis 用于缓存与验证码/限流等短期状态。
- 推荐使用仓库根目录的 `docker-compose.yml` 启动开发环境，它会同时拉起 `app`、`postgres` 和 `redis`，并等待数据库与缓存通过健康检查后再启动应用。
- 首次启动前请确认已安装 Docker / Docker Compose，并按需配置 `.env` 或环境变量，至少应关注 `JWT_SECRET`、`ABLY_ROOT_KEY`、`RESEND_API_KEY`、`EMAIL_FROM` 等运行相关配置。
- 进程监听地址由 **`ADDR`**（如 `:8080`）或 PaaS 注入的 **`PORT`** 决定；`docker-compose.yml` 中的 **`APP_PORT`** 仅映射宿主机端口，不写入 Go 配置。

## 说明

本仓库为前后端分离模式，主要提供 API、认证、房间与视频元数据。**不提供本机媒资落盘或文件下发**（适配 Vercel 等 SaaS 无服务器部署）。

- 不再包含前端工程与前端部署配置
- 不再由 Go 服务托管 SPA 静态页面
- 房间实时同步统一使用 Ably，后端不提供 `/ws/room/:roomId`
- **服务端视频下载（yt-dlp / ffmpeg / aria2）与本地静态目录已移除**；影片与封面请在数据库中配置为 **外链 URL**（`source_url`、`file_path` 存绝对/相对播放地址，`poster_path` 存封面绝对 URL）

### 部署在 Vercel 等无服务器/短生命周期环境时

- 无长驻后台进程，不适合在进程内跑下载队列、长时转码或本机大文件落盘
- 勿依赖 `GET /api/videos/:id/file` 或 `GET /static/*`（已删除）；播放地址由客户端直接请求 CDN / 对象存储 / HLS 源

### 破坏性变更（媒资与 capabilities）

- 已删除：`GET /api/videos*`（全局影片库）、`GET /api/videos/:id/file`、`GET /static/*`、`GET /api/capabilities`，以及 `storage_dir` / `poster_dir` 配置
- 房间队列仅接受 **http(s):// 或 //** 外链；`POST /api/rooms/:id/control` 可选 `video_duration`（秒），写入 Redis 供多端进度投影

## Quick Start

### 本地运行

```bash
go run ./cmd/server
```

默认读取 `config.yaml`，也支持环境变量覆盖（参考 `.env.example`）。本地直接运行前需先准备可访问的 **PostgreSQL** 与 **Redis**（`CACHE_BACKEND` 必须为 `redis`；`memory` 会在启动时被拒绝）。数据库 URL 优先级：`POSTGRES_URL` > `POSTGRES_DSN` > `DATABASE_URL`。

### Docker 开发模式

```bash
docker compose -f docker-compose.dev.yml up -d --build
```

### Docker 生产样例（Postgres + Redis）

```bash
docker compose up -d --build
```

## 配置与环境变量（节选）

| 变量 | 说明 |
|------|------|
| `ADDR` / `PORT` | 监听地址；未设 `ADDR` 时若存在 `PORT` 则绑定 `:{PORT}` |
| `POSTGRES_URL` / `POSTGRES_DSN` / `DATABASE_URL` | PostgreSQL 连接串（见上文优先级） |
| `CACHE_BACKEND` | 仅 `redis` |
| `REDIS_URL` / `REDIS_ADDR` | `REDIS_URL`（含 `rediss://`）优先于 `REDIS_ADDR` |
| `CORS_ORIGINS` | 逗号分隔的允许源 |
| `EMAIL_FROM` / `RESEND_FROM` | 发件人；后者在未设前者时生效 |
| `CHAT_*` | 聊天 Stream / Ably 重试 / 限流等，见 `.env.example` |

完整列表见 `internal/config/config.go` 与 `.env.example`。

## 核心接口

- `GET /healthz` — `status`、`storage_backend`、`cache_backend`
- `POST /api/auth/register/code`（发送注册邮箱验证码）
- `POST /api/auth/register`（`email`、`username`、`password`、`code`、可选 `nickname`/`avatar_url`）
- `POST /api/auth/password/reset/code`
- `POST /api/auth/password/reset`（`email`、`code`、`new_password`，成功后使 refresh token 失效）
- `POST /api/auth/login`（请求体：`login` 为邮箱或用户名，`password`）
- `POST /api/auth/refresh`
- `POST /api/auth/logout`
- `GET /api/users/me`（含 `email`）
- `POST /api/rooms`
- `GET /api/rooms`（查询：`limit` 默认 20、`offset`、`q` 按名称过滤）
- `GET /api/rooms/:roomId`
- `POST /api/rooms/:roomId/join`（响应可含 `left_room_id`：加入新房前自动离开的旧房）
- `POST /api/rooms/:roomId/leave`
- `POST /api/rooms/:roomId/snapshot`
- `GET /api/rooms/:roomId/chat`（查询：`before_id`、`limit`；私有房 **`password` 为查询参数**）
- `POST /api/rooms/:roomId/chat`（JSON：`text`；私有房 **`password` 在 body**；响应含 `message` 与 `realtime`: `ok` | `deferred`）
- `POST /api/rooms/:roomId/control`
- `POST /api/ably/token`（JSON：`room_id` 必填，可选 `password`、`purpose`；返回 `token`、`expires_at` RFC3339）
- `GET /api/rooms/:roomId/state`
- `POST /api/rooms/:roomId/kick/:uid`
- `DELETE /api/rooms/:roomId`
- `GET /api/admin/rooms`（管理员；含 `online_count`、播放状态等）
- `GET /api/admin/debug/rooms`（管理员；房间快照 + Redis 成员，排障用）

### 错误码补充

- 房间实时聊天依赖 **Redis**（`CACHE_BACKEND=redis`，亦为唯一支持的缓存后端）：消息仅存 **Redis Stream**（与房间生命周期一致，随房间关闭清理），经服务端 **Ably REST** 向同一控制频道发布 **`room.chat`**。未注入 `RoomChat` 实现时（例如部分测试）聊天接口返回 **503**。
- 可选环境变量（均有默认值，见 `internal/config`）：`CHAT_STREAM_MAXLEN`、`CHAT_ABLY_PUBLISH_RETRY`、`CHAT_ABLY_PUBLISH_TOTAL_TIMEOUT`、`CHAT_ABLY_PENDING_MAX`、`CHAT_MAX_PAYLOAD_BYTES`、`CHAT_MAX_TEXT_RUNES`、`CHAT_RATE_PER_SECOND`。
- `RATE_LIMITED`（HTTP 429）：验证码发送过频、每日上限、IP 限流、**聊天发言过频**等；部分响应带 `Retry-After` 头（秒）。
- `PAYLOAD_TOO_LARGE`（HTTP 413）：聊天正文或序列化消息体超过上限。
- `SERVICE_UNAVAILABLE`（HTTP 503）：聊天依赖 Redis 未启用、或 Ably 待投递队列已满等。
- 验证码相关文案见 API 错误 `message`（过期、错误、尝试过多等）。

## CI/CD

- CI: Go 测试 + 后端构建（linux/amd64）
- CD: Tag 发布后端产物与 Docker 镜像

## 设计文档

- [docs/room_queue_url_only_zh.md](docs/room_queue_url_only_zh.md) — 房间 URL 队列与 `control` / `video_duration` 契约
- [docs/room_chat_realtime_design_zh.md](docs/room_chat_realtime_design_zh.md) — 聊天（Redis Stream + Ably `room.chat`）
- [docs/room_empty_cleanup_ops_zh.md](docs/room_empty_cleanup_ops_zh.md) — 空房清理触发、扫描范围与日志排障

## 目录

- `cmd/server` 程序入口
- `internal/api` 路由与 handler
- `internal/store` 存储抽象与实现（postgres）
- `internal/cache` 缓存抽象与实现（进程内 memory 包仅用于测试；生产仅 **redis**）
- `internal/realtime` Ably JWT（客户端）与房间消息发布（REST）
- `pkg` 通用工具
