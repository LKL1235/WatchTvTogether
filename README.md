# 🎬 WatchTogether — 同步视频观看平台

> 基于 Go 后端 + Vue 前端的多人同步视频观看服务，支持 m3u8/mp4 等格式、房间管理、账号密码注册与登录、JWT 鉴权、服务器视频缓存与下载。
> 单体 Go 应用，接口分层设计，存储后端可替换（开发阶段使用 SQLite + 内存，生产可无缝切换至 PostgreSQL + Redis）。

---

## Quick start

下面两种方式任选其一：前者适合「静态站点托管 + 自建 API」；后者适合「一条命令在本地/服务器跑齐前后端」。

### 方式一：Vercel 前端 + 自托管后端

1. 在 [Vercel](https://vercel.com) 中 **Import** 本仓库。若根目录已提供 `vercel.json`，会按其中配置在 `frontend/` 下执行安装与构建；**否则** 在项目 **Settings → General** 里将 **Root Directory** 设为 `frontend`，Build 命令 `npm run build`，输出目录 `dist`。
2. 在 **Settings → Environment Variables** 中设置 `VITE_API_BASE`，值为后端对浏览器可访问的基础地址（例如 `https://api.你的域名`，**不要**在末尾加 `/`）。需自行在服务器上部署并暴露 Go 服务，并在后端为前端所在域名配置 **CORS**；若使用 WebSocket，需保证公网/内网到后端的 `ws` 或 `wss` 可连通（见前端对 `/api`、`/ws` 等路径的访问方式）。
3. 触发部署后，使用 Vercel 给出的站点 URL 即可打开页面。

### 方式二：Docker Compose（前后端一体）

镜像在构建时会把 Vite 产物嵌入 Go 进程，**同一端口** 提供 API 与静态资源，适合本地或单机服务器快速体验。

**开发 / 轻量单容器（SQLite + 内存缓存）：**

```bash
docker compose -f docker-compose.dev.yml up -d --build
```

**带 PostgreSQL + Redis 的样例（更接近生产，请先参考 `.env.example` 准备 `.env` 并调整密钥等）：**

```bash
docker compose up -d --build
```

默认在 `http://localhost:8080` 访问；宿主机端口可通过环境变量 `APP_PORT` 等调整，详见各 Compose 文件。

### 方式三：独立二进制（Release）+ `config.yaml`

从本仓库 **Releases** 页面下载 Linux amd64 产物并解压后，将仓库根目录的 `config.yaml` 复制到**你准备启动服务的目录**（或按需编辑后保存为该文件名）。

1. **配置文件路径**：服务端固定从当前工作目录读取名为 `config.yaml` 的文件（见 `cmd/server/main.go` 中的 `config.Load("config.yaml")`）。**不是**可执行文件所在目录；请在含有 `config.yaml` 的目录下执行二进制，例如：
   ```bash
   cd /opt/watchtogether
   ./watchtogether-linux-amd64
   ```
2. **缺少文件时**：若当前目录没有 `config.yaml`，程序不会报错，会先使用内置默认值，再应用下面列出的环境变量覆盖。
3. **敏感配置**：生产环境务必修改 `jwt_secret`，并按需设置 `storage_backend` / `cache_backend` 及数据库、Redis 等字段。

**环境变量覆盖**（仅当变量非空时生效，覆盖 `config.yaml` 或默认值）：

| 环境变量 | 说明 |
|----------|------|
| `ADDR` | 监听地址（如 `:8080`） |
| `STORAGE_BACKEND` | `sqlite` 或 `postgres` |
| `SQLITE_PATH` | SQLite 数据库文件路径 |
| `POSTGRES_DSN` | PostgreSQL 连接串 |
| `CACHE_BACKEND` | `memory` 或 `redis` |
| `REDIS_ADDR` | Redis 地址 |
| `JWT_SECRET` | JWT 密钥 |
| `JWT_ACCESS_TTL` / `JWT_REFRESH_TTL` | Access / Refresh 有效期 |
| `STORAGE_DIR` / `POSTER_DIR` | 视频与海报目录 |
| `DOWNLOAD_WORKERS` | 下载并发数 |
| `ARIA2_RPC_URL` / `ARIA2_SECRET` | aria2 JSON-RPC |

完整字段说明与示例见下文「配置示例（`config.yaml`）」。

---

## 技术栈概览

| 层级 | 技术选型 |
|------|----------|
| 后端 | Go（Gin）、WebSocket（gorilla/websocket）、FFmpeg、yt-dlp / aria2 |
| 前端 | Vue 3 + Vite、Pinia、HLS.js（Video.js 作为依赖预留）|
| 鉴权 | 账号密码（注册/登录）+ JWT（Access / Refresh）|
| 数据库（开发）| SQLite（用户/房间/视频元数据）|
| 缓存（开发）| 内存 Map（房间状态/会话/Pub/Sub）|
| 数据库（生产）| PostgreSQL 15+（替换 SQLite，接口不变）|
| 缓存（生产）| Redis 7+（替换内存实现，接口不变）|
| 存储 | 本地文件系统 / MinIO（可选）|
| 部署 | 单体二进制，开发无需 Docker；生产可用 Docker Compose |

---

## 架构设计原则

### 接口分层（Interface-Driven Design）

所有数据库操作与缓存操作都通过 **Go Interface** 抽象，上层业务代码只依赖接口，不依赖具体实现。

```
internal/
├── store/
│   ├── store.go          # 定义所有 Repository Interface
│   ├── sqlite/           # SQLite 实现（开发/单机）
│   └── postgres/         # PostgreSQL 实现（生产）
├── cache/
│   ├── cache.go          # 定义 Cache / PubSub Interface
│   ├── memory/           # 内存实现（开发/单机）
│   └── redis/            # Redis 实现（生产）
```

#### Store Interface 示意

```go
// internal/store/store.go

type UserStore interface {
    GetByID(ctx context.Context, id string) (*model.User, error)
    GetByUsername(ctx context.Context, username string) (*model.User, error)
    Create(ctx context.Context, user *model.User) error
    Update(ctx context.Context, user *model.User) error
}

type RoomStore interface {
    Create(ctx context.Context, room *model.Room) error
    GetByID(ctx context.Context, id string) (*model.Room, error)
    List(ctx context.Context, opts ListRoomsOpts) ([]*model.Room, int, error)
    Update(ctx context.Context, room *model.Room) error
    Delete(ctx context.Context, id string) error
}

type VideoStore interface {
    Create(ctx context.Context, video *model.Video) error
    GetByID(ctx context.Context, id string) (*model.Video, error)
    List(ctx context.Context, opts ListVideosOpts) ([]*model.Video, int, error)
    UpdateStatus(ctx context.Context, id, status string) error
    Delete(ctx context.Context, id string) error
}

type DownloadTaskStore interface {
    Create(ctx context.Context, task *model.DownloadTask) error
    GetByID(ctx context.Context, id string) (*model.DownloadTask, error)
    List(ctx context.Context) ([]*model.DownloadTask, error)
    UpdateProgress(ctx context.Context, id string, progress float64, status string) error
    Delete(ctx context.Context, id string) error
}
```

#### Cache Interface 示意

```go
// internal/cache/cache.go

// SessionCache: JWT 黑名单 + Refresh Token 存储
type SessionCache interface {
    SetRefreshToken(ctx context.Context, userID, token string, ttl time.Duration) error
    GetRefreshToken(ctx context.Context, userID string) (string, error)
    BlacklistToken(ctx context.Context, jti string, ttl time.Duration) error
    IsBlacklisted(ctx context.Context, jti string) (bool, error)
}

// RoomStateCache: 房间播放状态快照（新成员加入时同步用）
type RoomStateCache interface {
    SetRoomState(ctx context.Context, roomID string, state *model.RoomState) error
    GetRoomState(ctx context.Context, roomID string) (*model.RoomState, error)
    DeleteRoomState(ctx context.Context, roomID string) error
}

// PubSub: 跨 goroutine / 跨实例消息广播
// 开发阶段用内存 channel 实现；生产阶段替换为 Redis Pub/Sub
type PubSub interface {
    Publish(ctx context.Context, channel string, payload []byte) error
    Subscribe(ctx context.Context, channel string) (<-chan []byte, func(), error)
}
```

#### 依赖注入方式

```go
// cmd/server/main.go

func main() {
    cfg := config.Load()

    // 根据配置选择底层实现
    var userStore store.UserStore
    var pubsub cache.PubSub
    // ...

    if cfg.UsePostgres {
        db := postgres.Connect(cfg.DSN)
        userStore = postgres.NewUserStore(db)
        // ...
        pubsub = rediscache.NewPubSub(cfg.RedisAddr)
    } else {
        db := sqlite.Connect(cfg.SQLitePath)
        userStore = sqlite.NewUserStore(db)
        // ...
        pubsub = memory.NewPubSub()
    }

    svc := service.New(userStore, pubsub, ...)
    router := api.NewRouter(svc)
    router.Run(cfg.Addr)
}
```

---

## 同步方案：WebSocket

**已确定采用 WebSocket**，原因：

1. 播放控制（暂停、拖进度、切换视频）是**双向、高频**事件，WebSocket 天然契合
2. 成员加入/离开、聊天等扩展功能均需双向通信，一套连接全覆盖
3. 开发阶段 PubSub 用内存实现，无需启动 Redis；生产阶段无缝替换
4. SSE 架构割裂（房主推送需额外 HTTP 接口），HTTP 轮询实时性差，均不采用

### 消息协议（JSON）

```json
// 客户端 → 服务端（房主控制）
{
  "type": "play_control",
  "action": "seek" | "play" | "pause" | "next" | "switch",
  "position": 123.45,
  "video_id": "uuid"
}

// 服务端 → 客户端（广播同步）
{
  "type": "sync",
  "action": "seek" | "play" | "pause" | "next" | "switch",
  "position": 123.45,
  "video_id": "uuid",
  "timestamp": 1714204800
}

// 服务端 → 客户端（房间事件）
{
  "type": "room_event",
  "event": "user_joined" | "user_left" | "user_kicked" | "queue_updated",
  "payload": { ... }
}
```

---

## TODO / 开发计划

### 阶段 0：存储接口层（**优先完成，是其他所有任务的地基**）

**目标**：定义并实现所有 Store / Cache Interface，上层业务零依赖具体 DB。

#### 0.1 接口定义

- [x] 在 `internal/store/store.go` 定义 `UserStore`、`RoomStore`、`VideoStore`、`DownloadTaskStore` interface
- [x] 在 `internal/cache/cache.go` 定义 `SessionCache`、`RoomStateCache`、`PubSub` interface
- [x] 在 `internal/model/` 定义所有领域模型结构体（`User`、`Room`、`Video`、`DownloadTask`、`RoomState`）

#### 0.2 SQLite 实现（开发阶段底层）

- [x] 引入 `modernc.org/sqlite`（纯 Go，无 CGO 依赖）
- [x] 实现 `internal/store/sqlite/` 下所有 Store interface
- [x] 编写 SQLite schema 初始化脚本（`internal/store/sqlite/schema.sql`）
- [x] 实现自动建表（首次启动时执行 schema，无需迁移工具）

#### 0.3 内存实现（开发阶段底层）

- [x] 实现 `internal/cache/memory/session_cache.go`（内存 Map + sync.RWMutex，支持 TTL）
- [x] 实现 `internal/cache/memory/room_state_cache.go`（内存 Map）
- [x] 实现 `internal/cache/memory/pubsub.go`（内存 channel，支持多订阅者 fanout）

#### 0.4 PostgreSQL 实现（生产阶段底层，可后续补充）

- [x] 实现 `internal/store/postgres/` 下所有 Store interface（使用 `pgx/v5`）
- [x] 编写 SQL 迁移脚本（`migrations/`，使用 `golang-migrate`）

#### 0.5 Redis 实现（生产阶段底层，可后续补充）

- [x] 实现 `internal/cache/redis/` 下所有 Cache interface（使用 `go-redis/v9`）

#### 0.6 配置与依赖注入

- [x] 在 `internal/config/` 定义配置结构（支持环境变量 + 配置文件）
- [x] 在 `cmd/server/main.go` 实现工厂函数：根据 `STORAGE_BACKEND=sqlite|postgres` 和 `CACHE_BACKEND=memory|redis` 动态选择实现（SQLite / PostgreSQL、Memory / Redis 均已接入）
- [x] 编写接口实现一致性测试（`internal/store/testutil/`），SQLite 与 PostgreSQL 共用同一套测试用例

---

### 阶段 1：Go 后端基础框架

- [x] 初始化 Go Module（`go mod init`），配置项目目录结构
- [x] 引入 Gin 路由框架，配置基础中间件（日志、Recovery、CORS）
- [x] 统一错误响应格式与错误码定义（`pkg/apierr/`）
- [x] 健康检查接口 `GET /healthz`
- [x] 静态文件服务（视频文件、海报图片）
- [x] 配置加载（`config.yaml` + 环境变量覆盖）

---

### 阶段 2：用户鉴权系统（注册 / 登录 + JWT）

**目标**：通过账号密码完成注册与登录，签发与校验 JWT，区分管理员与普通用户权限。

#### 功能清单

- [x] 用户注册：用户名、密码（bcrypt/argon2 等安全哈希存储，不明文存库）
- [x] 用户登录：校验密码后生成 JWT（Access Token + Refresh Token）
- [x] 用户角色体系：
  - `admin`：全部管理权限（用户管理、全局视频库、强制销毁任意房间）
  - `user`：可创建房间，在自己房间内行使房主权限
- [x] JWT 中间件鉴权，路由级别 RBAC 权限控制
- [x] 用户信息存储（昵称、头像等可选字段）—— 调用 `UserStore`
- [x] Token 刷新接口，调用 `SessionCache.SetRefreshToken`
- [x] 登出接口，调用 `SessionCache.BlacklistToken`（JWT 黑名单）

#### 接口设计

```
POST /api/auth/register    → 注册
POST /api/auth/login       → 登录，返回 JWT
POST /api/auth/refresh     → 刷新 Token
POST /api/auth/logout      → 登出
GET  /api/users/me         → 获取当前用户信息
```

#### 权限矩阵

| 操作 | admin | 房主(user) | 普通成员 |
|------|-------|------------|----------|
| 用户管理 | ✅ | ❌ | ❌ |
| 全局视频库管理 | ✅ | ❌ | ❌ |
| 强制销毁任意房间 | ✅ | ❌ | ❌ |
| 创建房间 | ✅ | ✅ | ❌ |
| 邀请/踢出用户 | ✅ | ✅（本房间）| ❌ |
| 播放控制 | ✅ | ✅（本房间）| ❌ |
| 视频队列管理 | ✅ | ✅（本房间）| ❌ |
| 加入房间/观看 | ✅ | ✅ | ✅ |

---

### 阶段 3：房间管理 + WebSocket 同步

**目标**：实现房间创建/加入、WebSocket 实时同步播放状态。

#### 功能清单

- [x] 房间 CRUD 接口，调用 `RoomStore`
- [x] WebSocket Hub 实现（`internal/room/hub.go`）：
  - 每个房间一个 Hub，管理该房间所有 WebSocket 连接
  - 通过 `PubSub` 接收广播消息（解耦消息来源，支持未来多实例）
  - 收到消息后 fanout 给房间内所有连接
- [x] 播放状态写入 `RoomStateCache`（新成员加入时拉取快照同步进度）
- [x] 消息类型处理：`play_control`（权限校验）、`room_event`（成员变化）
- [x] 心跳检测（Ping/Pong）与断线清理
- [x] 房间销毁时清理 Hub 和 `RoomStateCache`

#### 接口设计

```
POST   /api/rooms                    → 创建房间
GET    /api/rooms                    → 房间列表
GET    /api/rooms/:roomId            → 房间详情
DELETE /api/rooms/:roomId            → 销毁房间（房主/admin）
POST   /api/rooms/:roomId/join       → 加入房间（私有房间需密码）
POST   /api/rooms/:roomId/kick/:uid  → 踢出成员（房主/admin）

WebSocket /ws/room/:roomId           → 房间实时通信连接
GET       /api/rooms/:roomId/state   → 房间播放状态快照（初次加入同步用）
```

---

### 阶段 4：Vue 前端页面

**目标**：提供多人同步观看视频的前端界面。

#### 页面/组件清单

- [x] **登录 / 注册页**：账号密码注册与登录
- [x] **首页/大厅**：展示公开房间列表，支持创建房间
- [x] **房间创建弹窗**：房间名、公开/私有、可选密码
- [x] **房间页**（核心，基础交互已完成）：
  - [x] 真实视频播放器（HLS.js / 原生 MP4）接入
  - [x] 播放控制栏权限控制：房主/admin 可操作，普通成员只读
  - [x] 视频队列拖拽排序、删除、添加 URL 或从视频库选择
  - [x] 在线成员列表基础展示已完成；踢出交互已接入后端调用
- [x] **管理员后台**：下载任务、视频库管理、房间监控基础交互已接入
- [x] WebSocket 连接管理（composable `useRoomSocket`）：
  - 接收 `sync` 消息后同步本地播放器进度
  - 普通成员播放器控制栏置为禁用只读

---

### 阶段 5：视频下载系统

**目标**：管理员/房主提交 URL，服务端下载视频并缓存到服务器。

#### 支持格式

| 格式/来源 | 工具 | 备注 |
|-----------|------|------|
| mp4 / mkv / webm 等直链 | Go 原生 HTTP 下载 | 支持断点续传 |
| m3u8 (HLS) | FFmpeg | 合并为 mp4 存储 |
| YouTube / Bilibili / 其他站点 | yt-dlp | 需服务器安装 yt-dlp |
| 磁力链接 / BT 种子 | aria2 RPC | 需服务器运行 aria2 |

#### 接口设计

```
POST   /api/admin/downloads           → 提交下载任务
GET    /api/admin/downloads           → 下载任务列表（含进度/状态）
GET    /api/admin/downloads/:taskId   → 查询单个任务状态
DELETE /api/admin/downloads/:taskId   → 取消下载任务
```

#### 开发任务

- [x] 下载任务队列（Go channel + goroutine worker pool），任务状态持久化到 `DownloadTaskStore`
- [x] mp4/mkv 等直链下载，支持进度上报（Content-Length + 已下载字节数）
- [x] m3u8 下载：调用 FFmpeg 合并 HLS 流为 mp4
- [x] yt-dlp 集成（`pkg/ytdlp/`）：exec 调用，解析 stdout 进度输出
- [x] aria2 RPC 集成：提交磁力链接，轮询进度（依赖外部 aria2 RPC 服务）
- [x] 下载完成后自动触发元数据提取（见阶段 6）
- [x] 任务状态实时推送（复用 WebSocket 管理频道 `/ws/admin/downloads`）
- [x] 文件存储路径规划（按日期 + 任务 ID 命名）

---

### 阶段 6：视频库查询接口

**目标**：客户端查询服务器已缓存视频列表，含封面、名称、时长等元数据。

#### 元数据提取方案

- **时长/格式**：`ffprobe -v quiet -print_format json -show_format <file>`
- **关键帧海报**：`ffmpeg -ss <duration/2> -i <file> -vframes 1 -q:v 2 poster.jpg`
- **存储**：元数据写入 `VideoStore`，海报存静态目录并通过 HTTP 暴露

#### 数据库表设计（`videos`）

| 字段 | 类型 | 说明 |
|------|------|------|
| id | UUID/TEXT | 主键 |
| title | TEXT | 视频名称 |
| file_path | TEXT | 服务器存储路径 |
| poster_path | TEXT | 海报图片路径 |
| duration | REAL | 时长（秒）|
| format | TEXT | 格式（mp4/mkv/...）|
| size | INTEGER | 文件大小（字节）|
| source_url | TEXT | 原始下载 URL |
| created_at | TEXT | 入库时间（RFC3339）|
| status | TEXT | ready / processing / error |

#### 接口设计

```
GET    /api/videos              → 视频列表（分页 + 关键词搜索）
GET    /api/videos/:id          → 视频详情
DELETE /api/admin/videos/:id    → 管理员删除视频
GET    /static/posters/:id.jpg  → 海报静态文件
GET    /static/videos/:path     → 视频文件直链（支持 Range 请求）
```

#### 开发任务

- [x] `pkg/ffmpeg/` 封装：`ffprobe` 提取时长与格式
- [x] `pkg/ffmpeg/` 封装：关键帧提取生成海报
- [x] 视频列表查询接口（分页 + 关键词，调用 `VideoStore`）
- [x] 视频文件 Range 请求支持（静态文件服务支持 `bytes=` Header，另提供 `/api/videos/:id/file`）
- [x] 视频删除接口（同时清理文件与数据库记录）

---

## 项目目录结构（规划）

```
watchtogether/
├── cmd/
│   └── server/
│       └── main.go           # 程序入口，依赖注入，根据配置选择底层实现
├── internal/
│   ├── config/               # 配置结构与加载
│   ├── model/                # 领域模型（User, Room, Video, DownloadTask, RoomState）
│   ├── store/
│   │   ├── store.go          # 所有 Repository Interface 定义
│   │   ├── sqlite/           # SQLite 实现（开发/单机）
│   │   ├── postgres/         # PostgreSQL 实现（生产）
│   │   └── testutil/         # 接口一致性测试用例（两种实现共用）
│   ├── cache/
│   │   ├── cache.go          # SessionCache / RoomStateCache / PubSub Interface 定义
│   │   ├── memory/           # 内存实现（开发/单机）
│   │   └── redis/            # Redis 实现（生产）
│   ├── service/              # 业务逻辑层（只依赖 Interface，不依赖具体实现）
│   ├── api/                  # HTTP Handler + 路由注册
│   ├── auth/                 # 注册/登录、密码哈希、JWT 签发与校验
│   ├── room/                 # WebSocket Hub + 房间管理
│   ├── download/             # 下载任务队列
│   └── middleware/           # Gin 中间件（日志、鉴权、RBAC）
├── pkg/
│   ├── apierr/               # 统一错误码与响应格式
│   ├── ffmpeg/               # FFmpeg/ffprobe 封装
│   ├── ytdlp/                # yt-dlp 封装
│   └── aria2/                # aria2 RPC 封装
├── frontend/
│   ├── src/
│   │   ├── views/            # 页面组件
│   │   ├── components/       # 通用组件
│   │   ├── stores/           # Pinia 状态管理
│   │   └── composables/      # useRoomSocket 等
│   └── vite.config.ts
├── config.yaml               # 默认配置（STORAGE_BACKEND=sqlite, CACHE_BACKEND=memory）
└── README.md
```

---

## CI/CD 与容器化部署

> **已接入**：M6 已落地 Dockerfile、生产/开发 Compose、`.env.example`、GitHub Actions CI 与 CD 工作流。

### 需求目标

- [x] 代码推送到 `main` 分支或合并 PR 时，自动触发 CI 流水线（Go 测试 + 前端构建）
- [x] 打 Tag（`v*.*.*`）时，自动触发 CD 流水线，构建多平台 Docker 镜像并推送到镜像仓库
- [x] 镜像仓库目标：GitHub Container Registry（GHCR），可选同步推送到 Docker Hub
- [x] 生产部署通过 Docker Compose 完成，镜像内需保证 `ffmpeg` 与 `yt-dlp` 可用

### CI 流水线需求（`.github/workflows/ci.yml`）

触发时机：`push` 到 `main` 分支 / 任意 PR 到 `main`

流程步骤：
1. 检出代码
2. 安装 Go 1.25 工具链（含缓存）
3. 在 CI 环境中安装 `ffmpeg`（apt）与 `yt-dlp`（pip），确保工具可用分支被测试覆盖
4. `go test -race ./...`（含竞态检测）
5. 安装 Node.js 20，执行前端依赖安装与生产构建

### CD 流水线需求（`.github/workflows/cd.yml`）

触发时机：推送符合 `v*.*.*` 格式的 Git Tag

流程步骤：
1. 检出代码
2. 配置 Docker Buildx（多平台：`linux/amd64` + `linux/arm64`）
3. 登录 GHCR（使用 `GITHUB_TOKEN`，GitHub 自动注入，无需额外配置）
4. 构建镜像并推送，镜像 Tag 自动按语义版本命名（`1.2.3` / `1.2` / `latest`）

可选 Secret（不配置则跳过 Docker Hub 推送）：

| Secret 名称 | 用途 |
|-------------|------|
| `GITHUB_TOKEN` | 推送到 GHCR（自动注入）|
| `DOCKERHUB_USERNAME` | 推送到 Docker Hub（可选）|
| `DOCKERHUB_TOKEN` | Docker Hub Access Token（可选）|

### Docker 镜像设计需求（`Dockerfile`）

采用**多阶段构建**以减小最终镜像体积：

- **frontend-builder 阶段**：`node:20-alpine` — 安装前端依赖并构建静态资源
- **backend-builder 阶段**：`golang:1.25-alpine` — 编译 Go 二进制（静态链接，`CGO_ENABLED=0`）
- **runtime 阶段**：`ubuntu:24.04` — 通过 `apt` 安装 `ffmpeg`，从 GitHub Release 下载 `yt-dlp` 静态二进制
- 以非 root 用户运行，包含 `HEALTHCHECK`
- 持久化数据目录（视频文件、数据库）通过 Volume 挂载至 `/data`

> 选用 Ubuntu 而非 Alpine 作为 runtime 的原因：`ffmpeg` 在 Ubuntu 官方源中编解码器更完整，`yt-dlp` 依赖生态更健全。

### Docker Compose 部署需求

提供两个 Compose 文件：

| 文件 | 用途 | 依赖 |
|------|------|------|
| `docker-compose.yml` | 生产环境 | PostgreSQL 15 + Redis 7 + App |
| `docker-compose.dev.yml` | 开发验证容器行为 | 仅 App（SQLite + 内存缓存）|

环境变量通过 `.env` 文件注入（参考 `.env.example` 模板）。

---

## 工具可用性检查（Graceful Degradation）

> **已接入**：服务启动时检测本地工具并生成能力报告；缺失工具只会关闭对应功能，不影响服务启动。

### 需求目标

服务**启动时**检查本地系统工具是否可用。若工具缺失，**禁用对应功能而非启动失败**（优雅降级），确保服务在不同部署环境下均可正常运行。

### 工具与功能映射

| 工具 | 依赖功能 | 缺失时行为 |
|------|----------|-----------|
| `ffmpeg` | m3u8 HLS 流合并为 mp4、关键帧海报生成 | 禁用 HLS 下载与海报生成；视频可正常播放但无封面 |
| `ffprobe` | 视频时长 / 格式 / 分辨率元数据自动提取 | 禁用自动元数据提取；用户上传时需手动填写 |
| `yt-dlp` | YouTube / Bilibili 等站点视频下载 | 禁用从上述站点导入功能；直链和 m3u8 下载不受影响 |
| `aria2c` | 磁力链接 / BT 种子下载 | 禁用磁力链接下载 |

### Go 实现状态

- [x] 在 `pkg/ffmpeg/`、`pkg/ytdlp/`、`pkg/aria2/` 各包中实现 `CheckAvailability()` 函数
- [x] 检测方式：`exec.LookPath` 确认工具在 PATH 中，再执行 `--version` 命令双重确认可运行
- [x] 函数设有超时（默认 5 秒，汇总检测整体 10 秒），不阻塞服务启动
- [x] 在 `internal/capabilities/` 汇总所有检测结果，推导功能开关，并在启动日志中打印摘要
- 开发任务：
  - [x] `pkg/ffmpeg/` — 检测 `ffmpeg` / `ffprobe` 可用性，返回版本信息
  - [x] `pkg/ytdlp/` — 检测 `yt-dlp` 可用性，返回版本信息
  - [x] `pkg/aria2/` — 检测 `aria2c` 可用性，返回版本信息
  - [x] `internal/capabilities/` — 汇总检测结果，推导功能开关，打印启动日志

### API 能力暴露接口需求

提供接口供前端查询当前可用功能。后端接口已接入，前端动态禁用对应 UI 入口仍待完善：

```
GET /api/capabilities
```

响应格式：

```json
{
  "ffmpeg": true,
  "ffprobe": true,
  "ytdlp": false,
  "aria2": false,
  "tools": {
    "ffmpeg": {"available": true, "version": "ffmpeg version ..."},
    "ffprobe": {"available": true, "version": "ffprobe version ..."},
    "ytdlp": {"available": false, "error": "yt-dlp not found"},
    "aria2": {"available": false, "error": "aria2c not found"}
  },
  "features": {
    "hls_download": true,
    "poster_generation": true,
    "metadata_extract": true,
    "ytdlp_import": false,
    "magnet_download": false
  }
}
```
---

## 里程碑（Milestone）

| 阶段 | 核心交付物 |
|------|-----------|
| M0 — 存储接口层 | Interface 定义完成；SQLite + 内存实现通过接口一致性测试 |
| M1 — 基础框架 | Go 服务启动、账号密码登录与 JWT 可用（SQLite 存用户）；工具可用性检查集成 |
| M2 — 房间核心 | 房间创建/加入、WebSocket 连接、播放同步基本可用（内存 PubSub）|
| M3 — 前端集成 | 基础 Vue 页面、WebSocket 控制、真实播放器、队列管理与管理后台基础交互已接入 |
| M4 — 下载系统 | 后端下载任务队列、m3u8/mp4/yt-dlp/aria2 路径已接入（工具缺失时优雅降级）|
| M5 — 视频库 | 后端元数据提取、海报生成、查询接口、视频选择与管理基础交互已接入 |
| M6 — CI/CD | GitHub Actions CI（Go 测试 + 前端构建）与 CD（Docker 多平台镜像推送）完整流水线 |
| M7 — 生产化 | 切换至 PostgreSQL + Redis 实现，多实例部署测试，性能调优 |

---

## 依赖与环境要求

### 开发环境（最低配置，2c2g 可运行）

- Go 1.25+
- Node.js 20+
- FFmpeg 6+（含 ffprobe）
- yt-dlp（管理员下载功能需要）
- aria2（磁力链接下载需要）
- **无需** PostgreSQL，**无需** Redis（使用 SQLite + 内存）

### 生产环境

- 以上所有开发依赖
- PostgreSQL 15+
- Redis 7+
- 切换方式：设置环境变量 `STORAGE_BACKEND=postgres` + `CACHE_BACKEND=redis` 并配置对应 DSN/地址

### 使用 Docker

已提供 `Dockerfile`、`docker-compose.yml`、`docker-compose.dev.yml` 与 `.env.example`。生产 Compose 默认启用 PostgreSQL + Redis，开发 Compose 使用 SQLite + 内存缓存快速验证容器行为。

### 配置示例（`config.yaml`）

```yaml
addr: ":8080"

# 存储后端: sqlite | postgres
storage_backend: sqlite
sqlite_path: "./data/watchtogether.db"
postgres_dsn: "postgres://user:pass@localhost:5432/watchtogether?sslmode=disable"

# 缓存后端: memory | redis
cache_backend: memory
redis_addr: "localhost:6379"

# JWT
jwt_secret: "change-me-in-production"
jwt_access_ttl: "15m"
jwt_refresh_ttl: "7d"

# 文件存储
storage_dir: "./data/videos"
```
