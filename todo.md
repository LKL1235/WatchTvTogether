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
- 已确认的产品决策：
  - 私有房密码记忆使用“方案 A：服务端访问授权”，授权有效期直到房间关闭/删除。
  - 管理员关闭房间等同删除房间，不需要关闭记录和审计日志。
  - 用户加入房间、离开房间、自动切换房间时，应向房间内其他用户展示“某某已加入/离开”。
  - 空房清理为异步操作，不影响登录接口响应。
  - 新用户加入播放中的房间时，如果浏览器阻止自动播放，在视频播放器区域显示遮罩与“同步进度”按钮。
  - 空房清理判断标准是“没有在线用户”，不是“没有用户/成员记录”。

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
  - [ ] 当 snapshot 表示 `play` 但 `video.play()` 被浏览器拒绝时，在视频播放器区域显示半透明遮罩，而不是 Toast 或全屏遮罩。
  - [ ] 遮罩文案建议：“房间正在播放，浏览器阻止了自动播放”。
  - [ ] 遮罩中提供“同步进度”按钮，点击后重新读取最新 state/snapshot，将视频跳到当前投影进度并调用播放。
  - [ ] 如果再次播放仍被阻止，保留遮罩并提示用户需要先点击播放器或解除静音。
  - [ ] 避免把新用户本地 autoplay 失败误上报成房间暂停；普通成员不得发送控制消息。
- [ ] 在 `useRoomRealtime.ts` 连接成功后，可选择读取 Ably history 或主动刷新 state，确保连接建立期间错过的 `room.sync` 不导致新用户状态落后。
- [ ] 补充客户端测试：
  - [ ] `applyRoomSnapshot` 收到 `play` 时会调用播放器播放逻辑。
  - [ ] autoplay 失败时显示恢复播放提示，不改变房间权威状态。
  - [ ] 普通成员只应用远端状态，不发送 `control`。

### 验收标准

- [ ] 房主播放视频后，新用户加入同一房间，视频定位到当前进度并尝试播放。
- [ ] 如浏览器阻止自动播放，新用户能看到明确提示，并可通过一次点击恢复到当前进度继续播放。
- [ ] 自动播放被阻止时，提示出现在视频播放器区域遮罩内，并包含“同步进度”按钮。
- [ ] 新用户加入不会导致房主或其他用户的视频被暂停。

## 2. 私有房密码记忆：同一房间再次进入不应重复要求密码

### 问题描述

同一用户进入私有房间并输入过密码后，再次进入同一房间不应该再次要求密码。

### 接口设计

采用服务端授权记录，不使用客户端会话缓存密码作为正式方案。

#### 方案 A：服务端记录房间访问授权（已确定）

- [ ] 新增房间访问授权缓存接口，例如：
  - `GrantRoomAccess(ctx, roomID, userID)`
  - `HasRoomAccess(ctx, roomID, userID) bool`
  - `RevokeRoomAccess(ctx, roomID, userID)`（可选）
  - `DeleteRoomAccess(ctx, roomID)`（房间关闭/删除时清理）
- [ ] Redis key 建议：
  - `room:access:{roomID}` 使用 set 存 userID。
  - 不依赖固定 TTL 表达产品语义；授权直到房间关闭/删除时统一清理。
  - 可保留较长 Redis TTL 作为异常兜底，但业务有效期以房间存在为准。
- [ ] `POST /api/rooms/:roomId/join`：
  - [ ] 如果用户是房主/管理员，跳过密码。
  - [ ] 如果用户已有该房授权，跳过密码。
  - [ ] 如果密码校验成功，写入授权记录。
- [ ] `POST /api/rooms/:roomId/snapshot` 与 `POST /api/ably/token`：
  - [ ] 支持使用已有授权跳过密码。
  - [ ] 密码校验成功时也可写入授权，避免 join 与 snapshot/token 时序差异。
