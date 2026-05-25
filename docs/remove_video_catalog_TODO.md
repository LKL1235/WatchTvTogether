# 移除影片库 + control 上报 duration

> 创建时间：2026-05-25

## 任务列表

- [x] 删除 VideoStore、`/api/videos*`、`videos` 表（migration 000002）
- [x] `pkg/queueurl` 校验队列 URL
- [x] control 请求/Redis 写入 `video_duration`
- [x] 前端 URL-only 队列 + 房主 metadata 上报
- [x] Admin 去掉视频库区块
- [x] 测试与文档
