# Findings

- 房间 JWT 无 `publish`，聊天须 HTTP + 服务端 Ably REST。
- `NewService` 仅在 `internal/api/router.go` 与 `internal/room/hub_test.go` 调用。
- `apierr` 需新增 413/503 以匹配设计契约。
- `router_test` 使用 memory cache，聊天接口预期 503；可不阻塞 CI。
