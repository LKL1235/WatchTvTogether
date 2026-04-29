# Ably 实时同步改造 TODO

目标：将当前 Go 后端中的房间 WebSocket 实时同步能力迁移到 Ably，适配 Vercel serverless 部署环境，并为参考前端仓库 `LKL1235/WatchTvTogether-Web` 制定对应的 Vue 前端改造计划。

参考资料：

- Ably Go SDK 入门：https://ably.com/docs/getting-started/go
- Ably Token Auth：https://ably.com/docs/auth/token
- 前端参考仓库：https://github.com/LKL1235/WatchTvTogether-Web

## 0. 当前状态与约束梳理

- [ ] 盘点现有实时同步入口：
  - 后端房间 WebSocket 路由：`GET /ws/room/:roomId`
  - 路由注册位置：`internal/api/room_handlers.go`
  - WebSocket/Hub 逻辑：`internal/room/hub.go`
  - 房间状态读取接口：`GET /api/rooms/:roomId/state`
  - 房间控制消息类型：
    - 客户端发送：`play_control`
    - 服务端广播：`room_snapshot`、`sync`、`room_event`、`error`
- [ ] 盘点当前后端依赖：
  - Gin HTTP API
  - `gorilla/websocket`
  - `cache.PubSub` 用于跨实例广播
  - `cache.RoomStateCache` 用于持久化房间播放状态
- [ ] 明确 Vercel 部署约束：
  - Vercel serverless 不适合承载长期 WebSocket 连接。
  - 后端应只保留短生命周期 HTTP 接口，例如签发 Ably token、写入房间状态、读取快照。
  - 客户端长期 realtime 连接交给 Ably Realtime SDK。
- [ ] 明确迁移原则：
  - 不在浏览器暴露 Ably root key。
  - 后端通过环境变量 `ABLY_ROOT_KEY` 读取 Ably root key。
  - 客户端只通过后端接口获取短期 Ably token。
  - 普通成员不允许发布播放控制消息；所有播放控制写入必须走后端 HTTP 接口，由后端校验房主/管理员权限后使用 root key 发布 Ably 消息。
  - 私有房间不维护持久成员关系；每次进入房间都要验证密码，房主和管理员不需要输入密码。
  - 不保留本地/Docker 环境旧 WebSocket 实现，迁移完成后直接删除 `/ws/room/:roomId` 及相关 Hub/client 代码。
  - 删除 SQLite 支持，后续后端只保留 Postgres 存储实现。
  - 房间状态最终一致性仍以后端 `RoomStateCache` / 存储为准，Ably 负责实时分发。
  - Ably channel 短期授予 `history` 便于重连补消息；长期仍以 `/snapshot` 作为权威初始化来源。
  - presence data 增加 `connectionId` 或设备名，前端 UI 需要能按用户聚合多设备或展示多设备状态。
  - 尽量保持现有消息结构兼容，降低前端 RoomView 迁移成本。

## 1. 后端 Ably SDK 接入计划

### 1.1 依赖与配置

- [ ] 添加 Go SDK 依赖：
  - 包名：`github.com/ably/ably-go/ably`
  - 命令：`go get github.com/ably/ably-go/ably@latest`
- [ ] 扩展配置结构 `internal/config/config.go`：
  - [ ] 新增字段：
    - `AblyRootKey string`
    - `AblyTokenTTL time.Duration`
    - `AblyTokenTTLRaw string`
    - `AblyChannelPrefix string`
  - [ ] 环境变量读取：
    - `ABLY_ROOT_KEY`
    - `ABLY_TOKEN_TTL`，默认 `30m`
    - `ABLY_CHANNEL_PREFIX`，默认 `watchtogether`
  - [ ] 配置校验：
    - 在启用 Ably 实时能力时，`ABLY_ROOT_KEY` 必须非空。
    - `ABLY_TOKEN_TTL` 必须大于 0，且不超过 Ably access token 最大 TTL；初始值使用 `30m`。
- [ ] 更新 `.env.example`：
  - [ ] 增加：
    - `ABLY_ROOT_KEY=appId.keyId:keySecret`
    - `ABLY_TOKEN_TTL=30m`
    - `ABLY_CHANNEL_PREFIX=watchtogether`
  - [ ] 注释说明：`ABLY_ROOT_KEY` 只能配置在服务端/Vercel Environment Variables，不可进入前端构建变量。

