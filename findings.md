# Findings: chat_design 与后端

## Web `docs/chat_design.md`

- **决策 #14**：不改 API、Ably 事件、消息结构。
- **§11 数据流**：`GET/POST /api/rooms/:id/chat` + Ably `room.chat`。
- 侧边栏仅为 UI/布局；宽度/折叠存 `localStorage`，**无后端接口**。

## 后端现状（main）

| 能力 | 位置 | 状态 |
|------|------|------|
| POST/GET chat | `internal/api/room_handlers.go` | 已实现 |
| Redis Stream + pending | `internal/cache/redis/room_chat.go` | 已实现 |
| SendChat / ListChat | `internal/room/chat.go` | 已实现 |
| CloseRoom 删聊天 key | `hub.go` → `deleteRoomChat` | 已实现 |
| Ably `room.chat` | `realtime/ably/service.go` | 已实现 |
| 全局 pending 补发 | `router.go` 后台 goroutine | 已实现 |
| 413/503/429 | `apierr` + handlers | 已实现 |

## 缺口（相对 room_chat_realtime_design_zh.md §10）

- 无 `room_chat` Redis 单元测试
- 无 `SendChat`/`ListChat` 专注单元测试（hub_test 未覆盖聊天）
