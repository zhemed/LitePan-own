# AGENTS.md — Agent 工作指引

本文件是该仓库中 AI 代理（以及任何新的对话/协作者）必须遵守的项目约定。

## Docker 环境标准（强制）

本项目 Docker 环境固定为 **Docker Engine 29.7.2 + Docker Compose v5.4.0**。

**任何 docker 构建、部署、运行操作之前，必须先运行 `./check-docker-env.sh` 校验环境。**
版本不符时，先安装锁定版本（Debian/Ubuntu）：

```bash
# 1. 安装 Docker 官方源版本
curl -fsSL https://get.docker.com | sh
# 2. 固定到标准版本
apt-get install -y docker-ce=5:29.7.2* docker-ce-cli=5:29.7.2* docker-compose-plugin=5.4.0*
# 3. 锁定，防止 apt 升级
apt-mark hold docker-ce docker-ce-cli docker-ce-rootless-extras docker-buildx-plugin docker-compose-plugin containerd.io
# 4. 校验
./check-docker-env.sh
```

**环境未达标时禁止执行任何 docker 操作。**

## 架构概览

```
cmd/litepan/main.go -> config.Load -> logx -> app.New
app.New: prepareDataDirs -> openStore(Migrate) -> wireCore(drivers/auth/cache) -> wireServices(file/playback/fuse/upload) -> wireHTTP(chi + davBypass + embed web)
internal/domain 纯 DTO, internal/driver 接口隔离, drivers/* 仅 115_Open + 189Cloud 注册
```

- `domain` 不得 `import internal/*`（depguard `domain-pure`）
- `api` 不得 `import store`，`drivers` 不得 `import file/auth/upload`
- 前端 `web/` Vue 3.5 + Vite 8 + Pinia，`outDir ../internal/api/web` 直嵌 `embed.FS`，预压缩 `.gz`

## 环境变量

| 变量 | 默认 | 说明 |
|------|------|------|
| `LITEPAN_DATA_DIR` | `./data` | 数据目录，含 `litepan.db` + `secret.key`（0600） |
| `LITEPAN_DB_PATH` | `$DATA_DIR/litepan.db` | 覆盖 DB 路径 |
| `LITEPAN_LISTEN` | `:5211` | HTTP 监听 |
| `LITEPAN_LOG_LEVEL` | `info` | debug/info/warn/error |
| `LITEPAN_LOCAL_SOURCES` | `{}` JSON | 本地源映射 `{"名":"/host/path"}` |
| `LITEPAN_SECRET_KEY` | 自动生成 32B hex | 会话 HMAC，需 >=32 字符 |
| `LITEPAN_CORS_ORIGINS` | dev 4 项 | 逗号分隔白名单 |

## 本地开发

```bash
# 前端
cd web && npm ci --include=dev && npm run type-check && npm run build
# 后端（需 Go 1.27.0）
GOWORK=off go test -race ./...
GOWORK=off golangci-lint run -c .golangci.yml ./...
# 构建
make web-build && make build
# Docker（先校验）
./check-docker-env.sh && make docker-build
```

## 安全约定

- 容器以 `litepan` 非 root 运行，`cap_add SYS_ADMIN` 替代 `privileged`
- 默认 `admin/admin` 强制改密，会话 2h（remember 30d）
- `litepan.db` 明文含 token，文件 0600，备份需加密
- `RequestOriginAllowed` 空 Origin 拒绝，写接口需同源或白名单
