package upload

import (
	"os"
	"log/slog"
	"testing"
)


func TestIsProtectedDeletionPath(t *testing.T) {
	m := &Manager{protectedPaths: []string{"/vol1/1000/临时-1", "/vol2/1000/spare_backup"}, log: slog.Default()}
	// 本地源（飞牛备份源）必须拒绝
	if !m.isProtectedDeletionPath("/vol1/1000/临时-1/01.zip") {
		t.Fatal("本地源文件必须拒绝删除")
	}
	if !m.isProtectedDeletionPath("/vol1/1000/临时-1/sub/x.bin") {
		t.Fatal("本地源子目录必须拒绝删除")
	}
	if !m.isProtectedDeletionPath("/vol2/1000/spare_backup/x.bin") {
		t.Fatal("本地源文件必须拒绝删除")
	}
	// 非受保护路径允许清理
	if m.isProtectedDeletionPath("/app/data/upload_tasks/webdav_abc.bin") {
		t.Fatal("临时文件应允许清理")
	}
	if m.isProtectedDeletionPath("/tmp/anything.bin") {
		t.Fatal("普通临时路径应允许清理")
	}
	// 无受保护路径时全部允许
	m2 := &Manager{log: slog.Default()}
	if m2.isProtectedDeletionPath("/vol1/1000/临时-1/01.zip") {
		t.Fatal("未配置受保护路径时应允许清理")
	}
}

func TestRemoveLocalFileProtection(t *testing.T) {
	m := &Manager{protectedPaths: []string{"/tmp/lp-protect-test"}, log: slog.Default()}
	// 受保护路径——即使误设清理模式也不能被删
	os.MkdirAll("/tmp/lp-protect-test", 0o755)
	src := "/tmp/lp-protect-test/important.bin"
	os.WriteFile(src, []byte("data"), 0o644)
	m.removeLocalFile(src)
	if _, err := os.Stat(src); err != nil {
		t.Fatal("受保护路径被误删！")
	}
	os.RemoveAll("/tmp/lp-protect-test")
}
