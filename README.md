# LitePan-own

基于 LitePan 的自维护稳定版，专注网盘备份与挂载（**v0.0.1**）。

## 特性

- **本地源映射上传**：飞牛备份文件直接读取本地源，一次传输、零落盘，支持超大文件（115GB+）
- **115 云盘**：512MB 大分片上传，稳定高效
- **天翼云盘**：10MB 分片上传（官方上限，实测 20MB 及以上均被拒）
- **WebDAV 挂载**：天翼 / 115 等网盘统一 WebDAV 访问
- **fnOS 备份源自动发现**：自动识别飞牛备份目录
- **强制删除保护**：映射目录防误删（ProtectedPaths）
- **离线下载**：内置 BT / 磁力下载

## 部署

> **前置**：本项目 Docker 环境固定为 `Docker 29.7.2 + Compose v5.4.0`，任何构建/运行前先校验：
> ```bash
> ./check-docker-env.sh  # 通过才继续
> ```

### 方式一：Compose 一键部署（推荐，含持久化与 FUSE）

```bash
git clone https://github.com/zhemed/LitePan-own.git
cd LitePan-own
./check-docker-env.sh
mkdir -p ./data ./mounts && sudo chown -R 1000:1000 ./data ./mounts
docker compose up -d --build
# 日志与健康
docker logs -f litepan
curl http://127.0.0.1:5211/api/health
```

### 方式二：GHCR 拉取镜像（fnOS/生产）

```bash
# 镜像由 CI 在 tag 推送后自动发布到 GHCR，若本地已改动请先 docker build
docker pull ghcr.io/zhemed/litepan-own:0.0.1
docker run -d --name litepan --restart unless-stopped --network host \
  --cap-add SYS_ADMIN --device /dev/fuse --security-opt apparmor:unconfined \
  -e TZ=Asia/Shanghai \
  -e LITEPAN_LISTEN=:5211 \
  -e "LITEPAN_LOCAL_SOURCES={\"临时-1\":\"/vol1/1000/临时-1\"}" \
  -v /vol1/1000:/vol1/1000 \
  -v /vol1/1000/litepan/data:/app/data \
  -v /vol1/1000/litepan/mounts:/app/mounts:shared \
  ghcr.io/zhemed/litepan-own:0.0.1
```

### 方式三：源码构建单容器

```bash
git clone https://github.com/zhemed/LitePan-own.git
cd LitePan-own
./check-docker-env.sh
docker build -t litepan-own .
docker run -d --name litepan --restart unless-stopped \
  -p 5211:5211 -p 42069:42069 -p 42069:42069/udp \
  --cap-add SYS_ADMIN --device /dev/fuse --security-opt apparmor:unconfined \
  -e TZ=Asia/Shanghai \
  -v ./data:/app/data -v ./mounts:/app/mounts:shared \
  litepan-own
```

部署完成后访问 `http://<主机>:5211`，`/api/health` 应返回 `{"status":"ok"}`，容器以 `litepan` 非 root 运行（`docker exec litepan id` → `uid=1000`）。

## 版本

- **v0.0.1**（稳定）：天翼 10MB 分片 + WebDAV 上传修复，天翼/115 上传验证通过

## 支持驱动

当前注册：**115 云盘**、**天翼云盘（189）**（其余驱动代码已归档，需要时可恢复）

## 许可

PolyForm Noncommercial 1.0.0 — 仅允许非商业/个人/研究使用，商业使用需另行授权。详见 [`LICENSE`](LICENSE) 与 [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md)。
