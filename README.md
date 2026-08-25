# LitePan-own

> **Fork 自 [Ponphil/LitePan](https://github.com/Ponphil/LitePan) `v0.5.1-beta`，自用版 — 新增「本地自动上传」**

`99` 闭环已验，`11` 生产待推。本仓库在上游基础上**只加一个功能**：把飞牛上 3 个本地目录（`我的文件/杂物间/pve_backup`）按 `daily 02:00` 全量 `hash` 增量自动推到天翼云盘，改过才重传，同名不同目录也能分清。

<div align="center">

![LitePan-own](docs/pictures/banner.png)

[![](https://img.shields.io/badge/Docker-ghcr.io%2Fzhemed%2Flitepan--own-2496ED?logo=docker)](https://github.com/zhemed/LitePan-own)
[![License](https://img.shields.io/badge/License-PolyForm%20NC-red)](
./LICENSE)

</div>

> [!NOTE]
> 上游 `Ponphil/LitePan` 是 Go 版 LitePan，`latest` 仍是 Python 旧版。本 Fork 仅为自用，不接受外部 PR。

---

## ▎ 本 Fork 新增

| 功能 | 说明 | 关键实现 |
|---|---|---|
| **本地自动上传** | 自动化里新增 `本地上传` 动作，选 `映射 + 网盘 + 目标目录` 即可 | `internal/domain/automation.go` `AutomationActionLocalUpload` |
| **全量 hash 增量** | `relPath → sha256` 存 `local_upload_state_<mapping>.json`，`hash` 没变秒跳过，`115G` 也 `4分钟` 扫完，不会 `6小时` 超时 | `internal/automation/service_run.go` `fileHash` + `load/saveState` |
| **重复文件名** | 按 `a/1.mp4` `b/1.mp4` 的 `relPath` 分开记，同名不同目录不串 | 同上 |
| **前端** | 自动化面板可直接选 `本地上传`，不用 `curl` | `web/src/components/admin/AutomationPanel.vue` |

**触发器** 复用现有 `daily 02:00` / `interval`，**不改** 上传引擎（仍是 `upload.SourceTypeServerLocal`）。

---

## ▎ 快速开始（自用）

**1. 拉代码编镜像（已推 `ghcr.io` 的可直接拉）**
```bash
git clone https://github.com/zhemed/LitePan-own.git
cd LitePan-own
docker build -t ghcr.io/zhemed/litepan-own:beta .
# 或直接拉
docker pull ghcr.io/zhemed/litepan-own:beta
```

**2. `docker-compose.yml`（3 个映射 `ro` 同飞牛）**
```yaml
services:
  litepan:
    image: ghcr.io/zhemed/litepan-own:beta
    container_name: litepan
    restart: unless-stopped
    ports: ["5211:5211","42069:42069/tcp","42069:42069/udp"]
    environment: [TZ=Asia/Shanghai]
    volumes:
      - ./data:/app/data
      - ./strm:/app/strm
      - ./mounts:/app/mounts:shared
      - /vol1/1000/我的文件:/vol1/1000/我的文件:ro
      - /vol2/1000/杂物间:/vol2/1000/杂物间:ro
      - /vol3/1000/pve_backup:/vol3/1000/pve_backup:ro
    devices: [/dev/fuse:/dev/fuse]
    pid: "host"
    privileged: true
```

**3. 配自动化**
* `http://飞牛IP:5211` → `存储管理` 加 `天翼云盘`
* `工具箱 → 本地上传` 确认 3 个映射在
* `任务管理 → 自动化 → 新增联动` → `当 每天 02:00` → `就执行 本地上传` 选 `pve_backup → 天翼云盘 /`，重复 3 条对应 3 个映射
* 点 `▶ 立即执行` 试一次，`运行记录` 看 `已创建 N` / `增量跳过 N`

---

## ▎ 与上游同步

```bash
git remote add upstream https://github.com/Ponphil/LitePan.git
git fetch upstream
git merge upstream/main  # 有冲突先解 drivers/all.go 的 115
```

---

## ▎ 许可

[PolyForm Noncommercial 1.0.0](./LICENSE) 同上游。第三方见 [THIRD_PARTY_NOTICES.md](./THIRD_PARTY_NOTICES.md)。

*本 Fork 仅自用，已在 `10.0.0.99` 闭环（`100M` 秒传，`2G` 全量 hash 2秒，增量 0秒跳过），`10.0.0.11` 生产待推。*
