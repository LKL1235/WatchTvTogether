# Notes

## 服务端下载能力已下线

原先依赖本机 `ffmpeg` / `yt-dlp` / `aria2` 的管理端下载任务与相关数据库表已移除；部署在无持久磁盘的平台（如 Vercel）时不应依赖「下载到服务器再播放」。视频来源应以外链、HLS、对象存储签名 URL 等与产品约定为准。

## Redis SaaS 免费版注意事项

当前使用的 Redis SaaS 免费版能力有限，后续实现房间 presence、空房清理、私有房授权等功能时，需要遵守以下约束。

### 不可依赖的 Redis 能力

- 不支持 Redis Stack 的 Triggers and Functions。
- 不开启/不依赖 Keyspace Notifications。
- 不使用 RedisGears、Triggers、Functions 或类似 Redis 事件钩子。
- 不把 Redis key 过期事件作为业务触发来源。
- 不设计“监听 key 过期/成员 key 删除事件后自动触发清理”的方案。

### 可以使用的 Redis 能力

- 普通 Redis 数据结构：string、hash、set、sorted set 等。
- TTL 作为异常兜底过期机制。
- `SETNX`/带 TTL 的锁实现分布式互斥。
- pipeline / transaction。
- 普通 Lua `EVAL` 脚本，用于原子更新 presence 或用户房间迁移。
- 后端应用层定时任务、登录后异步扫描、显式 API handler 事件发布。

### 设计原则

- Redis TTL 只能作为兜底，不是业务事件来源；TTL 到期后没有通知，只有后端扫描时才能观察到 key 不存在。
- 私有房访问授权有效期为“直到房间关闭/删除”，不能依赖授权 key 过期事件撤销；房间关闭、空房清理、未来密码变更时必须由应用层显式删除授权。
- 用户加入、离开、自动切换房间时，`user_joined` / `user_left` 事件应由后端 join/leave handler 显式发布，不通过 Redis trigger。
- 空房清理必须由应用层主动触发和扫描，例如登录/注册/可选 refresh 后异步执行，或后端定时任务执行。
- 空房清理不能只依赖 `room:pending_empty`，还需要能发现“没有在线用户但没有进入 pending empty”的房间。
- 如果继续使用服务端 Redis presence，需要应用层心跳/`last_seen` 机制；清理任务扫描超时用户并主动移除，不能等待 Redis 过期事件。
- 如果采用 Ably Presence 作为在线事实来源，清理任务应主动查询 Ably 当前 presence，不通过 Redis 事件钩子等待通知。
- 清理失败不影响登录响应，但必须记录日志/指标，便于排查线上“没有触发清理”的问题。

### 空房清理相关已知风险

- 当前清理逻辑可能只扫描 `room:pending_empty`，遗漏未进入 pending empty 的空房。
- 用户关闭页面、断网、刷新或浏览器崩溃时，如果没有显式 leave/heartbeat 过期扫描，Redis presence 可能仍保留旧成员。
- 客户端展示的 Ably Presence 与服务端 Redis presence 可能不一致，导致前端看起来没人在线，但后端仍认为房间有人。
- refresh token 恢复登录态如果不触发 cleanup hook，可能导致“用户打开站点但没有执行空房清理”。
