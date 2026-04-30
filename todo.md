# 认证、邮件验证码与 Ably JWT 改造 Todo

> 范围：仅记录待办事项，不改动现有 Go 后端代码。后端仓库当前为 Go + Gin + Postgres/Redis + Ably，客户端参考 `https://github.com/LKL1235/WatchTvTogether-Web`。

## 目标结果

- 注册流程必须通过邮箱验证码完成。
- 找回/重置密码流程必须通过邮箱验证码完成。
- 用户 `id` 继续保持全局唯一，作为服务端和 Ably `clientId` 的稳定身份标识。
- 显示名不唯一；不要把显示名用于登录唯一性判断。
- 登录允许使用邮箱或用户名 + 密码。
- 后端通过 `RESEND_API_KEY` 调用 Resend 发送邮件。
- 邮件发送域名为 `verify.bestlkl.top`，sender 固定为 `WatchTogether <login@verify.bestlkl.top>`。
- 邮箱验证码为 6 位，有效期 10 分钟，同一邮箱/用途 1 分钟只能发送 1 次，每日限制 5 次。
- Ably 鉴权不再返回 Ably TokenDetails，而是由后端签发 Ably JWT 给客户端。
- Ably JWT capability 按房间控制频道 `subscribe/presence/history` 规划，客户端不直接 `publish`。
- Ably JWT 接口返回 JSON：`{ "token": "<jwt>", "expires_at": "..." }`。

## 服务端待办

### 1. 邮件发送能力（Resend）

- [ ] 增加邮件发送模块，建议放在 `internal/email` 或类似目录。
  - [ ] 使用 Resend Go SDK：`github.com/resend/resend-go/v3`。
  - [ ] 从环境变量读取 `RESEND_API_KEY`，不要写入配置文件默认值，不要暴露给前端。
  - [ ] 使用已确认发送身份：域名 `verify.bestlkl.top`，sender 固定为 `WatchTogether <login@verify.bestlkl.top>`。
  - [ ] 增加邮件发件人配置，例如 `RESEND_FROM` / `EMAIL_FROM`，默认示例为 `WatchTogether <login@verify.bestlkl.top>`。
  - [ ] 明确本地开发策略：未配置 `RESEND_API_KEY` 时禁用邮件功能，并且输出日志
- [ ] 按 Resend 文档封装发送接口。
  - [ ] 创建 Resend client：`resend.NewClient(apiKey)`。
  - [ ] 使用 `client.Emails.Send(&resend.SendEmailRequest{From, To, Subject, Html/Text})`。
  - [ ] 发送注册验证码邮件。
  - [ ] 发送找回密码验证码邮件。
  - [ ] 记录 Resend 返回的 email id，便于排查投递问题；不要记录验证码明文。
- [ ] 邮件模板与安全。
  - [ ] 验证码邮件包含 6 位验证码、10 分钟过期时间、用途说明。
  - [ ] 邮件品牌模板、Logo、文案语气由实现方自主设计，保持简洁、可信、移动端可读。
  - [ ] 邮件 HTML 做基础样式即可；同时考虑纯文本 fallback。
  - [ ] 邮件内容不要包含 access token、refresh token、密码或其它敏感信息。

### 2. 配置与环境变量

- [ ] 扩展 `internal/config/config.go`。
  - [ ] 新增 `ResendAPIKey`，从 `RESEND_API_KEY` 读取。
  - [ ] 新增 `EmailFrom`，从 `EMAIL_FROM` 或 `RESEND_FROM` 读取。
  - [ ] 新增验证码 TTL：`EMAIL_CODE_TTL=10m`。
  - [ ] 新增验证码长度：`EMAIL_CODE_LENGTH=6`。
  - [ ] 新增验证码发送冷却时间：`EMAIL_CODE_SEND_INTERVAL=60s`。
  - [ ] 新增每日发送限制：`EMAIL_CODE_DAILY_LIMIT=5`。
  - [ ] 新增验证码尝试次数上限，例如 `EMAIL_CODE_MAX_ATTEMPTS=5`。
- [ ] 更新 `.env.example`。
  - [ ] 增加 `RESEND_API_KEY=`。
  - [ ] 增加 `EMAIL_FROM=WatchTogether <login@verify.bestlkl.top>`。
  - [ ] 增加验证码长度、TTL、冷却时间、每日限制、尝试次数示例值。
- [ ] 更新 `config.yaml` 示例。
  - [ ] 可只保留非敏感项；敏感 key 推荐只使用环境变量。

### 3. 数据模型与存储迁移

