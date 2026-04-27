# Ubuntu 24.04: FFmpeg 6.x (includes ffprobe in the same package as ffmpeg)
FROM ubuntu:24.04 AS base

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    ffmpeg \
    aria2 \
    python3 \
    && rm -rf /var/lib/apt/lists/*

# Official standalone binary; Python is optional (yt-dlp can be self-contained).
ARG YT_DLP_URL=https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp
RUN curl -fsSL -o /usr/local/bin/yt-dlp "${YT_DLP_URL}" && chmod 755 /usr/local/bin/yt-dlp

# Build: compile the Go server.
FROM base AS build
RUN apt-get update && apt-get install -y --no-install-recommends golang-go \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY go.mod go.sum* ./
COPY . .
RUN go mod tidy
RUN go build -trimpath -o /out/server ./cmd/server

# Runtime: binary + ffmpeg / ffprobe / yt-dlp / aria2c in PATH
FROM base AS runtime
COPY --from=build /out/server /usr/local/bin/watchtogether-server
ENV ADDR=:8080
EXPOSE 8080
USER nobody
CMD ["/usr/local/bin/watchtogether-server"]