### 1.2 Ably 客户端封装

- [ ] 新增内部包，建议路径：`internal/realtime/ably`
- [ ] 定义服务结构：
  - [ ] `type Service struct { rest *ably.REST; cfg config.Config }`
  - [ ] `NewService(cfg config.Config) (*Service, error)`
  - [ ] 使用 `ably.NewREST(ably.WithKey(cfg.AblyRootKey))` 创建 REST 客户端。
- [ ] 定义统一 channel 命名：
  - [ ] 房间控制频道：`{prefix}:room:{roomId}:control`
  - [ ] 房间 presence 使用同一个控制频道的 presence，不再拆分额外 presence channel。
  - [ ] 管理/系统频道预留：`{prefix}:admin`
- [ ] 定义 Ably message name：
  - [ ] `room.snapshot`
  - [ ] `room.sync`
  - [ ] `room.event`
  - [ ] `room.error`
  - [ ] `room.control`
- [ ] 定义消息 payload，尽量复用 `internal/room.Message`：
  - [ ] `type RoomRealtimeMessage struct`
    - `Type string`
    - `Action model.PlaybackAction`
    - `Event string`
    - `Position float64`
    - `VideoID string`
    - `Queue []string`
    - `Timestamp int64`
    - `Payload any`
    - `User *room.User`
  - [ ] 保持 JSON 字段名与当前 WebSocket 消息一致，方便前端平滑迁移。

### 1.3 替换现有 WebSocket Hub 职责

- [ ] 拆分 `internal/room/hub.go` 现有职责：
  - [ ] 保留可复用的领域类型：`User`、`Message`、`Snapshot`
  - [ ] 提取房间状态写入逻辑：
    - 输入：roomId、当前用户、play_control payload
    - 输出：最新 `sync` message
    - 副作用：写入 `RoomStateCache`
  - [ ] 移除或废弃长期连接管理：
    - `gorilla/websocket.Upgrader`
    - `client.readPump`
    - `client.writePump`
    - `Hub.clients`
    - ping/pong 逻辑
  - [ ] 直接删除旧 `/ws/room/:roomId` 路由，不保留本地/Docker 兼容实现。
- [ ] 重新定义后端实时服务边界：
  - [ ] 后端不再维护在线客户端列表。
  - [ ] 在线成员列表由 Ably presence 管理。
  - [ ] 播放控制写操作由 HTTP API 鉴权后落库并发布到 Ably。
  - [ ] 播放控制实时订阅由客户端直接订阅 Ably channel。
  - [ ] 普通成员只能订阅实时消息和进入 presence，不能通过 Ably publish 控制消息。
- [ ] 调整 `room.Manager` 或替换为新的 `room.Service`：
  - [ ] `Snapshot(ctx, roomId)` 从 `RoomStateCache` 读取状态。
  - [ ] `Destroy(ctx, roomId)` 删除房间状态，并发布 `room.event` / `room.deleted` 到 Ably。
  - [ ] `ApplyControl(ctx, roomId, user, request)` 验证权限、写状态、发布 `sync`。
  - [ ] `PublishRoomEvent(ctx, roomId, event, user)` 发布用户加入/离开/踢出等事件。

### 1.4 后端接口设计

#### 1.4.1 签发 Ably 临时 token

- [ ] 新增接口：`POST /api/ably/token`
- [ ] 鉴权：必须携带当前后端 access token，即复用 `requireAuth(authService)`。
- [ ] 请求体：

```json
{
  "room_id": "room_uuid",
  "purpose": "room",
  "password": "private-room-password-when-required"
}
```

- [ ] 响应体直接返回 Ably `TokenDetails` 兼容结构：

```json
{
  "token": "ably_token",
  "expires": 1710000000000,
  "issued": 1709998200000,
  "capability": "{\"watchtogether:room:ROOM_ID:control\":[\"subscribe\",\"presence\",\"history\"]}",
  "clientId": "USER_ID"
}
```