- [ ] 调整用户模型 `internal/model/model.go`。
  - [ ] 新增 `Email string`，JSON 字段建议为 `email`。
  - [ ] 明确 `Username` 是唯一登录名，`Nickname`/显示名不唯一。
  - [ ] 如需要单独显示名字段，可继续复用当前 `Nickname`，但接口文档统一称为 display name / 显示名。
- [ ] 调整数据库 schema 与迁移。
  - [ ] `users.email`：非空、唯一、规范化后存储。
  - [ ] `users.username`：继续唯一。
  - [ ] `users.id`：继续主键，保持唯一。
  - [ ] `users.nickname`：保持非唯一，不添加唯一索引。
  - [ ] 旧用户迁移策略：当前没有真实用户，直接忽略旧用户兼容，迁移可破坏性地要求新 schema 必须包含邮箱。
- [ ] 扩展 `store.UserStore`。
  - [ ] 增加 `GetByEmail(ctx, email)`。
  - [ ] 增加 `GetByLogin(ctx, login)` 或在 auth service 内判断邮箱/用户名后分别查询。
  - [ ] 更新 Postgres store 的 insert/update/scan。
  - [ ] 同步更新测试 store / sqlite schema / testutil。
- [ ] 新增验证码存储。
  - [ ] 可选方案 B：Redis 存储验证码 hash + attempts + cooldown，适合短生命周期验证码。
  - [ ] 不存储验证码明文；用 HMAC 或 bcrypt/argon2 hash 存储。
  - [ ] 验证码生成 6 位数字码。
  - [ ] `purpose` 至少区分 `register` 和 `reset_password`。
  - [ ] `expires_at` 固定按 10 分钟有效期计算。
  - [ ] 发送限制口径固定为邮箱 + purpose：冷却 1 分钟，每日发送上限 5 次。
  - [ ] 验证成功后立即标记 consumed 或删除。

### 4. 注册流程重做

- [ ] 重新定义注册 API。
  - [ ] 建议新增 `POST /api/auth/register/code`：请求注册验证码。
    - 请求：`email`。
    - 行为：邮箱规范化、检查邮箱未注册、生成 6 位验证码、发送邮件、设置 1 分钟冷却时间与每日 5 次限制。
    - 响应：不返回验证码，返回 `expires_at` / `retry_after`。
  - [ ] 调整 `POST /api/auth/register`：提交注册信息并校验验证码。
    - 请求：`email`、`username`、`password`、`code`、`nickname/display_name`、`avatar_url`。
    - 行为：校验邮箱验证码、校验用户名唯一、校验邮箱唯一、创建用户、签发当前系统 access/refresh token。
- [ ] 输入校验。
  - [ ] 邮箱 trim + lower-case；使用标准 email 解析或成熟校验库。
  - [ ] 用户名继续 trim + lower-case；保留长度限制并增加字符集规则，如 `a-z0-9_`。
  - [ ] 密码长度继续至少 8 位，必要时提高复杂度要求。
  - [ ] 显示名允许重复；为空时可默认使用用户名。
- [ ] 防滥用。
  - [ ] 对发送验证码接口增加 IP + email 维度限流。
  - [ ] 对验证码校验增加尝试次数上限。
  - [ ] 同一邮箱注册验证码每日最多发送 5 次。
  - [ ] 不通过错误文案泄露某个邮箱是否已注册，除非产品明确需要。

### 5. 邮箱/用户名登录

- [ ] 调整 `auth.Service.Login`。
  - [ ] 参数从 `username` 语义改为 `login`。
  - [ ] 若输入像邮箱，则按 email 查询；否则按 username 查询。
  - [ ] 对用户名和邮箱都做规范化后查询。
  - [ ] 错误统一返回 `invalid username/email or password`，避免账号枚举。
- [ ] 调整 `internal/api/auth_handlers.go`。
  - [ ] 登录请求体使用 `{ "login": "...", "password": "..." }`。
  - [ ] 不保留旧的无邮箱注册或只传 `username` 登录路径；客户端需要同步改造。
  - [ ] 注册请求体增加 `email` 与 `code`。
- [ ] 更新 JWT claims。
  - [ ] 当前系统 access/refresh token 可继续包含 `uid`、`username`、`role`。
  - [ ] 如客户端需要展示邮箱，优先通过 `/api/users/me` 返回，不建议把邮箱放入所有 access token。

### 6. 找回/重置密码流程

