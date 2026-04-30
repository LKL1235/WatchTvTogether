# WatchTvTogether 待办事项整理

本文档只整理需求与后续实现待办，不包含代码改动。范围包括当前 Go 后端仓库，以及对应 Vue 3 客户端仓库：<https://github.com/LKL1235/WatchTvTogether-Web>。

## 0. 背景与现状确认

- 后端核心房间接口位于 `internal/api/room_handlers.go`，房间状态与 presence 逻辑位于 `internal/room/hub.go`、`internal/cache/*/room_presence.go`。
- 当前同步链路为：客户端进入房间时调用 `POST /api/rooms/:roomId/snapshot`，拿到状态与 Ably channel，再通过 Ably 订阅 `room.sync`、`room.event`、`room.snapshot`。
- 当前客户端关键改动点预计为：
  - `src/api.ts`
  - `src/views/LobbyView.vue`
  - `src/views/RoomView.vue`
  - `src/views/AdminView.vue`
  - `src/composables/useRoomRealtime.ts`
  - `src/types.ts`
- 当前服务端已有的相关能力：
  - `GET /api/rooms/:roomId/state` 会返回投影后的播放进度与播放状态。
  - `DELETE /api/rooms/:roomId` 已允许房主或管理员删除/关闭房间，并发布 `room_closed` 事件。
  - 登录后通过 `authService.SetAfterLogin` 触发 `rooms.MaybeRunGlobalCleanup`。
  - Redis presence 当前会阻止用户同时在两个房间，冲突时 `join` 返回 409。

## 1. 新用户加入房间时同步播放状态

### 问题描述

新用户加入房间时，播放进度已经被同步，但播放状态没有正确同步。房主和房间内其他用户正在播放时，新加入用户的视频仍处于暂停状态。

### 服务端待办

- [ ] 明确 `POST /api/rooms/:roomId/snapshot` 的状态契约：`state.action` 必须代表房间当前权威播放状态，尤其是正在播放时应为 `play`，不能因为投影进度或默认值被降级为 `pause`。
- [ ] 检查 `ProjectedRoomState` 与 `ProjectedPlayback` 在 `play` 状态下的返回值，确保只投影 position，不改变非结束场景的 `Action`。
- [ ] 检查视频结束场景：仅当确实到达视频末尾并按播放模式需要暂停/切换时，才将 `Action` 置为 `pause` 或切换后的状态。
- [ ] 为 `snapshot` 添加/补充后端测试：
  - [ ] 房间状态为 `play`，一段时间后新用户获取 snapshot，返回 `state.action=play` 且 position 为投影后的值。
  - [ ] 房间状态为 `pause`，snapshot 返回 `pause` 且 position 不继续增长。
  - [ ] 视频播放至末尾时按顺序/循环模式处理，不误报正在播放。
- [ ] 如发现 `room.sync` 消息缺少客户端需要的字段，补齐 `control_version`、`playback_mode`、`updated_at` 等可选字段，避免客户端应用实时消息后状态不完整。

### 客户端待办

- [ ] 在 `RoomView.vue` 的 `applyRoomSnapshot` 中，确认先完成队列构建与视频 source 加载，再应用 `state.action` 和 `state.position`。
- [ ] 处理浏览器 autoplay 限制：
  - [ ] 当 snapshot 表示 `play` 但 `video.play()` 被浏览器拒绝时，展示明确提示，例如“房间正在播放，点击继续播放/解除静音后同步”。
  - [ ] 提供用户手动恢复播放按钮，点击后按 snapshot/最新 state 同步到当前进度并播放。
  - [ ] 避免把新用户本地 autoplay 失败误上报成房间暂停；普通成员不得发送控制消息。
- [ ] 在 `useRoomRealtime.ts` 连接成功后，可选择读取 Ably history 或主动刷新 state，确保连接建立期间错过的 `room.sync` 不导致新用户状态落后。
- [ ] 补充客户端测试：
  - [ ] `applyRoomSnapshot` 收到 `play` 时会调用播放器播放逻辑。
  - [ ] autoplay 失败时显示恢复播放提示，不改变房间权威状态。
  - [ ] 普通成员只应用远端状态，不发送 `control`。

### 验收标准

