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
