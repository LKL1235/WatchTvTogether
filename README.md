# 🎬 WatchTogether — 同步视频观看平台

> 基于 Go 后端 + Vue 前端的多人同步视频观看服务，支持 m3u8/mp4 等格式、房间管理、OAuth 鉴权、服务器视频缓存与下载。
> 单体 Go 应用，接口分层设计，存储后端可替换（开发阶段使用 SQLite + 内存，生产可无缝切换至 PostgreSQL + Redis）。

---

## 技术栈概览

| 层级 | 技术选型 |
|------|----------|
| 后端 | Go（Gin）、WebSocket（gorilla/websocket）、FFmpeg、yt-dlp / aria2 |
| 前端 | Vue 3 + Vite、Pinia、HLS.js / Video.js |
| 鉴权 | OAuth 2.0（GitHub / Google 等第三方）+ JWT |
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
    GetByProviderID(ctx context.Context, provider, providerID string) (*model.User, error)
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

- [ ] 在 `internal/store/store.go` 定义 `UserStore`、`RoomStore`、`VideoStore`、`DownloadTaskStore` interface
- [ ] 在 `internal/cache/cache.go` 定义 `SessionCache`、`RoomStateCache`、`PubSub` interface
- [ ] 在 `internal/model/` 定义所有领域模型结构体（`User`、`Room`、`Video`、`DownloadTask`、`RoomState`）

#### 0.2 SQLite 实现（开发阶段底层）

- [ ] 引入 `modernc.org/sqlite`（纯 Go，无 CGO 依赖）
- [ ] 实现 `internal/store/sqlite/` 下所有 Store interface
- [ ] 编写 SQLite schema 初始化脚本（`internal/store/sqlite/schema.sql`）
- [ ] 实现自动建表（首次启动时执行 schema，无需迁移工具）

#### 0.3 内存实现（开发阶段底层）

- [ ] 实现 `internal/cache/memory/session_cache.go`（内存 Map + sync.RWMutex，支持 TTL）
- [ ] 实现 `internal/cache/memory/room_state_cache.go`（内存 Map）
- [ ] 实现 `internal/cache/memory/pubsub.go`（内存 channel，支持多订阅者 fanout）

#### 0.4 PostgreSQL 实现（生产阶段底层，可后续补充）

- [ ] 实现 `internal/store/postgres/` 下所有 Store interface（使用 `pgx/v5`）
- [ ] 编写 SQL 迁移脚本（`migrations/`，使用 `golang-migrate`）

#### 0.5 Redis 实现（生产阶段底层，可后续补充）

- [ ] 实现 `internal/cache/redis/` 下所有 Cache interface（使用 `go-redis/v9`）

#### 0.6 配置与依赖注入

- [ ] 在 `internal/config/` 定义配置结构（支持环境变量 + 配置文件）
- [ ] 在 `cmd/server/main.go` 实现工厂函数：根据 `STORAGE_BACKEND=sqlite|postgres` 和 `CACHE_BACKEND=memory|redis` 动态选择实现
- [ ] 编写接口实现一致性测试（`internal/store/testutil/`），SQLite 和 Postgres 实现均需通过同一套测试用例

---

### 阶段 1：Go 后端基础框架

- [ ] 初始化 Go Module（`go mod init`），配置项目目录结构
- [ ] 引入 Gin 路由框架，配置基础中间件（日志、Recovery、CORS）
- [ ] 统一错误响应格式与错误码定义（`pkg/apierr/`）
- [ ] 健康检查接口 `GET /healthz`
- [ ] 静态文件服务（视频文件、海报图片）
- [ ] 配置加载（`config.yaml` + 环境变量覆盖）

---

### 阶段 2：用户 OAuth 鉴权系统

**目标**：接入第三方 OAuth 2.0 登录，区分管理员与普通用户权限。

#### 功能清单

- [ ] 接入 OAuth 2.0 Provider（GitHub / Google，可配置）
- [ ] OAuth 回调处理，生成 JWT（Access Token + Refresh Token）
- [ ] 用户角色体系：
  - `admin`：全部管理权限（用户管理、全局视频库、强制销毁任意房间）
  - `user`：可创建房间，在自己房间内行使房主权限
- [ ] JWT 中间件鉴权，路由级别 RBAC 权限控制
- [ ] 用户信息存储（头像、昵称、绑定账号）—— 调用 `UserStore`
- [ ] Token 刷新接口，调用 `SessionCache.SetRefreshToken`
- [ ] 登出接口，调用 `SessionCache.BlacklistToken`（JWT 黑名单）

#### 接口设计

