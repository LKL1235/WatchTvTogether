# WatchTogether：Vercel 部署与移除服务端下载 — 待办清单

部署目标：**后端跑在 Vercel（无持久磁盘、不适合长任务与本地写文件）**，因此应移除「把视频下载到服务器」整条能力，并一并去掉 **ffmpeg / yt-dlp**（以及下载链路里用到的 **aria2** 等）的安装、探测与业务依赖。

> 客户端仓库参考：[WatchTvTogether-Web](https://github.com/LKL1235/WatchTvTogether-Web)（Vue Web）。以下「客户端」小节在该仓库中执行。

---

## 一、后端（本仓库 Go）— 移除下载与工具链

### 1. 业务与 API

- [ ] 删除 `internal/download` 包及 `internal/api/download_handlers.go` 中的管理端路由：`POST/GET/DELETE /api/admin/downloads` 等；在 `internal/api/router.go` 中取消 `registerDownloadRoutes`。
- [ ] 在 `cmd/server/main.go` 中移除 `DownloadService` 的构造、`Start`、context cancel，以及 `Dependencies` / handler 注入里的下载服务。
- [ ] 梳理是否仍存在依赖「本机 `StorageDir` 上的视频文件」的接口（例如 `GET /api/videos/:id/file` 走 `c.FileAttachment`）。在纯 Vercel 场景下需明确：**视频来源改为外链 / HLS URL、或对象存储（S3/R2）+ 签名 URL** 等，并在待办中拆出独立里程碑（本清单不展开实现细节）。
- [ ] 若通过 Ably 或其它通道广播 `admin:downloads`（`internal/download/service.go` 中的 `UpdatesChannel`），删除相关发布与文档说明。

### 2. 数据层与模型

- [ ] 从 `internal/store` 接口与 **Postgres / SQLite / 内存测试 store** 中移除 `DownloadTaskStore` 及 `DownloadTask` 的 CRUD；更新 `internal/api/test_stores_test.go` 等测试桩。
- [ ] 数据库：新增迁移 **删除 `download_tasks` 表**（或保留表但废弃，二选一；推荐新迁移 `DROP TABLE` 并更新 `migrations` 与 `internal/store/postgres/schema.sql`、`internal/store/sqlite/schema.sql` 保持一致）。
- [ ] `internal/model/model.go`：移除 `DownloadTask` / `DownloadTaskStatus`（若已无引用）。

### 3. Capabilities 与 `pkg/*` 工具探测

- [ ] `internal/capabilities/capabilities.go`：移除对 `pkg/ffmpeg`、`pkg/ytdlp`、`pkg/aria2` 的探测；删除或收窄 `Report` / `Features` / `ToolReport` 中与 **HLS 下载、yt-dlp 导入、磁力 aria2、海报生成、metadata** 等仅服务于「本地下载/转码」的字段（**注意：这是 API 契约变更**，需在 README 中写明，并通知客户端做兼容或同步发版）。
- [ ] 删除或合并不再使用的包：`pkg/ffmpeg`、`pkg/ytdlp`、`pkg/aria2`（若全仓库无其它引用）。
- [ ] 全局搜索 `ffmpeg`、`yt-dlp`、`ytdlp`、`aria2` 字符串，清理残留 import 与注释。

### 4. 配置、环境与镜像

- [ ] `internal/config/config.go`、`config.yaml`、`.env.example`：移除 `download_workers`、`aria2_rpc_url`、`aria2_secret` 等仅服务下载的配置项及 env 映射。
- [ ] `docker-compose.yml` / `docker-compose.dev.yml`：删除 `DOWNLOAD_WORKERS` 等与下载相关的环境变量块。
- [ ] `Dockerfile`：移除 `apt-get install ffmpeg`、安装 `yt-dlp` 的步骤（以及仅为下载服务的依赖）。
- [ ] `.github/workflows/ci.yml`：移除 CI 中安装 `ffmpeg`、`yt-dlp` 的步骤（若移除下载后仍有测试依赖本地文件探测，再按需最小化保留或改为 mock）。

### 5. 文档与测试

- [ ] `README.md`：更新「核心接口」与目录说明，去掉「下载任务」、静态文件服务中与「本地下载入库」矛盾的表述；补充 **Vercel 限制**（无长驻进程、无大磁盘、不适合 aria2/ffmpeg 拉流落盘）。
- [ ] `Notes.md`（若存在相关说明）同步更新。
- [ ] 修复或删除 `internal/api/router_test.go` 中与下载流程强绑定的用例（如 `TestCapabilitiesDownloadsAndVideosFlow`）；`internal/store/testutil/store_suite.go` 中与 `download_tasks` 相关的子测试。
- [ ] `internal/config/config_test.go`：去掉对已删除配置项的断言（若有）。

---

## 二、客户端（[WatchTvTogether-Web](https://github.com/LKL1235/WatchTvTogether-Web)）— 建议待办

以下在 **WatchTvTogether-Web** 仓库中完成；需对照当前分支实际文件名与路由（仓库公开页信息较少，以克隆后代码为准）。

### 1. 与下载能力解耦

- [ ] 移除或隐藏管理端「创建下载任务 / 任务列表 / 取消任务」等 UI 与 API 调用（对应后端将删除的 `/api/admin/downloads*`）。
- [ ] 移除对 `GET /api/capabilities` 中 **`ytdlp_import`、`hls_download`、`magnet_download`**（以及若后端删除后不再返回的 **ffmpeg / ffprobe / aria2 工具块**）的依赖；避免把「可下载」作为功能开关导致空白或报错。
- [ ] 若存在轮询或实时订阅「下载进度」的逻辑，一并删除并清理状态管理（Vuex/Pinia/composable 等）。

### 2. 与 Vercel 部署的后端对齐

- [ ] 使用环境变量配置 **API Base URL**（生产 / 预览 / 本地），指向 Vercel 上的 Go API 域名；检查 CORS 与 cookie（若使用）是否需改为跨站策略或与后端 `Access-Control-*` 一致。
- [ ] 确认 Ably：`POST /api/ably/token` 与前端 Ably 初始化仍匹配；部署域名变更后更新白名单（若后端有配置）。

### 3. 视频与播放体验（产品向）

- [ ] 若原流程为「下载完成后从后端取本地文件 URL」，改为 **外链直链 / HLS** 或 **对象存储 URL** 等后端新约定（需与后端「无本地下载」后的视频模型对齐）。
- [ ] 播放器组件：支持常见流媒体形态（如 HLS）时，确认浏览器侧依赖（如 hls.js）与 HTTPS 要求。

### 4. 工程与发布

- [ ] 在 WatchTvTogether-Web 的 README 或部署文档中注明：**后端已部署在 Vercel**，不再提供服务端视频下载能力。
- [ ] 回归：注册/登录、房间列表、进房、同步播放、错误提示；管理端若仍有「视频管理」则只保留与新模式一致的入口。

---

## 三、风险与顺序提示

- **破坏性变更**：管理端下载 API 与 `capabilities` JSON 形状会变；建议后端与客户端 **约定版本号或同时发版**，避免旧前端调用已删除路由。
- **数据**：下线下载前若有未完成任务，决定是否备份 `download_tasks` 或直接丢弃。
- **视频文件路径**：当前 `Video` 模型含 `file_path` 等字段；全面 Vercel 化时往往要演进为「远程 URL + 元数据」模式，与「仅删下载」可分阶段实施。

---

*文档生成依据：本仓库内 `internal/download`、`internal/capabilities`、`pkg/ffmpeg`、`pkg/ytdlp`、`pkg/aria2`、`Dockerfile`、CI 与迁移中的 `download_tasks` 等现状整理。*
