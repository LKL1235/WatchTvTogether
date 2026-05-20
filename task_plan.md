# Task Plan: chat_design.md 对应后端（审计与补强）

> 依据 WatchTvTogether-Web `docs/chat_design.md` v1.0 与后端 `docs/room_chat_realtime_design_zh.md`

## Goal

确认前端 Twitch 风格聊天侧栏**无需**变更 HTTP/Ably 契约；后端聊天能力已满足 §11 数据流；补齐设计文档建议的单元测试与对齐说明。

## Current Phase

Phase 5 — Delivery

## Phases

### Phase 1: Requirements & Discovery
- [x] 阅读 Web `chat_design.md` §2/#14、§11
- [x] 审计 `internal/room/chat.go`、`redis/room_chat.go`、API 路由
- **Status:** complete

### Phase 2: Planning & Structure
- [x] 结论：无新 API；补强文档 + 测试
- **Status:** complete

### Phase 3: Implementation
- [x] `docs/chat_design_backend_alignment_zh.md`
- [x] `internal/room/chat_test.go`
- [x] `internal/cache/redis/room_chat_test.go`（miniredis）
- **Status:** complete

### Phase 4: Testing & Verification
- [x] `go test ./...`、`go vet ./...`
- **Status:** complete

### Phase 5: Delivery
- [ ] commit、push、PR
- **Status:** in_progress

## Decisions Made

| Decision | Rationale |
|----------|-----------|
| 不修改 POST/GET chat 契约 | `chat_design.md` 决策 #14 |
| 用 miniredis 测 Redis Stream 封装 | 对齐 `room_chat_realtime_design_zh.md` §10 |
| fake `RoomChat` 测 `SendChat`/`ListChat` 业务 | 不依赖 Ably/Postgres |

## Errors Encountered

| Error | Attempt | Resolution |
|-------|---------|------------|
| session-catchup.py 缺失 | 1 | 跳过 |
