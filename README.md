# 🎬 WatchTogether — 同步视频观看平台

> 基于 Go 后端 + Vue 前端的多人同步视频观看服务，支持 m3u8/mp4 等格式、房间管理、OAuth 鉴权、服务器视频缓存与下载。

---

## 技术栈概览

| 层级 | 技术选型 |
|------|----------|
| 后端 | Go (Gin / Echo)、WebSocket、FFmpeg、yt-dlp / aria2 |
| 前端 | Vue 3 + Vite、Pinia、HLS.js / Video.js |
| 鉴权 | OAuth 2.0（GitHub / Google 等第三方）+ JWT |
| 数据库 | PostgreSQL（用户/房间）+ Redis（房间状态/会话）|
| 存储 | 本地文件系统 / MinIO（可选） |
| 部署 | Docker Compose |

---

## TODO / 开发计划

### 1. Go 后端 Web 服务器基础

- [ ] 初始化 Go 项目结构（`cmd/`, `internal/`, `pkg/`, `api/`）
- [ ] 配置 Gin/Echo 路由框架
- [ ] 集成 PostgreSQL 与 Redis
- [ ] 编写 Docker Compose 一键启动配置
- [ ] 统一错误处理与日志中间件

---

### 1.1 用户 OAuth 鉴权系统

**目标**：接入第三方 OAuth 2.0 登录，区分管理员与普通用户权限。

#### 功能清单

- [ ] 接入 OAuth 2.0 Provider（GitHub / Google，可配置）
- [ ] OAuth 回调处理，生成 JWT Token（Access Token + Refresh Token）
- [ ] 用户角色体系设计：
  - `admin`（管理员）：拥有全部管理权限，包括用户管理、全局视频库管理、强制销毁任意房间等
  - `user`（普通用户）：可创建房间并在自己的房间内行使房主权限
- [ ] JWT 中间件鉴权，路由级别 RBAC 权限控制
- [ ] 用户信息存储（头像、昵称、绑定账号）
- [ ] Token 刷新接口 `POST /api/auth/refresh`
- [ ] 登出接口（Redis 黑名单使 Token 失效）

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

### 1.2 Vue 前端页面

**目标**：提供多人同步观看视频的前端界面，含房间管理与播放控制。

#### 页面/组件清单

- [ ] **登录页**：OAuth 登录入口
- [ ] **首页/大厅**：展示公开房间列表，支持创建房间
- [ ] **房间创建弹窗**：填写房间名、设置公开/私有、设置密码（可选）
- [ ] **房间页**（核心页面）：
  - 视频播放器（HLS.js 支持 m3u8，Video.js 支持 mp4/其他格式）
  - 播放控制栏（仅房主/管理员可见操作按钮：播放/暂停/拖动进度/切换视频）
  - 视频队列面板：显示队列中所有视频，房主可拖拽排序、删除、添加（URL 或从服务器视频库选择）
  - 在线成员列表：显示成员头像/昵称，房主可踢出成员
  - 实时聊天（可选，后续扩展）
- [ ] **管理员后台**：
  - 用户管理（封禁/授权/角色修改）
  - 服务器视频库管理（见 1.4/1.5）
  - 房间监控

#### 前端状态同步逻辑

- 加入房间后建立 WebSocket 连接（见 1.3 推荐方案）
- 收到服务端播放状态推送后，同步本地播放器进度
- 普通成员的播放器控制栏禁用，仅展示状态

---

### 1.3 视频进度同步方案（后端接口设计）

**目标**：实现房间内所有成员播放进度与房主保持一致。

#### 方案对比

| 维度 | WebSocket | SSE (Server-Sent Events) | HTTP 轮询 |
|------|-----------|--------------------------|-----------|
| 通信方向 | 全双工（双向） | 单向（服务端→客户端）| 客户端主动拉取 |
| 实时性 | ⭐⭐⭐ 极高 | ⭐⭐⭐ 高 | ⭐ 低（取决于轮询频率）|
| 服务端复杂度 | 中等（需维护连接状态）| 低 | 最低 |
| 客户端复杂度 | 中等 | 低（浏览器原生支持）| 最低 |
| 连接数占用 | 每客户端一个长连接 | 每客户端一个长连接 | 频繁短连接 |
| 断线重连 | 需手动实现 | 浏览器自动重连 | 天然支持 |
| 房主→服务端推送 | ✅ 原生支持 | ❌ 需额外 HTTP 请求 | ✅（每次轮询即上报）|
| 适合场景 | 高频双向交互 | 低频单向推送 | 简单状态同步 |
| 服务端负载 | 中 | 低 | 高（高频轮询）|
| Go 生态支持 | gorilla/websocket、nhooyr.io/websocket | 标准库即可实现 | 标准库 |
| 横向扩展难度 | 需借助 Redis Pub/Sub 同步跨实例状态 | 同 WebSocket | 易（无状态）|