- [ ] token 参数设计：
  - [ ] `ClientID`：当前登录用户 ID。
  - [ ] `TTL`：来自 `ABLY_TOKEN_TTL`，默认 `30m`。
  - [ ] `Capability`：
    - 所有客户端 token 仅授予当前房间 channel 的 `subscribe`、`presence`、`history`。
    - 普通成员、房主、管理员的 Ably token 都不授予 `publish`。
    - 后端使用 `ABLY_ROOT_KEY` 发布 `room.sync`、`room.event`、`room.deleted` 等消息。
    - 短期保留 `history` 能力便于重连补消息；初始化和最终校准仍以 `/api/rooms/:roomId/snapshot` 为准。
- [ ] 安全校验：
  - [ ] 校验 `room_id` 存在。
  - [ ] 公共房间：登录用户可签发当前房间 token。
  - [ ] 私有房间：房主和管理员可免密码签发 token；普通成员每次进入房间都必须提交正确密码。
  - [ ] 不新增持久成员关系，不新增 `room_members` 表，不用 join cache 记录「已进入」状态。
  - [ ] token 自动续签时，前端通过内存中的本次入房密码再次调用 token endpoint；密码不写入 localStorage/sessionStorage。
  - [ ] 不允许客户端传入任意 capability。
  - [ ] 不在日志中打印 `ABLY_ROOT_KEY` 或签发 token。
- [ ] 失败状态码：
  - [ ] `401`：未登录或 access token 无效。
  - [ ] `403`：无权访问房间。
  - [ ] `404`：房间不存在。
  - [ ] `500`：Ably SDK 签发失败或服务端未配置 root key。

#### 1.4.2 播放控制 HTTP 接口

- [ ] 新增接口：`POST /api/rooms/:roomId/control`
- [ ] 鉴权：必须登录。
- [ ] 权限：仅房主或管理员可控制，沿用当前 `canManageRoom`。
- [ ] 请求体：

```json
{
  "action": "play",
  "position": 12.34,
  "video_id": "video_uuid_or_url",
  "queue": ["video_id_1", "video_id_2"]
}
```

- [ ] 服务端处理流程：
  - [ ] 读取 room。
  - [ ] 校验当前用户是否房主或管理员。
  - [ ] 标准化 queue：如果 queue 为空且 video_id 非空，则 queue 为 `[video_id]`。
  - [ ] 写入 `RoomStateCache.SetRoomState`。
  - [ ] 使用 Ably REST publish 到房间 channel：
    - `name`: `room.sync`
    - `data`: 与当前 WebSocket `sync` 消息兼容。
  - [ ] 返回最新状态，便于控制端本地确认。
- [ ] 响应体：

```json
{
  "type": "sync",
  "action": "play",
  "position": 12.34,
  "video_id": "video_uuid_or_url",
  "queue": ["video_id_1", "video_id_2"],
  "timestamp": 1710000000,
  "user": {
    "id": "user_id",
    "username": "alice",
    "role": "admin",
    "is_owner": true
  }
}
```

#### 1.4.3 房间进入与快照接口

- [ ] 保留现有接口：`GET /api/rooms/:roomId/state`，用于轻量读取播放状态。
- [ ] 调整现有接口：`POST /api/rooms/:roomId/join`
  - [ ] 公共房间：登录用户可直接加入。
  - [ ] 私有房间：房主和管理员免密码；普通成员每次进入房间都必须提交正确密码。
  - [ ] 不写入持久成员关系，只返回房间信息和是否房主/管理员。
  - [ ] 前端进入私有房间时，在当前页面内存保存本次密码，供 `/api/ably/token` 自动续签使用；离开房间或刷新页面后清空。
- [ ] 新增接口：`POST /api/rooms/:roomId/snapshot`，用于前端进入房间时一次性获取完整初始化数据。
  - [ ] 请求体：`{ "password": "private-room-password-when-required" }`
  - [ ] 公共房间、房主、管理员可不传 password。
- [ ] 私有房间快照权限：
  - [ ] 房主和管理员可免密码读取。
  - [ ] 普通成员必须在请求中提交本次房间密码，或先调用 `POST /api/rooms/:roomId/join` 并在同一次前端流程内携带密码调用 snapshot。
  - [ ] 由于不维护持久成员关系，刷新页面后需要重新输入密码。
- [ ] 响应体：

```json
{
  "room_id": "room_uuid",
  "state": {
    "room_id": "room_uuid",
    "video_id": "video_uuid",
    "queue": ["video_uuid"],
    "action": "pause",
    "position": 0,
    "updated_by": "user_id",
    "updated_at": "2026-04-29T09:15:00Z"
  },
  "queue": ["video_uuid"],
  "viewer_count": 0,
  "ably": {
    "channel": "watchtogether:room:room_uuid:control",
    "token_endpoint": "/api/ably/token"
  }
}
```

