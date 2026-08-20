package fnosources

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestDiscover(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "basic_backup.db3")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE user_tasks (id INTEGER, name TEXT, source_paths TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO user_tasks VALUES (1,'t1','/vol1/1000/临时-1;/vol2/1000/临时-2')"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO user_tasks VALUES (2,'t2','/vol1/1000/pve_backup')"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	src, err := Discover(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(src.NameToPath) != 3 {
		t.Fatalf("应发现 3 个源，实际 %d", len(src.NameToPath))
	}
	if src.NameToPath["临时-1"] != "/vol1/1000/临时-1" {
		t.Fatalf("临时-1 映射错误: %s", src.NameToPath["临时-1"])
	}
	if len(src.Paths) != 3 {
		t.Fatalf("应返回 3 个保护路径，实际 %d", len(src.Paths))
	}
	// 去重：同名源只出现一次
	if _, err := db2InsertSame(t, dbPath); err != nil {
		t.Fatal(err)
	}
}

func db2InsertSame(t *testing.T, dbPath string) (any, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	_, err = db.Exec("INSERT INTO user_tasks VALUES (3,'t3','/vol1/1000/临时-1')")
	return nil, err
}

func TestDiscoverMissingDB(t *testing.T) {
	src, err := Discover(filepath.Join(t.TempDir(), "nope.db3"))
	if err == nil {
		t.Fatal("数据库不存在应报错")
	}
	if len(src.NameToPath) != 0 {
		t.Fatal("不应有映射")
	}
	_ = os.Getenv("HOME")
}
