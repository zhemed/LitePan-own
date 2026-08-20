#!/bin/bash
# Docker 环境标准检查：Docker 29.7.2 + Compose v5.4.0
# 不符合标准版本时拒绝继续（退出码 1）。
set -euo pipefail

REQUIRED_DOCKER="29.7.2"
REQUIRED_COMPOSE="v5.4.0"

DOCKER_VER=$(docker --version 2>/dev/null | grep -oP "Docker version \K[0-9.]+" || echo "missing")
COMPOSE_VER=$(docker compose version 2>/dev/null | grep -oP "Docker Compose version \K\S+" || echo "missing")

install_guide() {
  echo
  echo "安装锁定版本（Debian/Ubuntu）："
  echo "  curl -fsSL https://get.docker.com | sh"
  echo "  apt-get install -y docker-ce=5:29.7.2* docker-ce-cli=5:29.7.2* docker-compose-plugin=5.4.0*"
  echo "  apt-mark hold docker-ce docker-ce-cli docker-ce-rootless-extras docker-buildx-plugin docker-compose-plugin containerd.io"
}

ok=1
if [ "$DOCKER_VER" != "$REQUIRED_DOCKER" ]; then
  echo "错误: Docker 版本不符合标准（期望 $REQUIRED_DOCKER，实际 $DOCKER_VER）"
  ok=0
fi
if [ "$COMPOSE_VER" != "$REQUIRED_COMPOSE" ]; then
  echo "错误: Compose 版本不符合标准（期望 $REQUIRED_COMPOSE，实际 $COMPOSE_VER）"
  ok=0
fi
if [ $ok -eq 1 ]; then
  echo "通过: Docker $DOCKER_VER + Compose $COMPOSE_VER 符合标准"
else
  install_guide
fi
exit $((1-ok))
