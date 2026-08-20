package upload

import (
	"encoding/json"
	"context"
	"os"
	"litepan/internal/fnosources"
	"strings"
	"path/filepath"
	"sync"
	"time"
)

const (
	TempCleanupInterval = time.Hour
	TempMaxAge          = 24 * time.Hour
)

func TempDir(dataDir string) string {
	return filepath.Join(dataDir, "upload_tasks")
}

type TempRegistry struct {
	mu    sync.RWMutex
	paths map[string]struct{}
}

func NewTempRegistry() *TempRegistry {
	return &TempRegistry{paths: make(map[string]struct{})}
}

func (r *TempRegistry) Track(path string) func() {
	path = filepath.Clean(path)
	if path == "" {
		return func() {}
	}
	r.mu.Lock()
	r.paths[path] = struct{}{}
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		delete(r.paths, path)
		r.mu.Unlock()
	}
}

func (r *TempRegistry) Snapshot() map[string]struct{} {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]struct{}, len(r.paths))
	for p := range r.paths {
		out[p] = struct{}{}
	}
	return out
}

func CleanupTempDir(dir string, active map[string]struct{}, maxAge time.Duration) (int, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	now := time.Now()
	deleted := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Clean(filepath.Join(dir, e.Name()))
		if active != nil {
			if _, ok := active[path]; ok {
				continue
			}
		}
		if maxAge > 0 {
			info, err := e.Info()
			if err != nil {
				continue
			}
			if now.Sub(info.ModTime()) < maxAge {
				continue
			}
		}
		if err := os.Remove(path); err == nil {
			deleted++
		}
	}
	return deleted, nil
}

func (m *Manager) activeTempPaths() map[string]struct{} {
	active := make(map[string]struct{})
	m.mu.Lock()
	for _, st := range m.tasks {
		if st.localPath == "" {
			continue
		}
		active[filepath.Clean(st.localPath)] = struct{}{}
	}
	m.mu.Unlock()
	if m.tempRegistry != nil {
		for p := range m.tempRegistry.Snapshot() {
			active[p] = struct{}{}
		}
	}
	return active
}

func (m *Manager) CleanupOrphanTempFiles(maxAge time.Duration) (int, error) {
	active := m.activeTempPaths()
	return CleanupTempDir(m.TempDir(), active, maxAge)
}

func (m *Manager) TempRegistry() *TempRegistry {
	return m.tempRegistry
}

func (m *Manager) StartTempCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(TempCleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				n, err := m.CleanupOrphanTempFiles(TempMaxAge)
				if err != nil {
					m.log.Warn("upload temp cleanup failed", "err", err)
					continue
				}
				if n > 0 {
					m.log.Info("upload temp cleanup done", "deleted", n)
				}
			}
		}
	}()
}

func (m *Manager) initTempCleanup() {
	if n, err := m.CleanupOrphanTempFiles(0); err != nil {
		m.log.Warn("upload temp startup cleanup failed", "err", err)
	} else if n > 0 {
		m.log.Info("upload temp startup cleanup done", "deleted", n)
	}
}

// protectedPathsFromOptions 收集强制保护路径：
// 显式 Options.ProtectedPaths + LITEPAN_LOCAL_SOURCES 环境变量中的本地源路径。
// 这些路径下的文件（如飞牛备份源）绝不允许被清理逻辑删除。
func protectedPathsFromOptions(opts Options) []string {
	var paths []string
	paths = append(paths, opts.ProtectedPaths...)
	if raw := strings.TrimSpace(os.Getenv("LITEPAN_LOCAL_SOURCES")); raw != "" {
		var m map[string]string
		if err := json.Unmarshal([]byte(raw), &m); err == nil {
			for _, v := range m {
				paths = append(paths, strings.TrimSpace(v))
			}
		}
	}
	// 自动发现：同机飞牛备份任务源路径（新目录自动纳入保护）。
	if fn, err := fnosources.Discover(fnosources.DefaultDBPath); err == nil {
		for _, v := range fn.Paths {
			found := false
			for _, exist := range paths {
				if exist == v {
					found = true
					break
				}
			}
			if !found {
				paths = append(paths, v)
			}
		}
	}
	return paths
}