#### 推荐方案：**WebSocket + Redis Pub/Sub**

**推荐理由**：
1. 播放控制（暂停、拖动进度、切换视频）是**双向、高频**事件，WebSocket 天然契合
2. 成员加入/离开、聊天等扩展功能均需双向通信，WebSocket 一套连接全部覆盖
3. Redis Pub/Sub 作为消息总线，解决多实例部署下的跨节点广播问题
4. SSE 虽然简单，但房主端推送控制指令仍需单独 HTTP 接口，架构割裂

**消息协议设计（JSON）**：

```json
// 客户端 → 服务端（房主控制）
{
  "type": "play_control",
  "action": "seek" | "play" | "pause" | "next" | "switch",
  "position": 123.45,   // 秒，seek 时使用
  "video_id": "uuid"    // switch 时使用
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

#### 接口设计

```
WebSocket  /ws/room/:roomId          → 房间实时通信连接
POST       /api/rooms/:roomId/control → 备用 HTTP 控制接口（兜底）
GET        /api/rooms/:roomId/state   → 获取当前房间播放状态快照（初次加入同步用）
```

#### 开发任务

- [ ] 实现 WebSocket Hub（房间维度，管理所有连接）
- [ ] Redis Pub/Sub 消息广播（支持多实例部署）
- [ ] 播放状态持久化到 Redis（新成员加入时拉取当前进度快照）
- [ ] 成员权限校验（非房主/管理员的控制消息直接拒绝）
- [ ] 心跳检测与断线重连处理
- [ ] 房间销毁时清理所有连接与 Redis 状态

---

### 1.4 管理页面 — 视频下载接口

**目标**：管理员（或房主）在后台提交 URL，由服务端下载视频并缓存到服务器。

#### 支持格式

| 格式/来源 | 工具 | 备注 |
|-----------|------|------|
| mp4 / mkv / webm 等直链 | Go 原生 HTTP 下载 | 支持断点续传 |
| m3u8 (HLS) | FFmpeg (`ffmpeg -i <url>`) | 合并为 mp4 存储 |
| YouTube / Bilibili / 其他站点 | yt-dlp | 需服务器安装 yt-dlp |
| 磁力链接 / BT 种子 | aria2 RPC | 需服务器运行 aria2 daemon |

#### 接口设计

```
POST /api/admin/downloads
Body: {
  "url": "https://...",       // 视频 URL 或磁力链接
  "title": "自定义名称",       // 可选
  "quality": "best"           // yt-dlp 画质选项，可选
}
Response: { "task_id": "uuid", "status": "queued" }