- [ ] 在线用户列表处理：
  - [ ] 不再由后端 Hub 内存维护。
  - [ ] 前端进入 Ably presence 后通过 `channel.presence.get()` 获取当前在线成员。
  - [ ] 后端快照不包含实时在线 users，避免在 Vercel serverless 中额外耦合 Ably presence 查询。

#### 1.4.4 房间成员事件与踢人

- [ ] 保留接口：`POST /api/rooms/:roomId/kick/:uid`
- [ ] 增强行为：
  - [ ] 校验房主/管理员权限。
  - [ ] 发布 Ably message：
    - `name`: `room.event`
    - `data`: `{ "type": "room_event", "event": "user_kicked", "user": { ... } }`
  - [ ] 前端收到 `user_kicked` 且 user.id 为自己时自动离开房间并提示。
- [ ] 私有房间成员关系：
  - [ ] 不维护持久成员表。
  - [ ] 踢人只影响当前在线 presence/页面状态，不写入永久黑名单。
  - [ ] 被踢用户重新进入私有房间时仍需重新输入正确密码。

### 1.5 Vercel 部署事项

- [ ] 在 Vercel 项目环境变量中配置：
  - [ ] `ABLY_ROOT_KEY`
  - [ ] `ABLY_TOKEN_TTL`
  - [ ] `ABLY_CHANNEL_PREFIX`
  - [ ] 现有后端需要的 `POSTGRES_URL`、`REDIS_URL`、`JWT_SECRET` 等。
- [ ] 后端只暴露 HTTP API：
  - [ ] `/api/ably/token`
  - [ ] `/api/rooms/:roomId/control`
  - [ ] `/api/rooms/:roomId/snapshot`
  - [ ] 现有 auth/room/video/download API
- [ ] 删除 `/ws/room/:roomId`：
  - [ ] README 明确：实时同步统一使用 Ably，不再提供后端 WebSocket 路由。
  - [ ] 本地、Docker、Vercel 环境均不保留旧 WebSocket 兼容实现。
- [ ] Ably root key 权限：
  - [ ] root key 至少需要 `publish`、`subscribe`、`presence`、`history`、`channel-metadata` 中与 token 签发/发布相关能力。
  - [ ] 客户端 token 能力固定最小化为当前房间 channel 的 `subscribe`、`presence`、`history`。

### 1.6 后端测试计划

- [ ] 单元测试：
  - [ ] `config.Load` 能读取 `ABLY_ROOT_KEY`、`ABLY_TOKEN_TTL`。
  - [ ] capability 生成只允许目标 room channel。
  - [ ] 普通用户/房主/管理员 capability 差异正确。
  - [ ] `POST /api/rooms/:roomId/control` 权限校验正确。
- [ ] 集成测试：
  - [ ] mock Ably publisher，验证控制接口写入 `RoomStateCache` 后发布 `room.sync`。
  - [ ] `POST /api/ably/token` 在缺少 root key 时返回明确错误。
  - [ ] 私有房间鉴权路径覆盖。
- [ ] 手动验证：
  - [ ] 使用 Ably dashboard 或 Ably CLI 订阅房间 channel。
  - [ ] 调用控制接口后能看到 `room.sync`。
  - [ ] 客户端 token 只能订阅自己的房间 channel，不能订阅其他房间。

## 2. 前端改造计划（基于 LKL1235/WatchTvTogether-Web）

当前参考前端为 Vue + Vite + Pinia，关键文件：

- `src/api.ts`：HTTP API 封装
- `src/types.ts`：类型定义
- `src/composables/useRoomSocket.ts`：当前原生 WebSocket composable
- `src/views/RoomView.vue`：房间播放、队列、成员和实时事件 UI
- `src/stores/auth.ts`：保存后端 access token

### 2.1 依赖与环境变量

- [ ] 添加 Ably 前端 SDK：
  - [ ] `npm install ably@latest`
- [ ] 新增前端环境变量：
  - [ ] `VITE_API_BASE=https://your-backend.vercel.app`
  - [ ] 不添加 `VITE_ABLY_ROOT_KEY`，禁止将 root key 注入前端。
