# 房间清理日志降为 info

> 创建时间：2026-05-26

## 任务列表

- [x] 新增 applog.Infof，将 info 日志写入 stdout（`internal/applog/info.go`）
- [x] hub.go 中 room cleanup 诊断日志改用 applog.Infof
- [x] go test ./internal/room/... 验证
- [ ] 提交并推送 PR
