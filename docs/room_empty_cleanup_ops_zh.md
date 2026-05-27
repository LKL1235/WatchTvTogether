# 空房间清理 — 运维与排障

> 创建时间：2026-05-27  
> 实现：`internal/room/hub.go`（`MaybeRunGlobalCleanup`、`RunEmptyRoomCleanup`）、`internal/cache/redis/room_presence.go`。

当房间**服务端 Redis presence 在线人数为 0** 时，应用会关闭房间：删除 PostgreSQL 房间行、Redis 状态 / presence / 私有房授权 / 聊天 Stream，并发布 Ably `room.event`（`room_closed`）。

---

## 1. 触发方式

| 来源 | 行为 |
|------|------|
| 登录 / 注册发 token 后 | `auth.SetAfterLogin` 异步调用 `MaybeRunGlobalCleanup`（不阻塞登录响应） |
| `MaybeRunGlobalCleanup` | 距上次全局清理 ≥ **5 分钟** 且抢到分布式锁（`SETNX`，TTL 2 分钟）时执行 `RunEmptyRoomCleanup` |
| 直接调用 | 测试或运维可调用 `RunEmptyRoomCleanup`（无 5 分钟节流） |

同一轮全局清理结束后还会执行 `ProcessGlobalChatPending`（补发 Ably 聊天待投递）。

---

## 2. 扫描范围（候选房间）

`RunEmptyRoomCleanup` 合并以下三路的 **room_id 并集**（去重）：

1. **`room:pending_empty`** — 成员离开后标记的待清理集合  
2. **仍有成员集合 key 的房间** — `ListRoomsWithMembers`（用于发现「未进 pending 但需复核」的房间）  
3. **PostgreSQL 房间列表** — `List` 最多 500 条（`created_at` 排序）

对每个候选房间调用 `MemberCount`：

- `> 0`：从 `pending_empty` 移除，记为「不需清理」  
- `== 0`：执行 `closeEmptyRoom`

> 设计约束见仓库根目录 [Notes.md](../Notes.md)（Redis 免费版不可用 keyspace 通知；清理必须靠应用层扫描）。

---

## 3. 日志格式（stdout / info）

自 PR #46 起，清理诊断使用 `internal/applog.Infof` 写入 **stdout**（避免被聚合器当成 stderr 错误）。

单房：

```text
room cleanup: 已清理 room_id=<id> 存在时间=<自创建起时长> 在线人数=0
room cleanup: 不需清理 room_id=<id> 存在时间=<...> 在线人数=<n>
```

汇总（每轮一次）：

```text
room cleanup: 触发条件 来源=登录后全局清理(MaybeRunGlobalCleanup) 距上次清理=5m0s 最小间隔=5m0s 清理锁=已获取 扫描pending=<n> 扫描active=<n> 扫描db=<n> 候选房间=<n> 已清理=<n> 不需清理=<n> 规则=在线人数为0时关闭房间
```

失败仍由 `router.go` 以 `log.Printf("room global cleanup: %v", err)` 打在 stderr。

**排障建议：**

- 房间「应该已空」却仍在列表：在日志中查该 `room_id` 是否出现在「不需清理」且 `在线人数>0`（Redis presence 未过期）。  
- 长时间无清理：查是否缺少登录触发，或 5 分钟内已跑过；查 `global:room_cleanup_lock` 是否被占用。  
- 与前端 Ably Presence 不一致：以**服务端 `MemberCount`** 为准决定是否关房；客户端仅作展示。

---

## 4. 相关 Redis 键（节选）

| 键 | 用途 |
|----|------|
| `room:pending_empty` | 待复核空房集合 |
| `global:last_room_cleanup_at` | 上次全局清理时间（毫秒） |
| `global:room_cleanup_lock` | 全局清理互斥锁 |

房间级 presence 与状态键在 `closeEmptyRoom` 中一并删除；聊天 Stream 见 [room_chat_realtime_design_zh.md](./room_chat_realtime_design_zh.md)。

---

## 5. 已知边界

- DB `List` 单次最多 500 房间；超出部分依赖后续登录触发或成员离开进入 `pending_empty`。  
- `refresh` 若未挂载 `AfterLogin`，仅打开站点可能不会触发清理（见 [Notes.md](../Notes.md)）。  
- 用户异常断线且 presence 未剔除时，房间不会被关闭，直至 presence 归零。