- [ ] `DELETE /api/rooms/:roomId` / 空房清理时同步删除该房授权缓存。
- [ ] 如果未来支持房间密码修改，密码修改时应清理该房已有授权；当前需求未要求实现密码修改。
- [ ] 修改错误语义：
  - [ ] 未授权访问私有房仍返回 403。
  - [ ] 已授权用户不需要再次提交 password。
- [ ] 补充后端测试：
  - [ ] 首次输入正确密码 join 成功并写入授权。
  - [ ] 同一用户同一房间再次 join/snapshot/ably token 不带密码也成功。
  - [ ] 不同用户不能复用该授权。
  - [ ] 房间删除后授权失效。

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
  - [ ] 授权不存在、房间已关闭后重建、或密码校验失败时需要重新输入。
- [ ] 补充客户端测试：
  - [ ] 私有房首次进入需要输入密码。
  - [ ] 同一用户再次进入不弹密码。
  - [ ] 403 时仍能回退到密码弹窗。

### 验收标准

- [ ] 用户 A 输入密码进入私有房后，返回大厅再进入同一房间，不再弹密码。
- [ ] 用户 B 未输入过密码时仍需要密码。
- [ ] 房主/管理员仍无需密码。
- [ ] 授权直到房间关闭/删除前持续有效；房间关闭/删除后授权失效。
- [ ] 房间关闭后重建为新的房间 ID 时，客户端能重新要求密码。

## 3. 加强管理功能：管理员手动关闭房间

### 问题描述

现有管理能力需要加强，至少支持管理员手动关闭房间，并通知房间内用户退出。

### 服务端待办

- [ ] 明确产品语义：管理员关闭房间等同删除房间，不需要关闭记录和审计日志。
- [ ] 梳理当前 `DELETE /api/rooms/:roomId` 是否已满足“关闭房间=删除房间”语义：
  - [ ] 删除 DB 房间记录。
  - [ ] 删除 Redis room state。
  - [ ] 删除 Redis presence。
  - [ ] 删除私有房访问授权缓存。
  - [ ] 发布 `room_closed` 事件。
- [ ] 管理员接口建议：
  - [ ] `GET /api/admin/rooms`：返回房间列表、owner、在线人数、当前视频、播放状态、创建时间。
  - [ ] 复用 `DELETE /api/rooms/:roomId` 作为管理员关闭房间接口，文档中明确“关闭=删除房间”。
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

在任意用户登录时，应检查 Redis 中的上次清理时间戳。如果已经过去 5 分钟，则异步触发房间清理。清理目标是没有在线用户的房间，不是没有用户记录的房间。

### 现状

- 后端已有 `MaybeRunGlobalCleanup`，间隔常量为 `5 * time.Minute`。
- `NewRouter` 中已通过 `authService.SetAfterLogin` 在登录后调用该清理逻辑。
- `auth.Service.Register` 与 `auth.Service.Login` 都会调用 `runAfterLogin`；`Refresh` 不会调用。
- 当前 `runAfterLogin` 同步执行并忽略错误：`_ = s.afterLogin(ctx)`。这能保证错误不返回给登录接口，但仍可能增加登录耗时，且清理失败没有日志或可观测性。
- Redis presence 中已有：
  - `global:last_room_cleanup_at`
  - `global:room_cleanup_lock`
  - `room:pending_empty`
  - `room:active`

### 疑似问题与排查重点

