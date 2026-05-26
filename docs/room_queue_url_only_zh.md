# 房间 URL 队列与播放同步

> 创建时间：2026-05-26  
> 面向：`WatchTvTogether`（Go API + Redis）与 `WatchTvTogether-Web`（Vue 3）。  
> 背景：全局影片库（`videos` 表、`GET /api/videos*`）已移除，房间播放完全依赖 **外链 URL 队列**，适配 Vercel 等无持久磁盘部署。

---

## 1. 架构摘要

| 层级 | 职责 |
|------|------|
| **PostgreSQL** | 房间元数据（名称、可见性、房主）；**不存**片源或队列 |
| **Redis** | 权威播放状态（`RoomState`：当前 URL、`queue[]`、`position`、`video_duration` 等） |
| **Ably** | 实时 `room.sync` / `room.event` / `room.snapshot` 推送 |
| **客户端** | 解析 URL 直接播放（mp4 / HLS）；房主上报 `video_duration` 供服务端进度投影 |

```
房主浏览器 ──POST /control──► Go API ──► Redis (RoomState)
                │                      └──► Ably room.sync
成员浏览器 ◄── Ably 订阅 ────────────────┘
成员浏览器 ──GET /state 或 /snapshot──► 投影后的 position
```

---

## 2. 队列 URL 规则

实现：`pkg/queueurl`（后端）、`src/utils/queueUrl.ts`（前端，规则一致）。

**有效条目**（`video_id` 与 `queue[]` 中每一项）：

- `http://…` 或 `https://…`
- 协议相对 URL：`//cdn.example.com/video.m3u8`（至少 3 字符）

**无效 / 会被过滤**：空串、相对路径（`/static/foo.mp4`）、UUID、旧版 catalog `video_id`。

后端在 `POST /api/rooms/:roomId/control` 中：

- `queue` 经 `FilterPlaybackURLs` 过滤，仅保留合法 URL，顺序不变
- 非空 `video_id` 必须是合法 URL，否则 `400`：`video_id must be an http(s) or protocol-relative URL`

---

## 3. 控制 API：`POST /api/rooms/:roomId/control`

**权限**：房主或管理员（`canManageRoom`）。

**请求体**（JSON）：

| 字段 | 类型 | 说明 |
|------|------|------|
| `action` | string | **必填**。`play` \| `pause` \| `seek` \| `next` \| `switch` |
| `position` | number | 当前进度（秒） |
| `video_id` | string | 当前播放 URL；须为合法外链 |
| `queue` | string[] | 完整队列 URL 列表（有序） |
| `playback_mode` | string | `sequential`（默认）或 `loop` |
| `control_version` | number | 客户端已知版本；低于服务端则 `409 stale control version` |
| `video_duration` | number | **可选**。当前片源时长（秒）；见 §4 |

**行为要点**（`internal/room/hub.go` → `ApplyControl`）：

- 每次成功写入递增 `control_version`，并发布 Ably `room.sync`
- `action=next`：按 `playback_mode` 在队列中取下一 URL，切到新片并 `pause`、`position=0`
- `action=switch`：若未传 `video_id` 则切到 `queue[0]`
- 若 `queue` 为空但 `video_id` 非空，自动将 `video_id` 设为单元素队列

**响应**：与 Ably `room.sync` 载荷同结构的 `Message` JSON。

---

## 4. `video_duration` 与进度投影

**问题**：成员拉取 `GET /state` / `POST /snapshot` 时，若房主正在播放，服务端需将 Redis 中的 `position` 投影到「当前时刻」，避免每人本地计时漂移。

**做法**：

1. 房主在 `<video>` 的 `loadedmetadata` 后，若拿到有效 `duration`，通过 `control` 上报 `video_duration`（Web：`RoomView.vue` → `onVideoLoadedMetadata`）。
2. 服务端写入 Redis `RoomState.VideoDuration`；若本次 control 未带且片源 URL 未变，保留上次值（`resolveControlDuration`）。
3. `ProjectedRoomState` / `projectState` 用 `video_duration` 计算播放 elapsed；到达片尾时按 `playback_mode` 自动切下一首或暂停。

**约束**：

- `video_duration <= 0` 时仍投影 position，但**不会**在片尾自动切歌（无可靠终点）
- 切换 `video_id` 后 duration 清零，直至新房主再次上报

前端投影辅助：`src/utils/roomStateProjection.ts`（`advancePlayPositionSinceServerUpdatedAt` 等）。

---

## 5. 快照与轻量状态

| 接口 | 用途 |
|------|------|
| `POST /api/rooms/:roomId/snapshot` | 进房初始化：Redis 状态 + `queue` + Ably 频道名 + 在线人数 |
| `GET /api/rooms/:roomId/state` | 轻量轮询；返回**已投影**的 `RoomState` |

两者均在 `play` 状态下返回投影后的 `position` 与 `updated_at`；`snapshot` 额外含 `base_updated_at` 供客户端二次微调。

---

## 6. 前端队列 UX（无全局片库）

- **添加**：房主在工具抽屉输入 URL（可选本地展示名）；展示名仅存浏览器内存（`queueDisplayTitles`），**不**持久化到后端
- **同步**：队列变更通过 `control` 提交完整 `queue[]`（真实 URL 列表）
- **展示**：`shortTitleFromUrl` 从 URL 推导默认标题；支持本机重命名

---

## 7. 已删除能力（勿再依赖）

- `GET /api/videos*`、`videos` 表、`VideoStore`
- `GET /api/videos/:id/file`、`GET /static/*`
- Admin 视频库 CRUD、`GET/POST/DELETE /api/admin/downloads*`
- `GET /api/capabilities` 中的 `ffmpeg`、`ytdlp`、`aria2` 等字段（当前返回 `{ "features": {} }`）

---

## 8. 本地验证

```bash
# 后端（需 Postgres + Redis）
go test ./pkg/queueurl/... ./internal/room/... ./internal/api/...

# 示例 control（Bearer token + 房主权限）
curl -X POST "$API/api/rooms/$ROOM/control" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"action":"play","position":0,"video_id":"https://example.com/a.mp4","queue":["https://example.com/a.mp4"],"video_duration":120}'
```

---

## 9. 相关文件

| 路径 | 说明 |
|------|------|
| `pkg/queueurl/queueurl.go` | URL 校验 |
| `internal/api/room_handlers.go` | control / snapshot 路由 |
| `internal/room/hub.go` | ApplyControl、投影、next/switch |
| `internal/model/model.go` | `RoomState`、`PlaybackMode` |
| `WatchTvTogether-Web/src/views/RoomView.vue` | 队列 UI、duration 上报 |
| `WatchTvTogether-Web/src/utils/queueUrl.ts` | 前端 URL 校验 |