- [ ] 更新前端 README：
  - [ ] 说明实时连接由 Ably 处理。
  - [ ] 说明前端通过后端 `/api/ably/token` 自动续签。

### 2.2 API 封装改造

- [ ] 在 `src/api.ts` 新增：

```ts
export function fetchAblyToken(
  token: string,
  input: { room_id: string; purpose: 'room'; password?: string },
) {
  return apiFetch<AblyTokenDetails>('/api/ably/token', {
    method: 'POST',
    body: JSON.stringify(input),
  }, token)
}

export function sendRoomControl(
  token: string,
  roomId: string,
  input: { action: PlaybackAction; position: number; video_id: string; queue?: string[] },
) {
  return apiFetch<RoomSocketMessage>(`/api/rooms/${roomId}/control`, {
    method: 'POST',
    body: JSON.stringify(input),
  }, token)
}

export function fetchRoomSnapshot(token: string, roomId: string, password?: string) {
  return apiFetch<RoomSnapshotPayload>(`/api/rooms/${roomId}/snapshot`, {
    method: 'POST',
    body: JSON.stringify({ password }),
  }, token)
}
```

- [ ] 在 `src/types.ts` 新增/调整：
  - [ ] `PlaybackAction`
  - [ ] `RoomRealtimeMessage`
  - [ ] `AblyTokenDetails`
  - [ ] `RoomPresenceData`
  - [ ] `RoomSnapshotPayload` 保持兼容当前字段。
- [ ] 将 `RoomSocketMessage` 从 `useRoomSocket.ts` 移到 `types.ts`，避免 composable 与页面互相耦合。

### 2.3 替换 useRoomSocket

- [ ] 将 `src/composables/useRoomSocket.ts` 重命名或替换为 `src/composables/useRoomRealtime.ts`。
- [ ] 新 composable 输入：

```ts
export function useRoomRealtime(options: {
  roomId: () => string
  accessToken: () => string
  currentUser: () => User | null
  canControl: () => boolean
  roomPassword?: () => string
})
```

- [ ] composable 状态：
  - [ ] `connected`
  - [ ] `connecting`
  - [ ] `connectionState`
  - [ ] `lastMessage`
  - [ ] `events`
  - [ ] `members`
  - [ ] `error`
- [ ] Ably client 初始化：
  - [ ] 使用 `new Ably.Realtime({ authCallback })` 或 `authUrl`。
  - [ ] `authCallback` 调用后端 `fetchAblyToken(accessToken, { room_id, purpose: 'room', password })`；公共房间、房主和管理员不传 password。
  - [ ] `clientId` 由后端 token 决定，不在前端伪造。
  - [ ] 自动续签交给 Ably SDK。
- [ ] 频道连接：
  - [ ] channel 名称来自后端 snapshot 返回的 `ably.channel`，或前端按同一规则生成。
  - [ ] 订阅 `room.sync`、`room.event`、`room.snapshot`。
  - [ ] 使用 `channel.presence.enter({ username, role, is_owner })` 进入 presence。
  - [ ] 使用 `channel.presence.subscribe()` 维护成员列表。
  - [ ] 首次进入后调用 `channel.presence.get()` 补齐在线成员。
- [ ] 控制发送：
  - [ ] 禁止直接 `channel.publish('room.control')`，前端 token 没有 `publish` 能力。
  - [ ] `sendControl` 必须调用后端 `POST /api/rooms/:roomId/control`。
  - [ ] 后端校验权限、写状态、发布 Ably `room.sync`。
  - [ ] 控制端收到接口响应后可本地 optimistic sync；最终以 Ably `room.sync` 为准。
- [ ] 连接清理：
  - [ ] 组件卸载时 `presence.leave()`。
  - [ ] 取消订阅。
  - [ ] `client.close()`。

### 2.4 RoomView.vue 页面逻辑改造

- [ ] 替换导入：
  - [ ] `useRoomSocket` -> `useRoomRealtime`
  - [ ] `fetchRoomState` -> 优先 `fetchRoomSnapshot`
  - [ ] 新增 `sendRoomControl`
- [ ] 页面初始化流程：
  - [ ] 进入页面时并行加载：
    - `fetchRoomSnapshot`
    - `fetchVideos`
  - [ ] 用 snapshot 初始化：
    - `state`
    - `position`
    - `currentVideo`
    - `queue`
    - Ably channel name
  - [ ] 初始化后连接 `useRoomRealtime.connect()`。