- [ ] 实际使用中“没有看到登录触发清理”，需要排查是否存在以下 bug 或语义偏差：
  - [ ] 当前清理只扫描 `room:pending_empty`，如果某个房间没有在线用户但从未被加入 pending empty，就不会被清理。
  - [ ] 当前 pending empty 主要由后端 `POST /api/rooms/:roomId/leave` 或 presence 迁移触发；如果用户直接关闭页面、断网、刷新、浏览器崩溃，后端 Redis presence 可能仍保留 `room:members:{roomID}`，不会认为房间为空。
  - [ ] 客户端使用 Ably Presence 展示在线用户，但服务端清理使用自建 Redis presence；两套 presence 可能不一致，导致“前端看起来没人在线，后端认为仍有人在房间”。
  - [ ] Redis member TTL 当前较长，过期前可能一直阻止空房清理。
  - [ ] `runAfterLogin` 吞掉清理错误且没有日志，Redis 连接、lock、解析时间戳等错误可能完全不可见。
  - [ ] 如果用户通过 refresh token 恢复登录态而不是调用 `/api/auth/login`，不会触发 `SetAfterLogin` hook。
  - [ ] 如果部署环境不是 Redis cache backend，memory presence 的 last cleanup/pending empty 只在单进程内有效，重启后状态丢失。

### 服务端待办

- [ ] 保持 `Register` 触发同样清理：注册后直接获得登录态，视为登录行为。
- [ ] 明确 `Refresh` 是否也算“任意用户登录/恢复会话”：如果前端主要通过 refresh 恢复登录态，应在 refresh 成功后也异步触发同样检查。
- [ ] 检查 memory 与 redis 两套 `RoomPresence` 实现行为是否一致：
  - [ ] last cleanup 时间戳。
  - [ ] cleanup lock。
  - [ ] pending empty 标记。
- [ ] 将清理改为异步触发：
  - [ ] 登录/注册/可选 refresh 成功后启动后台 goroutine 或任务队列执行 `MaybeRunGlobalCleanup`。
  - [ ] 不使用会被请求结束取消的 context 直接跑后台任务；应派生带超时的后台 context。
  - [ ] 清理失败只记录日志/指标，不影响登录响应。
  - [ ] 登录接口不等待清理完成。
- [ ] 完善清理策略：
  - [ ] 用户离开房间后，如果房间在线人数为 0，将 roomID 加入 pending empty。
  - [ ] 登录触发清理时，如果 `last_cleanup_at` 为空或距今超过 5 分钟，尝试获取 lock。
  - [ ] 获得 lock 后扫描 pending empty，并补充扫描可能遗漏的房间（例如 DB 房间列表或 `room:active`）来发现“没有在线用户但未进入 pending empty”的房间。
  - [ ] 判定在线人数时，以真实在线状态为准；需决定服务端是否接入 Ably Presence 查询，或让客户端/服务端可靠同步在线离开事件到 Redis。
  - [ ] 对没有在线用户的房间删除 DB 记录、Redis state、presence、授权缓存，并发布 `room_closed`。
  - [ ] 对已有在线用户重新进入的房间清除 pending empty。
- [ ] 处理异常与幂等：
  - [ ] 房间 DB 已不存在时不阻断清理。
  - [ ] Redis key 缺失时不阻断清理。
  - [ ] 多实例并发登录时只有一个实例执行清理。
  - [ ] 清理失败不影响用户登录成功，但必须记录日志，便于排查“没有触发”的线上问题。
- [ ] 补充后端测试：
  - [ ] 距离上次清理不足 5 分钟时不执行清理。
  - [ ] 超过 5 分钟时执行清理并更新时间戳。
  - [ ] lock 获取失败时不执行清理。
  - [ ] 没有在线用户的房间被删除，有在线用户的房间保留。
  - [ ] 未进入 pending empty 但没有在线用户的房间也能被发现并清理。
  - [ ] 登录流程中清理错误不影响登录响应，且错误被记录。

### 客户端待办

- [ ] 登录后正常刷新大厅房间列表，避免展示刚被清理掉的空房间。
- [ ] 如果用户尝试进入已被清理房间，`LobbyView.vue` 应展示“房间已关闭或不存在”，并刷新列表。
- [ ] `RoomView.vue` 收到 `room_closed` 后返回大厅，并触发房间列表刷新。
- [ ] 管理后台 `AdminView.vue` 的房间监控应能反映清理后的房间数量变化。

### 验收标准

