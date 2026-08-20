package fnosources

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// DefaultDBPath 飞牛备份任务的数据库路径（LitePan 与飞牛同机时可读）。
const DefaultDBPath = "/usr/trim/var/backup_service/basic_backup.db3"

// Sources 飞牛备份源信息。
type Sources struct {
	// NameToPath 目录名 → 本地路径（WebDAV 第一段目录名映射）。
	NameToPath map[string]string
	// Paths 所有源路径（受保护——清理逻辑绝不删除）。
	Paths []string
}

// Discover 读取飞牛备份任务数据库，自动提取备份源路径。
// 返回的映射：WebDAV 第一段目录名 → 本地源路径（例如 "临时-1" → /vol1/1000/临时-1）。
// 同时返回所有源路径列表（用于强制删除保护）。
func Discover(dbPath string) (*Sources, error) {
	out := &Sources{NameToPath: map[string]string{}}
	if strings.TrimSpace(dbPath) == "" {
		dbPath = DefaultDBPath
	}
	if _, err := os.Stat(dbPath); err != nil {
		return out, fmt.Errorf("飞牛备份数据库不存在: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query("SELECT source_paths FROM user_tasks")
	if err != nil {
		return out, fmt.Errorf("读取 user_tasks 失败: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			continue
		}
		for _, p := range strings.Split(raw, ";") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			clean := filepath.Clean(p)
			name := filepath.Base(clean)
			if name == "" || name == "." || name == string(filepath.Separator) {
				continue
			}
			if _, dup := out.NameToPath[name]; !dup {
				out.NameToPath[name] = clean
				out.Paths = append(out.Paths, clean)
			}
		}
	}
	return out, nil
}