- [ ] 房主播放视频后，新用户加入同一房间，视频定位到当前进度并尝试播放。
- [ ] 如浏览器阻止自动播放，新用户能看到明确提示，并可通过一次点击恢复到当前进度继续播放。
- [ ] 新用户加入不会导致房主或其他用户的视频被暂停。

## 2. 私有房密码记忆：同一房间再次进入不应重复要求密码

### 问题描述

同一用户进入私有房间并输入过密码后，再次进入同一房间不应该再次要求密码。

### 接口设计建议

推荐采用服务端授权记录，而不是仅依赖客户端内存保存密码。

#### 方案 A：服务端记录房间访问授权（推荐）

- [ ] 新增房间访问授权缓存接口，例如：
  - `GrantRoomAccess(ctx, roomID, userID, ttl)`
  - `HasRoomAccess(ctx, roomID, userID) bool`
  - `RevokeRoomAccess(ctx, roomID, userID)`（可选）
  - `DeleteRoomAccess(ctx, roomID)`（房间关闭/删除时清理）
- [ ] Redis key 建议：
  - `room:access:{roomID}` 使用 set 存 userID，设置合理 TTL。
  - 或 `room:access:{roomID}:{userID}` 使用 string，便于单用户 TTL。
- [ ] 授权 TTL 待产品确认，初始可与 refresh token 或房间生命周期对齐；至少应覆盖同一登录会话内的再次进入。
- [ ] `POST /api/rooms/:roomId/join`：
  - [ ] 如果用户是房主/管理员，跳过密码。
  - [ ] 如果用户已有该房授权，跳过密码。
  - [ ] 如果密码校验成功，写入授权记录。
- [ ] `POST /api/rooms/:roomId/snapshot` 与 `POST /api/ably/token`：
  - [ ] 支持使用已有授权跳过密码。
  - [ ] 密码校验成功时也可写入授权，避免 join 与 snapshot/token 时序差异。
- [ ] `DELETE /api/rooms/:roomId` / 空房清理时同步删除该房授权缓存。
- [ ] 修改错误语义：
  - [ ] 未授权访问私有房仍返回 403。
  - [ ] 已授权用户不需要再次提交 password。
- [ ] 补充后端测试：
  - [ ] 首次输入正确密码 join 成功并写入授权。
  - [ ] 同一用户同一房间再次 join/snapshot/ably token 不带密码也成功。
  - [ ] 不同用户不能复用该授权。
  - [ ] 房间删除后授权失效。

#### 方案 B：客户端会话缓存密码（临时方案）

- [ ] 在客户端用 `sessionStorage` 按 `roomId + userId` 缓存已验证密码或授权标记。
- [ ] 再次进入时自动带上密码调用 `join` / `snapshot` / `ably/token`。
- [ ] 退出登录、房间关闭、密码错误时清理缓存。
- [ ] 风险：密码仍由客户端持有，不适合作为长期方案；刷新/多设备一致性也较差。

### 客户端待办

- [ ] 在 `src/api.ts` 中适配后端新的“已授权可不传 password”契约。
- [ ] 在 `LobbyView.vue` 中，私有房进入流程改为：
  - [ ] 先尝试不带密码 `joinRoom`。
  - [ ] 若返回 403 再弹出密码输入框。
  - [ ] 若成功则直接进入房间，不再要求密码。
- [ ] 在 `RoomView.vue` 中，snapshot 与 Ably token 获取逻辑兼容“无 password 但服务端已有授权”的场景。
- [ ] 在 `useRoomRealtime.ts` 的 `fetchAblyToken` 调用中，不再强依赖 `roomPassword`，优先依赖后端授权。
- [ ] 私有房密码弹窗文案区分：
  - [ ] 首次进入需要密码。
  - [ ] 授权过期或密码变更需要重新输入。
- [ ] 补充客户端测试：
  - [ ] 私有房首次进入需要输入密码。
  - [ ] 同一用户再次进入不弹密码。
  - [ ] 403 时仍能回退到密码弹窗。

### 验收标准

- [ ] 用户 A 输入密码进入私有房后，返回大厅再进入同一房间，不再弹密码。
- [ ] 用户 B 未输入过密码时仍需要密码。
- [ ] 房主/管理员仍无需密码。
- [ ] 授权失效、房间关闭或密码校验失败时，客户端能重新要求密码。

## 3. 加强管理功能：管理员手动关闭房间

### 问题描述

