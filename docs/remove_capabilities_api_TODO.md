# 移除 GET /api/capabilities 与 capabilities 包

> 创建时间：2026-05-26

## 任务列表

- [x] 删除 `internal/capabilities` 与 `capabilities_handlers.go`
- [x] 从 `router`、`main`、测试中移除依赖
- [x] 更新 README 对外 API 说明
- [x] 验证 `go test ./...`、`go vet ./...`
