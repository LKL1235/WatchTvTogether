FROM golang:1.25-alpine AS backend-builder
WORKDIR /src
RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 go build -trimpath -ldflags="-s -w" -o /out/watchtogether ./cmd/server

FROM ubuntu:24.04 AS runtime
RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ca-certificates curl ffmpeg \
    && curl -L "https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp" -o /usr/local/bin/yt-dlp \
    && chmod +x /usr/local/bin/yt-dlp \
    && groupadd -r watchtogether \
    && useradd -r -g watchtogether -d /app -s /usr/sbin/nologin watchtogether \
    && mkdir -p /app /data/videos /data/posters \
    && chown -R watchtogether:watchtogether /app /data \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=backend-builder /out/watchtogether /app/watchtogether
COPY config.yaml /app/config.yaml
ENV ADDR=:8080 \
    STORAGE_DIR=/data/videos \
    POSTER_DIR=/data/posters
EXPOSE 8080
VOLUME ["/data"]
USER watchtogether
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD curl -fsS http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/app/watchtogether"]