GET  /api/admin/downloads/:taskId  → 查询下载任务状态
GET  /api/admin/downloads          → 下载任务列表（含进度/状态）
DELETE /api/admin/downloads/:taskId → 取消下载任务
```

#### 开发任务

- [ ] 下载任务队列（Go channel + goroutine worker pool）
- [ ] mp4/mkv 等直链下载，支持进度上报（Content-Length + 已下载字节数）
- [ ] m3u8 下载：调用 FFmpeg 合并 HLS 流为 mp4
- [ ] yt-dlp 集成：exec 调用，解析 stdout 进度输出
- [ ] aria2 RPC 集成：提交磁力链接，轮询下载进度
- [ ] 下载完成后自动触发 1.5 中的元数据提取（关键帧/时长）
- [ ] 任务状态实时推送（WebSocket 或 SSE 均可，推荐复用房间 WebSocket 的管理频道）
- [ ] 文件存储路径规划（按日期/ID 分目录，防止文件名冲突）

---

### 1.5 视频库查询接口

**目标**：客户端可以查询服务器已缓存视频列表，获取封面（关键帧海报）、名称、时长等元数据。

#### 元数据提取方案

- **时长**：使用 `ffprobe -v quiet -print_format json -show_format <file>` 解析
- **关键帧海报**：使用 FFmpeg 从视频中间帧提取缩略图
  ```bash
  ffmpeg -ss <duration/2> -i <file> -vframes 1 -q:v 2 poster.jpg
  ```
- **元数据存储**：提取结果存入 PostgreSQL `videos` 表，海报图片存于静态文件目录并通过 HTTP 对外暴露

#### 数据库表设计（`videos`）

| 字段 | 类型 | 说明 |
|------|------|------|
| id | UUID | 主键 |
| title | TEXT | 视频名称（下载时指定或从文件名推断）|
| file_path | TEXT | 服务器存储路径 |
| poster_path | TEXT | 海报图片路径 |
| duration | FLOAT | 视频时长（秒）|
| format | TEXT | 视频格式（mp4/mkv/...）|
| size | BIGINT | 文件大小（字节）|
| source_url | TEXT | 原始下载 URL |
| created_at | TIMESTAMP | 入库时间 |
| status | TEXT | ready / processing / error |

#### 接口设计

```
GET /api/videos
Query: ?page=1&limit=20&keyword=关键词&format=mp4
Response: {
  "total": 100,
  "page": 1,
  "data": [
    {
      "id": "uuid",
      "title": "视频名称",
      "duration": 3600.5,
      "format": "mp4",
      "size": 1073741824,
      "poster_url": "https://.../posters/uuid.jpg",
      "created_at": "2026-04-27T00:00:00Z"
    }
  ]
}

GET /api/videos/:id         → 获取单个视频详情
DELETE /api/admin/videos/:id → 管理员删除视频
GET /static/posters/:id.jpg  → 海报静态文件服务
GET /static/videos/:path     → 视频文件直链（可配合 Range 请求支持拖进度）
```

#### 开发任务

- [ ] `ffprobe` 调用封装：提取时长、分辨率、编码格式
- [ ] FFmpeg 关键帧提取：下载完成后异步生成海报
- [ ] `videos` 数据库表迁移脚本
- [ ] 视频列表查询接口（分页 + 关键词搜索）
- [ ] 海报静态文件服务配置
- [ ] 视频文件 Range 请求支持（`bytes=` 头，支持拖进度直播）
- [ ] 视频删除接口（同时清理文件与数据库记录）

---

## 项目目录结构（规划）

```
watchtogether/
├── cmd/
│   └── server/          # 主程序入口
├── internal/
│   ├── auth/            # OAuth + JWT 逻辑
│   ├── room/            # 房间管理 + WebSocket Hub
│   ├── video/           # 视频库查询 + 元数据
│   ├── download/        # 下载任务队列
│   └── admin/           # 管理员接口
├── pkg/
│   ├── ffmpeg/          # FFmpeg/ffprobe 封装
│   ├── ytdlp/           # yt-dlp 封装
│   └── aria2/           # aria2 RPC 封装
├── migrations/          # 数据库迁移脚本
├── frontend/            # Vue 3 前端项目
│   ├── src/
│   │   ├── views/       # 页面组件
│   │   ├── components/  # 通用组件
│   │   ├── stores/      # Pinia 状态管理
│   │   └── composables/ # WebSocket hooks 等
│   └── vite.config.ts
├── docker-compose.yml
└── README.md
```

---

## 里程碑（Milestone）

| 阶段 | 核心交付物 |
|------|-----------|
| M1 — 基础框架 | Go 服务启动、数据库连通、OAuth 登录可用 |
| M2 — 房间核心 | 房间创建/加入、WebSocket 连接、播放同步基本可用 |
| M3 — 前端集成 | Vue 播放器页面、控制栏权限、队列管理 UI |
| M4 — 下载系统 | 下载任务队列、m3u8/mp4/yt-dlp/aria2 全格式支持 |
| M5 — 视频库 | 元数据提取、海报生成、查询接口、前端视频库页面 |
| M6 — 稳定化 | 多实例部署测试、性能调优、错误处理完善、文档 |

---

## 依赖与环境要求

- Go 1.22+
- Node.js 20+
- PostgreSQL 15+
- Redis 7+
- FFmpeg 6+（含 ffprobe）
- yt-dlp（管理员下载功能需要）
- aria2（磁力链接下载需要）
