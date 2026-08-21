package config

import (
	"os"
	"testing"
)

func TestDefaultDataDir(t *testing.T) {
	t.Setenv("LITEPAN_DATA_DIR", "")
	_ = os.Unsetenv("LITEPAN_DATA_DIR")
	cfg := Load()
	if cfg.DataDir != "./data" {
		t.Fatalf("got data=%q", cfg.DataDir)
	}
}