现有管理能力需要加强，至少支持管理员手动关闭房间，并通知房间内用户退出。

### 服务端待办

- [ ] 梳理当前 `DELETE /api/rooms/:roomId` 是否已满足“关闭房间”语义：
  - [ ] 删除 DB 房间记录。
  - [ ] 删除 Redis room state。
  - [ ] 删除 Redis presence。
  - [ ] 发布 `room_closed` 事件。
- [ ] 如果需要保留房间历史记录，设计独立状态字段或接口：
  - [ ] `POST /api/admin/rooms/:roomId/close`
  - [ ] 或复用 `DELETE /api/rooms/:roomId` 并在文档中明确“关闭=删除房间”。
- [ ] 管理员接口建议：
  - [ ] `GET /api/admin/rooms`：返回房间列表、owner、在线人数、当前视频、播放状态、创建时间。
  - [ ] `POST /api/admin/rooms/:roomId/close` 或 `DELETE /api/rooms/:roomId`：管理员关闭房间。
  - [ ] 可选：`POST /api/admin/rooms/:roomId/kick/:userId` 与现有 kick 能力对齐。
- [ ] 统一房间事件命名：
  - [ ] 客户端目前同时处理 `room_deleted` 和 `room_closed`，后端实际发布 `room_closed`；建议统一保留 `room_closed`。
- [ ] 补充后端测试：
  - [ ] 普通用户不能关闭他人房间。
  - [ ] 房主可以关闭自己的房间。
  - [ ] 管理员可以关闭任意房间。
  - [ ] 关闭后状态、presence、授权缓存均被清理。
  - [ ] 关闭后发布 `room_closed` 事件。

### 客户端待办

- [ ] 在 `src/api.ts` 增加关闭房间 API，例如 `closeRoom(token, roomId)`。
- [ ] 在 `AdminView.vue` 的房间监控列表中增加“关闭房间”按钮：
  - [ ] 点击前二次确认。
  - [ ] 调用关闭接口。
  - [ ] 成功后刷新房间列表并显示成功消息。
  - [ ] 失败时展示权限或网络错误。
- [ ] 在 `RoomView.vue` 中继续监听 `room_closed`，收到后提示用户并返回大厅；确认事件文案与服务端统一。
- [ ] 如果管理员从后台关闭当前自己正在观看的房间，前端应同时离开房间页并刷新大厅/后台列表。
- [ ] 补充客户端测试：
  - [ ] 管理员后台可以关闭房间。
  - [ ] 普通用户界面不显示管理员关闭按钮。
  - [ ] 房间内用户收到关闭事件后退出房间。

### 验收标准

- [ ] 管理员在后台房间监控列表点击关闭后，该房间从列表消失。
- [ ] 房间内所有在线用户收到“房间已关闭”提示并回到大厅。
- [ ] 被关闭房间无法再次 join/snapshot/token。

## 4. 服务端定时/登录触发空房间清理检查

### 需求描述

在任意用户登录时，应检查 Redis 中的上次清理时间戳。如果已经过去 5 分钟，则触发清理房间，清理没有用户在其中的房间。

### 现状

- 后端已有 `MaybeRunGlobalCleanup`，间隔常量为 `5 * time.Minute`。
- `NewRouter` 中已通过 `authService.SetAfterLogin` 在登录后调用该清理逻辑。
- Redis presence 中已有：
  - `global:last_room_cleanup_at`
  - `global:room_cleanup_lock`
  - `room:pending_empty`
  - `room:active`

### 服务端待办

- [ ] 验证 `Register` 是否也应触发同样清理：当前需求写“任意用户登录”，若注册后直接获得登录态，也可视为登录行为。
- [ ] 检查 memory 与 redis 两套 `RoomPresence` 实现行为是否一致：
  - [ ] last cleanup 时间戳。
  - [ ] cleanup lock。
  - [ ] pending empty 标记。
- [ ] 完善清理策略：
  - [ ] 用户离开房间后，如果房间人数为 0，将 roomID 加入 pending empty。
  - [ ] 登录触发清理时，如果 `last_cleanup_at` 为空或距今超过 5 分钟，尝试获取 lock。
  - [ ] 获得 lock 后扫描 pending empty。
  - [ ] 对仍为 0 人的房间删除 DB 记录、Redis state、presence、授权缓存，并发布 `room_closed`。
  - [ ] 对已有成员重新进入的房间清除 pending empty。
