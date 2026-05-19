# Findings

## Issue #38（移除 memory 缓存后端）

- Issue: https://github.com/LKL1235/WatchTvTogether/issues/38
- 生产路径：`cmd/server/main.go` 曾以 `cache_backend` 选择 memory/redis；现已仅 Redis。
- 默认配置：`internal/config.Default()` 与 `config.yaml` 使用 `redis`。
- 单测仍通过 `internal/cache/memory` 与 `testDeps` 注入内存实现，与「移除后端选型」不冲突。

## 历史：房间聊天设计

- 房间 JWT 无 `publish`，聊天须 HTTP + 服务端 Ably REST。
- `NewService` 仅在 `internal/api/router.go` 与 `internal/room/hub_test.go` 调用。
- `apierr` 需新增 413/503 以匹配设计契约。
- `router_test` 使用 memory cache 实现类，聊天 `RoomChat` 为 nil 时期望 503；可不阻塞 CI。
