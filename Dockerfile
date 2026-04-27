# ─── Stage 1: Build ───────────────────────────────────────────────────────────
# 使用官方 Go 镜像编译二进制，产物为静态链接的 Linux 可执行文件
FROM golang:1.22-alpine AS builder

# 安装构建依赖（git 用于 go mod 下载带 VCS 信息的依赖）
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /src

# 优先复制 go.mod/go.sum 以利用 Docker 层缓存
COPY go.mod go.sum ./
RUN go mod download

# 复制全部源码并编译
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/watchtogether ./cmd/server

# ─── Stage 2: Runtime ─────────────────────────────────────────────────────────
# 使用 Ubuntu 24.04 作为运行时基础镜像：
#   - apt 官方源的 ffmpeg 包含完整编解码器
#   - yt-dlp 的 Python 运行时生态完善
FROM ubuntu:24.04

# 环境变量：避免 apt 交互式提示
ENV DEBIAN_FRONTEND=noninteractive \
    TZ=Asia/Shanghai

# 安装运行时依赖
RUN apt-get update && apt-get install -y --no-install-recommends \
        ffmpeg \
        ca-certificates \
        tzdata \
        curl \
        python3 \
    && rm -rf /var/lib/apt/lists/*

# 安装 yt-dlp（从官方 GitHub Release 下载静态二进制，无需 pip）
RUN curl -fsSL https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp \
        -o /usr/local/bin/yt-dlp \
    && chmod +x /usr/local/bin/yt-dlp

# 创建非 root 运行用户
RUN useradd -r -u 1001 -s /bin/false appuser

# 复制编译产物
COPY --from=builder /out/watchtogether /usr/local/bin/watchtogether

# 数据目录（视频文件、SQLite 数据库等持久化数据挂载点）
RUN mkdir -p /data/videos /data/posters && \
    chown -R appuser:appuser /data

USER appuser
WORKDIR /app

# 默认配置文件（可通过挂载覆盖）
COPY --chown=appuser:appuser config.yaml /app/config.yaml

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:8080/healthz || exit 1

ENTRYPOINT ["watchtogether"]
CMD ["--config", "/app/config.yaml"]