- [ ] 房间最后一名在线用户离开后进入待清理状态。
- [ ] 超过 5 分钟后任意用户登录，会异步触发空房清理，登录响应不等待清理完成。
- [ ] 清理只删除没有在线用户的房间，不影响有在线用户的房间。
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
- [ ] 用户加入房间成功时，向目标房间发布 `user_joined` 事件，事件 payload 至少包含 user id、username、role、is_owner。
- [ ] 用户主动离开房间时，向原房间发布 `user_left` 事件，事件 payload 包含用户信息。
- [ ] 如果 `previousRoomID` 不为空：
  - [ ] 向旧房间发布 `user_left` 事件，便于旧房间客户端展示“某某已离开”并更新成员。
  - [ ] 向新房间发布 `user_joined` 事件，便于新房间客户端展示“某某已加入”。
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
- [ ] `RoomView.vue` 监听 `user_joined` / `user_left` 事件：
  - [ ] 其他用户加入时展示“某某已加入房间”。
  - [ ] 其他用户离开或切换到别的房间时展示“某某已离开房间”。
  - [ ] 避免对当前用户自己的 join/leave 事件重复弹提示。
- [ ] `RoomView.vue` 在用户从另一个页面/标签加入新房间时，若旧房间收到针对当前用户的 `user_left`，应提示“你已加入其他房间，当前房间已退出”并返回大厅或切换房间。
- [ ] 若用户在多个浏览器标签同时打开不同房间，定义预期行为：
  - [ ] 推荐只允许一个当前房间，旧标签收到事件后提示“你已在其他房间加入，当前房间已退出”。
  - [ ] 旧标签应断开 Ably presence，避免成员列表显示错误。
- [ ] 补充客户端测试：
  - [ ] 从房间 A 返回大厅加入 B，不显示 409 错误。
  - [ ] 加入 B 后 A 的成员列表移除当前用户。
  - [ ] 房间内其他用户能看到加入/离开提示。
  - [ ] 多标签场景旧标签能收到退出提示。

### 验收标准

- [ ] 已在房间 A 的用户加入房间 B 时，接口返回成功。
- [ ] 用户不再出现在房间 A 的成员列表中。
- [ ] 房间 A 若变为空房，会进入后续空房清理流程。
- [ ] 房间 A 的其他用户看到“某某已离开”，房间 B 的其他用户看到“某某已加入”。
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
  - 行为：房主或管理员关闭/删除房间；关闭等同删除。
  - 响应：204。
  - 事件：发布 `room_closed`。
- [ ] `room.event`
  - `user_joined`：用户加入房间时发布，客户端展示“某某已加入”。
  - `user_left`：用户主动离开或自动切换离开旧房间时发布，客户端展示“某某已离开”。
  - `room_closed`：房间关闭/删除时发布，客户端提示并返回大厅。

### 可新增接口

- [ ] `GET /api/admin/rooms`
  - 管理员查看房间监控信息。
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
  - [ ] 用户加入/离开/自动切换时，房间内其他用户看到对应提示。
  - [ ] 自动播放被阻止时，视频播放器区域出现遮罩和“同步进度”按钮。
- [ ] 浏览器兼容重点：
  - [ ] Chrome autoplay 限制。
  - [ ] 多标签 presence 与退出提示。
  - [ ] Ably token 续签时 JWT 过期的错误提示或刷新流程。

## 9. 已确认的产品口径

- [x] 私有房“已输入过密码”的有效期：直到房间关闭/删除。
- [x] 私有房密码记忆实现方案：服务端访问授权（方案 A）。
- [x] 管理员关闭房间等同删除房间，不需要关闭记录和审计日志。
- [x] 用户自动切换房间、主动离开、加入房间时，需要向房间内其他用户展示“某某已加入/离开”。
- [x] 空房清理失败或执行中不影响登录响应；清理应异步执行。
- [x] 新用户加入播放中的房间时，如果浏览器阻止自动播放，在视频播放器区域展示遮罩和“同步进度”按钮。
- [x] 空房清理目标是没有在线用户的房间，不是没有用户记录的房间。