```
GET  /api/auth/login/:provider     → 跳转 OAuth 授权页
GET  /api/auth/callback/:provider  → OAuth 回调，返回 JWT
POST /api/auth/refresh             → 刷新 Token
POST /api/auth/logout              → 登出
GET  /api/users/me                 → 获取当前用户信息
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

- [ ] 房间 CRUD 接口，调用 `RoomStore`
- [ ] WebSocket Hub 实现（`internal/room/hub.go`）：
  - 每个房间一个 Hub，管理该房间所有 WebSocket 连接
  - 通过 `PubSub` 接收广播消息（解耦消息来源，支持未来多实例）
  - 收到消息后 fanout 给房间内所有连接
- [ ] 播放状态写入 `RoomStateCache`（新成员加入时拉取快照同步进度）
- [ ] 消息类型处理：`play_control`（权限校验）、`room_event`（成员变化）
- [ ] 心跳检测（Ping/Pong）与断线清理
- [ ] 房间销毁时清理 Hub 和 `RoomStateCache`

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

- [ ] **登录页**：OAuth 登录入口
- [ ] **首页/大厅**：展示公开房间列表，支持创建房间
- [ ] **房间创建弹窗**：房间名、公开/私有、可选密码
- [ ] **房间页**（核心）：
  - 视频播放器（HLS.js 支持 m3u8，Video.js 支持 mp4）
  - 播放控制栏（仅房主/admin 可操作：播放/暂停/拖进度/切换视频）
  - 视频队列面板（房主可拖拽排序、删除、添加 URL 或从视频库选择）
  - 在线成员列表（头像/昵称，房主可踢出）
- [ ] **管理员后台**：用户管理、视频库管理、房间监控
- [ ] WebSocket 连接管理（composable `useRoomSocket`）：
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

- [ ] 下载任务队列（Go channel + goroutine worker pool），任务状态持久化到 `DownloadTaskStore`
- [ ] mp4/mkv 等直链下载，支持进度上报（Content-Length + 已下载字节数）
- [ ] m3u8 下载：调用 FFmpeg 合并 HLS 流为 mp4
- [ ] yt-dlp 集成（`pkg/ytdlp/`）：exec 调用，解析 stdout 进度输出
- [ ] aria2 RPC 集成（`pkg/aria2/`）：提交磁力链接，轮询进度
- [ ] 下载完成后自动触发元数据提取（见阶段 6）
- [ ] 任务状态实时推送（复用 WebSocket 管理频道）
- [ ] 文件存储路径规划（按日期/ID 分目录）

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

- [ ] `pkg/ffmpeg/` 封装：`ffprobe` 提取时长/分辨率/编码格式
- [ ] `pkg/ffmpeg/` 封装：关键帧提取生成海报
- [ ] 视频列表查询接口（分页 + 关键词，调用 `VideoStore`）
- [ ] 视频文件 Range 请求支持（`bytes=` Header，支持拖进度播放）
- [ ] 视频删除接口（同时清理文件与数据库记录）

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
│   ├── auth/                 # OAuth + JWT 逻辑
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

## 里程碑（Milestone）

| 阶段 | 核心交付物 |
|------|-----------|
| M0 — 存储接口层 | Interface 定义完成；SQLite + 内存实现通过接口一致性测试 |
| M1 — 基础框架 | Go 服务启动、OAuth 登录可用（SQLite 存用户）|
| M2 — 房间核心 | 房间创建/加入、WebSocket 连接、播放同步基本可用（内存 PubSub）|
| M3 — 前端集成 | Vue 播放器页面、控制栏权限、队列管理 UI |
| M4 — 下载系统 | 下载任务队列、m3u8/mp4/yt-dlp/aria2 全格式支持 |
| M5 — 视频库 | 元数据提取、海报生成、查询接口、前端视频库页面 |
| M6 — 生产化 | 切换至 PostgreSQL + Redis 实现，多实例部署测试，性能调优 |

---

## 依赖与环境要求

### 开发环境（最低配置，2c2g 可运行）

- Go 1.22+
- Node.js 20+
- FFmpeg 6+（含 ffprobe）
- yt-dlp（管理员下载功能需要）
- aria2（磁力链接下载需要）
- **无需** PostgreSQL，**无需** Redis（使用 SQLite + 内存）

#### Docker 镜像与 Compose

仓库根目录提供 `Dockerfile` 与 `docker-compose.yml`：基础镜像为 Ubuntu 24.04，通过 `apt` 安装 **FFmpeg 6+**（含 `ffprobe`）与 **aria2**，并下载官方 **yt-dlp** 单文件到 `/usr/local/bin/yt-dlp`，供容器内 `PATH` 使用。

- 构建并运行（需本机已安装 Docker / Compose）：

  ```bash
  docker compose up --build
  ```

- 服务启动后可在容器内用 **GET** `/readyz` 或 **GET** `/api/deps` 查看 `ffmpeg` / `ffprobe` / `yt-dlp` / `aria2c` 是否可用及版本；全部就绪时返回 **200**，否则 **503**（用于编排就绪探针）。

- 可覆盖可执行文件路径（例如自定义安装位置）：

  | 环境变量        | 说明        | 默认       |
  |-----------------|-------------|------------|
  | `FFMPEG_PATH`   | `ffmpeg`    | `PATH` 中  |
  | `FFPROBE_PATH`  | `ffprobe`   | `PATH` 中  |
  | `YT_DLP_PATH`   | `yt-dlp`    | `PATH` 中  |
  | `ARIA2C_PATH`   | `aria2c`    | `PATH` 中  |

### 生产环境

- 以上所有开发依赖
- PostgreSQL 15+
- Redis 7+
- 切换方式：设置环境变量 `STORAGE_BACKEND=postgres` + `CACHE_BACKEND=redis` 并配置对应 DSN/地址

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

# OAuth
oauth:
  github:
    client_id: ""
    client_secret: ""
  google:
    client_id: ""
    client_secret: ""

# 文件存储
storage_dir: "./data/videos"
```
