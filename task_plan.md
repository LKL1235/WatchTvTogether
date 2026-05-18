# Task Plan: GitHub #38 — 移除 memory 缓存后端（当前）

## Goal

移除可配置的 `cache_backend=memory` 运行时路径及相关默认与文档，仅保留 Redis；进程内 `internal/cache/memory` 保留供单元测试注入。

## Current Phase

Phase 5 — Delivery

## Phases

### Phase 1: Requirements & Discovery

- [x] 阅读 Issue #38 与代码中 `memory` / `CacheBackend` 引用
- **Status:** complete

### Phase 2: Planning & Structure

- [x] 方案：`newCaches` 仅 Redis；`config` 默认与校验仅 `redis`；样例 `config.yaml`；README / AGENTS / 设计文档同步
- **Status:** complete

### Phase 3: Implementation

- [x] `cmd/server/main.go`、`internal/config`、`config.yaml`、注释与文档
- [x] 新增 `TestLoad_RejectsMemoryCacheBackend`
- **Status:** complete

### Phase 4: Testing & Verification

- [x] `go test ./...`、`go vet ./...`、`go build ./...`
- **Status:** complete

### Phase 5: Delivery

- [x] commit、push、PR
- **Status:** complete

## Decisions Made (#38)

| Decision | Rationale |
|----------|-----------|
| 保留 `internal/cache/memory` 包 | 单测直接构造依赖，非生产「后端」选型 |
| `CACHE_BACKEND=memory` 在 `normalize` 阶段报错 | 与移除支持一致，配置即失败 |

## Errors Encountered (#38)

| Error | Attempt | Resolution |
|-------|---------|------------|
|       |         |            |

---

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

- [x] README 可见变更、git commit/push、PR（#37 / Web #24）
- **Status:** complete

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