- [ ] 新增请求重置密码验证码接口。
  - [ ] 建议 `POST /api/auth/password/reset/code`。
  - [ ] 请求：`email`。
  - [ ] 行为：如果邮箱存在，发送 6 位验证码；如果不存在，也返回相同成功形态，避免账号枚举。
  - [ ] 同一邮箱找回密码验证码每日最多发送 5 次，发送间隔至少 1 分钟。
- [ ] 新增重置密码接口。
  - [ ] 建议 `POST /api/auth/password/reset`。
  - [ ] 请求：`email`、`code`、`new_password`。
  - [ ] 行为：校验验证码、更新密码 hash、使该用户已有 refresh token 失效。
- [ ] 会话失效策略。
  - [ ] 当前 refresh token 存在 `cache.SessionCache` 中，重置密码后需要删除/替换对应用户 refresh token。
  - [ ] 如 access token 无法集中失效，可考虑给用户增加 `token_version` 或 `password_changed_at` 并在 token 校验时比对。

### 7. Ably JWT 签发替代 Ably TokenDetails

- [ ] 保留现有 Ably 发布能力，调整客户端鉴权签发方式。
  - 当前 `internal/realtime/ably/service.go` 使用 `ably-go` REST client 发布消息和 `RequestToken` 签发 Ably token。
  - 新逻辑应停止调用 `rest.Auth.RequestToken` 给客户端返回 TokenDetails。
  - 后端可以继续使用 Ably SDK/REST 发布 `room.sync`、`room.event`、`room.snapshot`。
- [ ] 拆分并解析 `ABLY_ROOT_KEY`。
  - 格式通常是 `keyName:keySecret`，其中 `keyName` 用作 JWT header 的 `kid`，`keySecret` 用作 HS256 签名密钥。
  - 需要校验缺少冒号、keyName 为空、keySecret 为空等配置错误。
- [ ] 签发 Ably JWT。
  - [ ] 使用 `github.com/golang-jwt/jwt/v5` 或手动 HMAC-SHA256 均可；仓库当前已依赖 `github.com/golang-jwt/jwt/v5`。
  - [ ] JWT header：`typ=JWT`、`alg=HS256`、`kid=<ABLY_KEY_NAME>`。
  - [ ] JWT claims：
    - `iat`：当前 Unix 秒。
    - `exp`：当前时间 + Ably JWT TTL。
    - `x-ably-capability`：JSON 字符串，例如 `{"watchtogether:room:<roomId>:control":["subscribe","presence","history"]}`。
    - `x-ably-clientId`：当前登录用户 `id`，继续保持用户 id 唯一且稳定。
  - [ ] TTL 复用或重命名当前 `ABLY_TOKEN_TTL`；若重命名为 `ABLY_JWT_TTL`，需要提供迁移说明。
  - [ ] 能力范围固定为房间控制频道 `subscribe/presence/history`，不包含 `publish`。
- [ ] 调整 API。
  - [ ] 当前 `POST /api/ably/token` 返回 Ably `TokenDetails` JSON。
  - [ ] 改为返回 JSON：`{ "token": "<jwt>", "expires_at": "..." }`。
  - [ ] `expires_at` 使用客户端可直接解析的时间格式，建议 RFC3339 UTC 字符串。
  - [ ] `snapshot` 响应中的 `ably.token_endpoint` 可继续指向该端点，但文档应说明它返回 JWT。
- [ ] 更新测试。
  - [ ] 验证 JWT header `kid` 与 `alg`。
  - [ ] 验证 claims 包含 `x-ably-capability`、`x-ably-clientId`、`iat`、`exp`。
  - [ ] 验证响应 JSON 包含 `token` 与 `expires_at`，且不再包含 Ably TokenDetails 字段。
  - [ ] 验证私有房间密码/授权逻辑仍适用于 JWT 签发端点。
  - [ ] 验证 JWT 能力不包含 `publish`。

### 8. API 文档与兼容策略

- [ ] 更新 README 核心接口。
  - [ ] 新增注册验证码接口。
  - [ ] 新增找回/重置密码接口。
  - [ ] 标注登录字段支持邮箱或用户名。
  - [ ] 标注 `/api/ably/token` 改为 Ably JWT。
- [ ] 更新错误码与错误文案。
  - [ ] 邮箱验证码错误、过期、冷却中、尝试次数过多。
  - [ ] 登录失败统一文案。
  - [ ] 注册邮箱/用户名冲突文案。
- [ ] 对旧客户端兼容做明确选择。
  - [ ] 不继续接受旧的 `username` 登录字段作为主协议字段；客户端统一迁移到 `login`。
  - [ ] 不保留旧的无邮箱注册路径；客户端和服务端需要同步发布。

