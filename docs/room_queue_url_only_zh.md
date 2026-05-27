# 房间播放队列（URL-only）

> 创建时间：2026-05-27  
> 覆盖代码：`pkg/queueurl`、`internal/api/room_handlers.go`（`control`）、`internal/room/hub.go`（`ApplyControl` / `ProjectedRoomState`）、Redis `RoomState`。

全局影片库与 `GET /api/videos*` 已移除。房间内的「当前片源」与「队列」均为**外链播放地址**，由房主通过 `POST /api/rooms/:roomId/control` 写入 Redis，经 Ably `room.sync` 同步到各端。

---

## 1. URL 校验规则

与前端 `WatchTvTogether-Web` 的 `src/utils/queueUrl.ts` 对齐，后端以 `pkg/queueurl` 为准：

| 形式 | 示例 | 是否接受 |
|------|------|----------|
| `https://` | `https://cdn.example/v.mp4` | 是 |
| `http://` | `http://example/stream.m3u8` | 是 |
| 协议相对 `//` | `//cdn.example/v.mp4` | 是（长度 > 2） |
| 相对路径、裸文件名 | `/static/a.mp4`、`a.mp4` | **否** |
| 空字符串 | | **否** |

- `video_id`：若非空，须通过 `IsPlaybackURL`；否则返回 `400`，`message` 含 `video_id must be an http(s) or protocol-relative URL`。
- `queue[]`：经 `FilterPlaybackURLs` **按原顺序**保留合法项，非法项**静默丢弃**（不报错）。

---

## 2. 控制接口契约

**`POST /api/rooms/:roomId/control`**（须登录；房主或管理员）

请求体（节选）：

```json
{
  "action": "play",
  "position": 12.5,
  "video_id": "https://example.com/ep1.m3u8",
  "queue": ["https://example.com/ep1.m3u8", "https://example.com/ep2.m3u8"],
  "playback_mode": "sequential",
  "control_version": 3,
  "video_duration": 3600
}
```

| 字段 | 说明 |
|------|------|
| `action` | `play` / `pause` / `seek` / `switch` / `next` 等（见 `model.PlaybackAction`） |
| `video_id` | 当前播放 URL；须为合法外链（见上表） |
| `queue` | URL 列表；非法项被过滤 |
| `playback_mode` | `sequential`（默认）或 `loop` |
| `control_version` | 客户端已知版本；若 **小于** 服务端 `control_version` → `409` `stale control version` |
| `video_duration` | 可选，当前片源时长（秒）；用于进度投影与播完检测 |

成功时响应为 `room.sync` 形态的 `Message`（含 `control_version`、`queue` 等）。

---

## 3. `video_duration` 与进度投影

- 房主在浏览器加载 metadata 后，应在 control 请求中带上 `video_duration`（前端见 `roomStateProjection.ts` 与 RoomView metadata 回调）。
- 服务端 `resolveControlDuration`：请求中 `video_duration > 0` 时写入；否则若 `video_id` 未变则沿用 Redis 中上一时长。
- **`GET /api/rooms/:roomId/state`** 与 **`POST /api/rooms/:roomId/snapshot`** 返回的 `state` 经 `ProjectedRoomState`：
  - `action === play` 时按 `updated_at` 将 `position` 向前推算；
  - 若 `video_duration > 0` 且推算位置到达片尾，可按 `playback_mode` 切下一首或暂停。
- 快照中的 `base_updated_at` 表示投影前的权威控制时间，供客户端避免重复投影（见前端 `advancePlayPositionSinceServerUpdatedAt`）。

---

## 4. 与已删除能力的关系

- 不再有 `video` 表驱动的全局 catalog、`GET /api/videos/:id/file`、`GET /static/*`。
- 封面、展示名：**不**由后端队列存储；前端可在本地为 URL 记展示名，同步给其他成员的仍是 URL 列表。
- `GET /api/capabilities` 已删除；客户端勿再探测服务端下载/转码能力。

---

## 5. 相关文档

- 实时同步与 Ably 频道：[room_chat_realtime_design_zh.md](./room_chat_realtime_design_zh.md)（聊天与控制共用频道）
- 前端队列校验：`WatchTvTogether-Web` → `src/utils/queueUrl.ts`