- [ ] 实时消息处理：
  - [ ] `room.sync`：
    - 更新 `state`
    - 更新 `position`
    - 更新 `currentVideo`
    - 更新 `queue`
    - 调用 `syncPlayer`
  - [ ] `room.event`：
    - `user_joined` / `user_left` 逐步改为 presence 驱动，事件仅用于提示。
    - `user_kicked` 命中当前用户时离开房间。
    - `room_deleted` 命中当前房间时返回大厅。
  - [ ] `room.snapshot`：
    - 用于服务端强制重同步或调试，不作为常规进入房间的唯一来源。
- [ ] 在线成员 UI：
  - [ ] `members` 改由 `useRoomRealtime.members` 提供。
  - [ ] 显示 Ably connection state：
    - `initialized`
    - `connecting`
    - `connected`
    - `disconnected`
    - `suspended`
    - `failed`
  - [ ] 断线时提示「正在重连实时同步」。
- [ ] 播放控制按钮：
  - [ ] `send(action)` 内不再调用 `socket.sendControl`。
  - [ ] 调用 `sendRoomControl(auth.accessToken.value, roomId, payload)`。
  - [ ] 成功后可立即本地 `syncPlayer(action, position)`，并等待 Ably 广播确认。
  - [ ] 失败时显示权限或网络错误。
- [ ] 事件调试区域：
  - [ ] 当前页面已有「实时事件」区域，可继续展示最近 Ably messages。
  - [ ] 增加 connection state、channel name，方便 Vercel/Ably 联调。

### 2.5 Auth 与 token 续签

- [ ] `src/stores/auth.ts` 保持后端 JWT access token/refresh token 逻辑。
- [ ] Ably token 不进入 localStorage。
- [ ] Ably token 通过 `authCallback` 临时获取，由 SDK 内存管理和续签。
- [ ] 私有房间密码只保存在房间页运行期内存中：
  - [ ] 房主/管理员进入私有房间不需要密码。
  - [ ] 普通成员每次进入私有房间都需要输入密码。
  - [ ] 刷新页面、离开房间或关闭标签页后清空密码，下次进入重新输入。
  - [ ] Ably token 自动续签时复用内存中的本次密码调用 `/api/ably/token`。
- [ ] 当后端 access token 过期导致 Ably token endpoint 返回 `401`：
  - [ ] 短期：提示用户重新登录。
  - [ ] 后续：在 `apiFetch` 中加入 refresh token 自动刷新，再重试 token endpoint。

### 2.6 前端页面设计补充

- [ ] 房间页顶部状态条：
  - [ ] 展示「实时同步：已连接 / 重连中 / 失败」。
  - [ ] 展示在线人数，来自 presence 成员数量。
  - [ ] 失败时提供「重新连接」按钮。
- [ ] 成员侧栏：
  - [ ] 成员项展示：
    - 用户名
    - 房主/管理员标识
    - presence 状态
  - [ ] 房主/管理员可点击「踢出」。
  - [ ] 被踢用户弹窗提示并返回大厅。
- [ ] 队列区域：
  - [ ] 非控制用户仍可查看队列，但按钮置灰。
  - [ ] 控制用户调整队列后通过控制接口同步。
  - [ ] 同步失败时回滚本地队列或重新拉取 snapshot。
- [ ] 视频播放器：
  - [ ] 避免普通用户本地 `play/pause/seek` 误触发广播。
  - [ ] 控制用户点击播放/暂停/同步进度时才调用控制接口。
  - [ ] 收到远端 sync 时处理浏览器 autoplay 限制，失败时显示「点击继续播放」提示。
- [ ] 调试/开发模式：
  - [ ] 在开发环境显示最近 5 条 Ably 消息。
  - [ ] 生产环境可折叠或隐藏调试信息。

### 2.7 前端测试计划

- [ ] 单元测试：
  - [ ] `useRoomRealtime` authCallback 会调用 `/api/ably/token`。
  - [ ] presence enter/update/leave 能正确更新 members。
  - [ ] `room.sync` 消息能正确更新 RoomView 状态。