## 客户端待办（WatchTvTogether-Web）

### 1. 环境与 API client

- [ ] 不在前端配置 `RESEND_API_KEY`、`ABLY_ROOT_KEY` 或任何邮件/Ably root secret。
- [ ] 更新 API 类型定义。
  - [ ] 注册请求增加 `email`、`code`。
  - [ ] 登录请求从 `username` 改为 `login`，UI 文案为“邮箱/用户名”。
  - [ ] 新增请求注册验证码接口。
  - [ ] 新增请求找回密码验证码接口。
  - [ ] 新增重置密码接口。
  - [ ] Ably JWT 响应类型定义为 `{ token: string; expires_at: string }`。
- [ ] 更新错误处理。
  - [ ] 处理验证码冷却倒计时。
  - [ ] 处理验证码过期/错误/尝试过多。
  - [ ] 处理验证码每日发送上限 5 次的错误提示。
  - [ ] 处理邮箱或用户名已存在。

### 2. 登录/注册页面（AuthView）

- [ ] 登录表单。
  - [ ] 输入框 label 改为“邮箱或用户名”。
  - [ ] 请求体必须使用 `login`，移除旧 `username` 登录字段调用。
  - [ ] 错误提示统一为“邮箱/用户名或密码错误”。
- [ ] 注册表单。
  - [ ] 增加邮箱输入。
  - [ ] 增加“发送验证码”按钮。
  - [ ] 增加验证码输入。
  - [ ] 发送后展示倒计时，倒计时期间禁用重复发送。
  - [ ] 展示或处理每日最多发送 5 次限制。
  - [ ] 提交注册时携带 `email`、`username`、`password`、`code`、`nickname/display_name`。
  - [ ] 显示名文案明确为可重复；用户名文案明确为唯一。
- [ ] 表单校验。
  - [ ] 邮箱格式基础校验。
  - [ ] 用户名长度/字符规则与后端一致。
  - [ ] 密码长度与后端一致。
  - [ ] 验证码长度固定为 6 位数字。

### 3. 找回密码页面/流程

- [ ] 在登录页增加“忘记密码？”入口。
- [ ] 新增找回密码视图或弹窗。
  - [ ] 步骤 1：输入邮箱并发送验证码。
  - [ ] 步骤 2：输入验证码与新密码。
  - [ ] 步骤 3：重置成功后引导回登录页。
- [ ] 安全与体验。
  - [ ] 不提示“邮箱不存在”这类可枚举账号的文案。
  - [ ] 支持 1 分钟重新发送验证码倒计时。
  - [ ] 处理每日最多发送 5 次限制。
  - [ ] 新密码提交成功后清空表单敏感内容。

### 4. 当前用户与展示名

- [ ] 统一用户展示字段。
  - [ ] 用户名用于登录唯一标识。
  - [ ] 显示名/`nickname` 用于 UI 展示，允许重复。
  - [ ] 房间成员列表、在线 Presence、管理员页面都优先展示显示名；必要时附带 username 区分。
- [ ] 更新 `/api/users/me` 类型。
  - [ ] 增加 `email` 字段展示或账户设置使用。
  - [ ] 不把 email 用作公开房间成员展示，除非产品明确需要。

### 5. Ably 客户端鉴权迁移到 JWT

- [ ] 更新当前 Ably 初始化逻辑。
  - [ ] 仍从 `POST /api/rooms/:roomId/snapshot` 获取 `ably.channel` 与 `ably.token_endpoint`。
  - [ ] `authCallback` 请求后端 token endpoint 时，预期返回 Ably JWT。
  - [ ] 后端返回 JSON：解析 `{ token, expires_at }` 后将 `token` 传给 Ably。
  - [ ] 可在本地记录 `expires_at` 用于调试续签时机，但实际续签仍交给 Ably SDK 的 `authCallback`。
- [ ] 保留现有私有房间逻辑。
  - [ ] token endpoint 仍需携带 `room_id`、`purpose=room`、必要时携带本次会话内存中的房间密码。
  - [ ] 认证失败时提示重新加入房间或重新输入私有房密码。
- [ ] 更新 token 刷新处理。
  - [ ] Ably SDK 会在 JWT 临近过期时再次调用 `authCallback`。
  - [ ] 后端返回 401 时先尝试刷新本系统 access token，再重试一次 Ably JWT 获取。
  - [ ] 后端返回 403 时提示房间权限失效或密码错误。
- [ ] 更新前端 README。
  - [ ] 将“Ably 临时 token”改为“Ably JWT”。
  - [ ] 明确前端仍不能配置 Ably root key。
