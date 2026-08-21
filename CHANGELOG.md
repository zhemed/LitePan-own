# Changelog

所有重要变更记录于此，遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 与 [Semantic Versioning](https://semver.org/lang/zh-CN/)。

## [0.0.1] - 2026-08-20

### 特性
- 自维护稳定版，基于 LitePan 专注备份/挂载
- 本地源映射上传（零落盘，支持 115GB+ 超大文件）
- 115 云盘 512MB 大分片、天翼 10MB 分片（官方上限）
- WebDAV 统一挂载（115/天翼）
- fnOS 备份源自动发现、`ProtectedPaths` 强制删除保护
- 内置离线下载（BT/磁力 via anacrolix/torrent）

### 修复
- 天翼 10MB 分片上限实测，≥20MB 被拒回退
- WebDAV 上传路径修复
- 115/天翼 上传验证通过

### 驱动
- 注册：`115_Open`、`189Cloud`；其余 `123_Open/139Cloud/Baidu_Open/Guangya/OneDrive/OpenList/Quark/WebDAV/template` 代码归档

### 工程
- Go 1.27.0 + `GOTOOLCHAIN=local` + `modernc/sqlite` 纯 Go
- 前端 Vue 3.5 + Vite 8 + `embed.FS` 预压缩
- Docker 多阶段 `node:20→golang:1.27→debian fuse3`

## [Unreleased] - 2026-08-21 审计加固

### 安全
- 容器非 root `litepan` + `cap_add SYS_ADMIN` + `HEALTHCHECK`
- 会话 10年→2h/30d、`admin/admin` 强制改密、`public_index` 默认关闭
- 登录 5次/15min 限流、`secret.key` 弱密钥拒绝、`DB` 0600
- CSRF 空 Origin 拒绝、CORS 白名单、`X-Frame-Options/DENY` 等安全头
- `store` 白名单防 `PRAGMA` 注入、`splitStatements` 引号处理

### 构建
- 前端 `xlsx` CDN→npm、`dompurify` `pdfjs` 高危修复、`TS 5.8.3`
- `vite` 去 `three-vendor` 增 `preview-vendor`、`Makefile` 对齐 `-trimpath -ldflags`

### 文档/CI
- `README` 许可 AGPL→PolyForm、`AGENTS.md` 架构/环境变量、`ci.yml` 前后端 lint/test/audit