- [ ] 手动联调：
  - [ ] 两个浏览器账号进入同一房间。
  - [ ] 房主播放/暂停/seek，另一端同步。
  - [ ] 房主调整队列，另一端同步队列和当前视频。
  - [ ] 普通成员控制按钮置灰，直接调用控制接口返回 `403`。
  - [ ] 断网/刷新页面后 Ably 自动重连并拉取当前 snapshot。
  - [ ] Vercel 部署环境下确认不再访问 `/ws/room/:roomId`。

## 3. 迁移步骤建议

- [ ] 第一阶段：后端基础设施
  - [ ] 添加 Ably SDK 和配置。
  - [ ] 新增 token 签发服务和 `/api/ably/token`。
  - [ ] 新增 Ably publisher 抽象和 mock。
  - [ ] 补齐配置/权限/capability 测试。
- [ ] 第二阶段：后端控制链路
  - [ ] 新增 `POST /api/rooms/:roomId/control`。
  - [ ] 将原 `handleClientMessage` 的状态写入逻辑迁移为可复用函数。
  - [ ] 发布 Ably `room.sync`。
  - [ ] 增强 `kick` / `delete room` 的 Ably 事件发布。
- [ ] 第三阶段：前端 Ably 接入
  - [ ] 添加 `ably` npm 依赖。
  - [ ] 新增 `fetchAblyToken`、`sendRoomControl`、`fetchRoomSnapshot`。
  - [ ] 替换 `useRoomSocket` 为 `useRoomRealtime`。
  - [ ] RoomView 接入 presence 和 Ably messages。
- [ ] 第四阶段：Vercel 联调
  - [ ] 配置 Vercel 环境变量。
  - [ ] 部署后端，确认 token endpoint 可用。
  - [ ] 部署前端，确认浏览器只连接 Ably，不连接后端 WebSocket。
  - [ ] 使用 Ably dashboard/CLI 观察 channel message 和 presence。
- [ ] 第五阶段：清理与文档
  - [ ] 删除 `/ws/room/:roomId` 路由和全部后端 WebSocket Hub/client 实现，不保留 deprecated 兼容入口。
  - [ ] 移除 `gorilla/websocket` 依赖。
  - [ ] 删除 SQLite 支持：
    - [ ] 移除 `internal/store/sqlite`。
    - [ ] 移除 `migrations` 中仅服务 SQLite 的迁移流程，或改为 Postgres-only 迁移。
    - [ ] 移除 `config.StorageBackendSQLite`、`SQLitePath`、`SQLITE_PATH`。
    - [ ] 更新 `docker-compose.dev.yml` 和本地启动文档为 Postgres-only。
    - [ ] 更新测试套件，不再跑 SQLite store suite。
  - [ ] 更新 README 核心接口列表。
  - [ ] 更新部署文档和 `.env.example`。

## 4. 已确认决策与剩余问题检查

- [x] 普通成员不允许发布控制消息：
  - 所有播放控制通过后端 `POST /api/rooms/:roomId/control`。
  - 所有客户端 Ably token 固定只授予当前房间 channel 的 `subscribe`、`presence`、`history`。
  - 后端使用 `ABLY_ROOT_KEY` 发布 `room.sync` / `room.event`。
- [x] 私有房间不需要持久成员关系：
  - 不新增 `room_members` 表。
  - 不通过数据库或缓存记录长期加入状态。
  - 普通成员每次进入私有房间都必须输入密码。
  - 房主和管理员进入私有房间不需要输入密码。
- [x] 不保留旧 WebSocket 实现：
  - 本地、Docker、Vercel 环境都统一使用 Ably。
  - 删除 `/ws/room/:roomId`、`gorilla/websocket` 和 Hub/client 长连接代码。
- [x] 删除 SQLite 支持：
  - 后端存储实现收敛为 Postgres-only。
  - 配置、文档、Docker、本地测试同步移除 SQLite 路径。
- [x] Ably channel 短期授予 `history`：
  - 用于断线重连时补最近消息。
  - `/api/rooms/:roomId/snapshot` 仍是初始化和最终校准的权威来源。
- [x] 多端同账号 presence 设计：
  - presence data 增加 `connectionId` 或设备名。
  - UI 支持按用户聚合多设备，也可展开显示每个设备状态。
- [x] 剩余待确认问题检查：
  - 当前方案中暂无待确认问题；后续进入实现时只需要按上述决策拆分任务执行。
