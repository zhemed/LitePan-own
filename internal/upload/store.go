package upload

import (
	"os"
	"path/filepath"
	"strings"
)

// removeLocalFile 删除本地文件——带强制保护：
// 只允许删除 upload_tasks 数据目录内的临时文件；任何其他路径（如
// 飞牛备份的本地源文件）一律拒绝删除，防止配置错误误删用户数据。
func (m *Manager) removeLocalFile(path string) {
	if path == "" {
		return
	}
	if m.isProtectedDeletionPath(path) {
		m.log.Warn("拒绝删除受保护路径文件（强制保护：本地源）", "path", path)
		return
	}
	_ = os.Remove(path)
}

// isProtectedDeletionPath 校验路径是否位于受保护路径（本地源映射）之下。
// 飞牛备份源（如 /vol1/1000/临时-1/...）在 protectedPaths 中——无论清理
// 模式如何配置，这些路径下的文件都绝不会被删除。
func (m *Manager) isProtectedDeletionPath(path string) bool {
	if len(m.protectedPaths) == 0 {
		return false
	}
	cleanPath := filepath.Clean(path)
	for _, root := range m.protectedPaths {
		cleanRoot := filepath.Clean(root)
		if cleanRoot == "" {
			continue
		}
		if cleanPath == cleanRoot || strings.HasPrefix(cleanPath, cleanRoot+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func (m *Manager) cleanupLocalSource(localPath, cleanupPath, mode string) {
	switch mode {
	case CleanupLocalFileOnSuccess:
		m.removeLocalFile(localPath)
	case CleanupLocalPathOnSuccess:
		path := strings.TrimSpace(cleanupPath)
		if path == "" {
			path = localPath
		}
		if path == "" {
			return
		}
		if m.isProtectedDeletionPath(path) {
			m.log.Warn("拒绝删除受保护路径文件（强制保护：本地源）", "path", path)
			return
		}
		_ = os.RemoveAll(path)
	case CleanupLocalTreeOnSuccess:
		m.removeLocalFile(localPath)
		removeEmptyParentDirs(m, localPath, cleanupPath)
	case CleanupLocalAlways:
		m.removeLocalFile(localPath)
	default:
		return
	}
}

// removeEmptyParentDirs 只在 root 内向上回收空目录，避免一个文件完成时误删
// 同批次仍在上传或失败保留的其它文件。
func removeEmptyParentDirs(m *Manager, localPath, root string) {
	localPath = filepath.Clean(strings.TrimSpace(localPath))
	root = filepath.Clean(strings.TrimSpace(root))
	if localPath == "" || localPath == "." || root == "" || root == "." {
		return
	}
	rel, err := filepath.Rel(root, localPath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return
	}
	for dir := filepath.Dir(localPath); ; dir = filepath.Dir(dir) {
		rel, err := filepath.Rel(root, dir)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return
		}
		if m != nil && m.isProtectedDeletionPath(dir) {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		if dir == root {
			return
		}
	}
}