- [ ] 处理异常与幂等：
  - [ ] 房间 DB 已不存在时不阻断清理。
  - [ ] Redis key 缺失时不阻断清理。
  - [ ] 多实例并发登录时只有一个实例执行清理。
  - [ ] 清理失败时不要影响用户登录成功，除非产品要求强一致。
- [ ] 补充后端测试：
  - [ ] 距离上次清理不足 5 分钟时不执行清理。
  - [ ] 超过 5 分钟时执行清理并更新时间戳。
  - [ ] lock 获取失败时不执行清理。
  - [ ] 空房间被删除，非空房间保留。
  - [ ] 登录流程中清理错误的处理符合预期。

### 客户端待办

- [ ] 登录后正常刷新大厅房间列表，避免展示刚被清理掉的空房间。
- [ ] 如果用户尝试进入已被清理房间，`LobbyView.vue` 应展示“房间已关闭或不存在”，并刷新列表。
- [ ] `RoomView.vue` 收到 `room_closed` 后返回大厅，并触发房间列表刷新。
- [ ] 管理后台 `AdminView.vue` 的房间监控应能反映清理后的房间数量变化。

### 验收标准

- [ ] 房间最后一名用户离开后进入待清理状态。
- [ ] 超过 5 分钟后任意用户登录，会触发空房清理。
- [ ] 清理只删除没有用户的房间，不影响已有用户的房间。
- [ ] 多实例并发登录不会重复或互相破坏清理。

## 5. 用户加入新房间时自动退出旧房间，而不是返回 409

### 问题描述

当前用户已在另一个房间时，加入新房间会返回 409：`already in another room; leave that room first`。期望行为是自动退出旧房间并加入新房间。

### 服务端待办

- [ ] 修改 presence `JoinRoom` 语义：
  - [ ] 如果用户不在任何房间，正常加入。
  - [ ] 如果用户已在同一房间，视为幂等成功。
  - [ ] 如果用户已在其他房间，从旧房间成员列表移除，再加入新房间。
  - [ ] 返回 `leftRoomID`，供上层发布离开事件或做清理。
- [ ] Redis Lua 脚本需原子完成：
  - [ ] 读取 `user:room:{userID}` 的旧 roomID。
  - [ ] 若旧房间不同，`HDEL room:members:{oldRoomID} userID`。
  - [ ] 如果旧房间成员数变为 0，移出 `room:active` 并加入 `room:pending_empty`。
  - [ ] `HSET room:members:{newRoomID}` 并更新 `user:room:{userID}`。
  - [ ] 返回旧 roomID 或空字符串。
- [ ] memory presence 实现保持同样行为，避免测试与本地开发表现不一致。
- [ ] `room.Service.Join` 保留返回 `previousRoomID`，并在 handler 中不再将该场景映射为 409。
- [ ] 如果 `previousRoomID` 不为空：
  - [ ] 向旧房间发布 `user_left` 或 `member_moved` 事件，便于旧房间客户端更新成员。
  - [ ] 如旧房间为空，进入 pending empty，等待清理。
- [ ] 设计响应体是否需要包含 `left_room_id`：
  - [ ] 推荐在 join 响应中保留 room 信息，并可选增加 `left_room_id`，方便客户端提示“已从旧房间退出”。
- [ ] 补充后端测试：
  - [ ] 用户从房间 A 加入房间 B，A 的成员被移除，B 的成员存在。
  - [ ] A 若为空则加入 pending empty。
  - [ ] 重复加入 B 幂等成功。
  - [ ] 不再返回 409。
  - [ ] Redis 与 memory 实现一致。

### 客户端待办

- [ ] `LobbyView.vue` 中移除“409 时要求用户先离开”的产品假设，join 成功即打开新房间。
- [ ] 如果后端返回 `left_room_id`，展示非阻塞提示：已自动离开旧房间。
- [ ] `RoomView.vue` 在用户从另一个页面/标签加入新房间时，若旧房间收到 `user_left` 或 `member_moved` 且目标是当前用户，应提示并返回大厅或切换房间。
- [ ] 若用户在多个浏览器标签同时打开不同房间，定义预期行为：
  - [ ] 推荐只允许一个当前房间，旧标签收到事件后提示“你已在其他房间加入，当前房间已退出”。
  - [ ] 旧标签应断开 Ably presence，避免成员列表显示错误。
