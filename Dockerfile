# Docker 环境标准：Docker 29.7.2 + Compose v5.4.0（部署前运行 ./check-docker-env.sh 校验）
# syntax=docker/dockerfile:1

FROM node:20-bookworm-slim AS web

WORKDIR /src/web

COPY web/package.json web/package-lock.json ./
RUN npm config set registry https://registry.npmmirror.com \
    && npm ci

COPY web/ ./
RUN npm run build


FROM golang:1.27.0-bookworm AS build

WORKDIR /src

# 与 go.mod 的 go 1.27.0 对齐；local 禁止再去拉 toolchain，避免 proxy.golang.org 中断
ENV GOTOOLCHAIN=local \
    CGO_ENABLED=0 \
    GOPROXY=https://goproxy.cn,direct
ARG BUILD_TAGS=fuse

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=web /src/internal/api/web /src/internal/api/web

RUN go build -tags "${BUILD_TAGS}" -trimpath -ldflags="-s -w" -o /out/litepan ./cmd/litepan


FROM debian:bookworm-slim AS runtime

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata fuse3 curl \
    && sed -i 's/^#user_allow_other/user_allow_other/' /etc/fuse.conf 2>/dev/null || true \
    && grep -q '^user_allow_other' /etc/fuse.conf || echo user_allow_other >> /etc/fuse.conf \
    && rm -rf /var/lib/apt/lists/* \
    && useradd -r -u 1000 -m -d /home/litepan -s /usr/sbin/nologin litepan \
    && mkdir -p /app/data/log /app/mounts \
    && chown -R litepan:litepan /app /home/litepan

COPY --from=build /out/litepan /app/litepan
RUN chown litepan:litepan /app/litepan && chmod 755 /app/litepan

ENV LITEPAN_DATA_DIR=/app/data \
    LITEPAN_LISTEN=:5211 \
    LITEPAN_LOG_LEVEL=info \
    TZ=Asia/Shanghai

EXPOSE 5211 42069/tcp 42069/udp

VOLUME ["/app/data", "/app/mounts"]

# 健康检查：依赖 curl，容器以 litepan 用户运行
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -fsS http://127.0.0.1:5211/api/health || exit 1

USER litepan

ENTRYPOINT ["/app/litepan"]
