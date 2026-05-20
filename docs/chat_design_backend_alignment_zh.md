# chat_design.md 与后端对齐说明

> Web 仓库：`WatchTvTogether-Web/docs/chat_design.md`（Twitch 风格聊天侧栏 UI）  
> 后端仓库：本文档 + `docs/room_chat_realtime_design_zh.md`（聊天数据与实时）

---

## 1. 结论

**前端 `chat_design.md` 不要求后端新增或修改 API。**

| Web 设计项 | 后端职责 |
|------------|----------|
| 可伸缩右侧聊天列、折叠、localStorage 宽度 | 无（纯前端） |
| 移动 Sheet / 工具抽屉 | 无 |
| 进房加载历史、发送消息、实时 `room.chat` | **已实现**（见 §2） |

侧边栏改版（Web PR #26）仅改变消息展示位置与交互，继续调用既有接口即可。

---

## 2. 数据流对照（chat_design.md §11）

| 步骤 | 前端 | 后端 |
|------|------|------|
| 进房历史 | `GET /api/rooms/:roomId/chat?limit=80` | `listRoomChat` → `ListChat` → Redis `XREVRANGE` |
| 发送 | `POST /api/rooms/:roomId/chat` `{ text }` | `postRoomChat` → `SendChat` → `XADD` + Ably REST `room.chat` |
| 实时 | `useRoomRealtime` 订阅 `room.chat` | `MessageRoomChat` / `ably.MessageNameChat` |
| 过长 | HTTP **413**，提示「消息过长」 | `ErrChatTextTooLong` / `ErrChatPayloadTooLarge` → `PayloadTooLarge` |
| 不可用 | HTTP **503** | `ErrChatRequiresRedis` / `ErrChatPendingFull` → `ServiceUnavailable` |
| 过快 | HTTP **429** | `ErrChatRateLimited` → `TooManyRequests` |
| 延迟推送 | 响应 `realtime: "deferred"` | Ably 重试失败后入 pending ZSET |

私有房：与 snapshot 一致，JSON/query `password` 鉴权。

---

## 3. 实现索引（WatchTvTogether）

| 模块 | 路径 |
|------|------|
| HTTP 路由 | `internal/api/room_handlers.go` |
| 业务 | `internal/room/chat.go` |
| Redis | `internal/cache/redis/room_chat.go` |
| 房间清理 | `internal/room/hub.go` → `deleteRoomChat` |
| Ably 发布 | `internal/realtime/ably/service.go` |
| 配置 | `internal/config/config.go`（`CHAT_*`） |
| 说明 | `README.md` 房间聊天小节 |

完整 Redis key、重试与 pending 语义见 **`docs/room_chat_realtime_design_zh.md`**。

---

## 4. 验收（后端侧）

- [x] `POST`/`GET` `/api/rooms/:roomId/chat` 与 Web `api.ts` 一致  
- [x] 关闭/空房清理删除 `room:chat:*` key  
- [x] `cache_backend` 非 Redis 注入时聊天 API 返回 503  
- [x] 单元测试：`internal/room/chat_test.go`、`internal/cache/redis/room_chat_test.go`

---

## 5. 变更记录

| 日期 | 说明 |
|------|------|
| 2026-05-19 | 初稿：确认 UI 设计无后端契约变更；补充测试与本文档 |
