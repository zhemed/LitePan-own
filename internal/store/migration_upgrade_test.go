package store

import (
	"context"
	"testing"
)

func TestMigrateV047ToBuiltinOfflineSchema(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, Options{Memory: true})
	if err != nil {
		t.Fatalf("打开测试数据库: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.write.ExecContext(ctx, `CREATE TABLE schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		t.Fatalf("创建迁移记录表: %v", err)
	}

	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("加载迁移: %v", err)
	}
	for _, item := range migrations {
		if item.version > 15 {
			continue
		}
		applyMigrationForTest(t, ctx, db, item)
	}

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("从 v0.4.7 升级: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("重复执行迁移: %v", err)
	}

	offlineColumns, err := tableColumns(ctx, db, "offline_download_tasks")
	if err != nil {
		t.Fatalf("读取离线任务列: %v", err)
	}
	for _, name := range []string{
		"provider_kind",
		"executor_type",
		"phase",
		"downloaded_bytes",
		"speed_bytes",
		"local_temp_path",
		"magnet_diagnostics_json",
	} {
		if !offlineColumns[name] {
			t.Errorf("缺少 offline_download_tasks.%s", name)
		}
	}
	for _, name := range []string{
		"resume_meta_json",
		"upload_task_id",
		"upload_task_ids_json",
		"cleanup_status",
	} {
		if offlineColumns[name] {
			t.Errorf("不应保留未发布的中间列 offline_download_tasks.%s", name)
		}
	}

	uploadColumns, err := tableColumns(ctx, db, "upload_tasks")
	if err != nil {
		t.Fatalf("读取上传任务列: %v", err)
	}
	for _, name := range []string{"cleanup_local_mode", "cleanup_local_path"} {
		if !uploadColumns[name] {
			t.Errorf("缺少 upload_tasks.%s", name)
		}
	}

	applied, err := appliedVersions(ctx, db)
	if err != nil {
		t.Fatalf("读取迁移版本: %v", err)
	}
	if !applied[16] {
		t.Error("未记录最终版 0016 迁移")
	}
	for version := 17; version <= 20; version++ {
		if applied[version] {
			t.Errorf("不应存在未发布中间迁移 %04d", version)
		}
	}
}

func applyMigrationForTest(t *testing.T, ctx context.Context, db *DB, item migration) {
	t.Helper()
	tx, err := db.write.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("开始迁移 %s: %v", item.name, err)
	}
	for _, statement := range splitStatements(item.sql) {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			_ = tx.Rollback()
			t.Fatalf("执行迁移 %s: %v", item.name, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES (?)`, item.version); err != nil {
		_ = tx.Rollback()
		t.Fatalf("记录迁移 %s: %v", item.name, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("提交迁移 %s: %v", item.name, err)
	}
}
