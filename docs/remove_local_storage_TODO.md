# 移除本地媒资存储（SaaS 无服务器）

> 创建时间：2026-05-25

## 任务列表

- [x] 后端：删除 StaticFS、`/api/videos/:id/file`、StorageDir/PosterDir 配置
- [x] 部署：Docker / compose / .env.example 去掉 /data 卷
- [x] 文档：README、AGENTS
- [x] 前端：播放 URL 解析、Vite 代理、Admin 封面 URL
- [x] 验证：`go test ./...`、`npm run test`（18 passed）
- [x] Canvas 架构图同步
