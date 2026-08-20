package upload

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// TestNewManagerInitializesProtectedPaths 验证 NewManager 从环境变量初始化保护路径。
func TestNewManagerInitializesProtectedPaths(t *testing.T) {
	srcDir := t.TempDir()
	t.Setenv("LITEPAN_LOCAL_SOURCES", `{"测试源":"`+srcDir+`"}`)
	m := NewManager(Options{DataDir: t.TempDir(), Log: slog.Default()})
	if len(m.protectedPaths) == 0 {
		t.Fatal("❌ 保护路径未初始化——NewManager 必须从 env 收集 protectedPaths")
	}
	found := false
	for _, p := range m.protectedPaths {
		if filepath.Clean(p) == filepath.Clean(srcDir) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("❌ protectedPaths 未包含 env 配置的本地源路径")
	}
	t.Log("✅ NewManager 保护路径初始化正常")
}

// TestDeleteRecordKeepsProtectedSource 验证删除上传记录（未勾选删除文件）不会删受保护源文件。
func TestDeleteRecordKeepsProtectedSource(t *testing.T) {
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "skills.zip")
	if err := os.WriteFile(srcFile, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &Manager{protectedPaths: []string{srcDir}, log: slog.Default()}
	// 模拟删除记录（mode 空 → removeLocalFile）
	m.removeLocalFile(srcFile)
	if _, err := os.Stat(srcFile); err != nil {
		t.Fatal("❌ 删除记录导致受保护源文件被删！")
	}
	t.Log("✅ 删除记录不会删受保护源文件")
}

// TestPathOnSuccessProtected 验证 path_on_success 清理不会删受保护路径。
func TestPathOnSuccessProtected(t *testing.T) {
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "vzdump.bin")
	if err := os.WriteFile(srcFile, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &Manager{protectedPaths: []string{srcDir}, log: slog.Default()}
	m.cleanupLocalSource(srcFile, srcFile, CleanupLocalPathOnSuccess)
	if _, err := os.Stat(srcFile); err != nil {
		t.Fatal("❌ path_on_success 绕过保护删除了受保护文件！")
	}
	t.Log("✅ path_on_success 不再绕过保护")
}
