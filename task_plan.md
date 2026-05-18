# Task Plan: 房间 Redis Stream 聊天（设计文档落地）

## Goal

按 `docs/room_chat_realtime_design_zh.md` 实现服务端（Redis Stream + Ably `room.chat` + HTTP API）与 Web 客户端（订阅、历史、发送与错误提示）。

## Current Phase

Phase 5 — Delivery

## Phases

### Phase 1: Requirements & Discovery

- [x] 阅读设计文档与现有 room/api/redis 结构
- **Status:** complete

### Phase 2: Planning & Structure

- [x] 确定模块：`cache.RoomChat`、`room.SendChat`/`ListChat`、`api` 路由、`config`、`apierr`
- **Status:** complete

### Phase 3: Implementation

- [x] 后端：config、apierr、RoomChat redis、room 逻辑、CloseRoom 清理、全局 pending 扫描
- [x] 前端：types、api、useRoomRealtime、RoomView UI
- **Status:** complete

### Phase 4: Testing & Verification

- [x] `go vet ./...`、`go build ./...`
- [x] Web：`vue-tsc`、`npm test`
- **Status:** complete

### Phase 5: Delivery

- [ ] README 可见变更、git commit/push、PR
- **Status:** in_progress

## Decisions Made

| Decision | Rationale |
|----------|-----------|
| `cache_backend!=redis` 时聊天 API 返回 503 | 与设计「仅 Redis」一致 |
| `room.User` 增加 `Nickname` | 与 Ably presence 载荷对齐，从 `UserStore.GetByID` 取 |
| pending 全局扫描用 `SCAN room:chat:ably_pending:*` | 无 KEYS，适合生产 |

## Errors Encountered

| Error | Attempt | Resolution |
|-------|---------|------------|
| session-catchup.py 路径不存在 | 1 | 跳过脚本，直接实现 |