- [ ] 补充客户端测试：
  - [ ] 从房间 A 返回大厅加入 B，不显示 409 错误。
  - [ ] 加入 B 后 A 的成员列表移除当前用户。
  - [ ] 多标签场景旧标签能收到退出提示。

### 验收标准

- [ ] 已在房间 A 的用户加入房间 B 时，接口返回成功。
- [ ] 用户不再出现在房间 A 的成员列表中。
- [ ] 房间 A 若变为空房，会进入后续空房清理流程。
- [ ] 客户端无 409 错误提示，体验为自动切换房间。

## 6. 接口与数据契约汇总

### 现有接口需要明确/增强

- [ ] `POST /api/rooms/:roomId/join`
  - 请求：`{ "password": "optional" }`
  - 响应建议：`{ room fields..., "is_owner": bool, "left_room_id": "optional" }`
  - 行为：已授权私有房可不传密码；已在其他房间时自动迁移。
- [ ] `POST /api/rooms/:roomId/snapshot`
  - 请求：`{ "password": "optional" }`
  - 行为：已授权私有房可不传密码。
  - 响应：`state.action` 必须准确表达当前播放状态。
- [ ] `POST /api/ably/token`
  - 请求：`{ "room_id": "...", "purpose": "room", "password": "optional" }`
  - 行为：已授权私有房可不传密码。
- [ ] `DELETE /api/rooms/:roomId`
  - 行为：房主或管理员关闭/删除房间。
  - 响应：204。
  - 事件：发布 `room_closed`。

### 可新增接口

- [ ] `GET /api/admin/rooms`
  - 管理员查看房间监控信息。
- [ ] `POST /api/admin/rooms/:roomId/close`
  - 如果不希望前端直接使用 DELETE，可新增更语义化的管理员关闭接口。
- [ ] `GET /api/rooms/:roomId/access`
  - 可选：客户端进入前查询是否已授权私有房；不推荐作为必要路径，优先让 join/snapshot/token 自身处理授权。

## 7. 跨端实现顺序建议

- [ ] 第一阶段：修复 snapshot/play 状态同步，确保新用户加入播放体验正确。
- [ ] 第二阶段：改造房间访问授权，让私有房再次进入免密码，并更新客户端进入流程。
- [ ] 第三阶段：改造 JoinRoom 自动迁移，移除 409 体验。
- [ ] 第四阶段：补齐管理员关闭房间的后台入口与事件体验。
- [ ] 第五阶段：完善空房清理测试、错误处理与管理后台展示。

## 8. 统一测试清单

### 后端

- [ ] 运行现有 Go 测试：`go test ./...`。
- [ ] 为房间同步、私有房授权、presence 迁移、空房清理、管理员关闭房间补充单元/集成测试。
- [ ] Redis 实现需要覆盖 Lua 脚本关键路径；memory 实现用于快速单测但不能替代 Redis 行为验证。

### 客户端

- [ ] 运行类型检查与单元测试：`npm run typecheck`、`npm test`（以客户端仓库实际脚本为准）。
- [ ] 手动验证：
  - [ ] 新用户加入正在播放的房间。
  - [ ] 私有房首次/再次进入。
  - [ ] 管理员关闭房间。
  - [ ] 空房清理后大厅刷新。
  - [ ] 从房间 A 自动切换到房间 B。
- [ ] 浏览器兼容重点：
  - [ ] Chrome autoplay 限制。
  - [ ] 多标签 presence 与退出提示。
  - [ ] Ably token 续签时 JWT 过期的错误提示或刷新流程。

## 9. 待产品确认的问题

- [ ] 私有房“已输入过密码”的有效期：仅当前登录会话、固定 TTL、还是直到房间关闭？
- [ ] 管理员关闭房间是否等同删除房间？是否需要保留关闭记录或审计日志？
- [ ] 用户自动切换房间时，是否需要向旧房间其他成员展示“某某已离开/切换房间”？
- [ ] 空房清理失败是否应该影响登录接口响应？当前建议不影响登录，只记录错误。
- [ ] 新用户加入播放中的房间时，如果浏览器阻止自动播放，产品希望显示按钮、Toast，还是全屏遮罩提示？
