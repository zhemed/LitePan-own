package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultLocalStrmBesideCWD(t *testing.T) {
	t.Setenv("LITEPAN_DATA_DIR", "")
	t.Setenv("LITEPAN_STRM_DIR", "")
	_ = os.Unsetenv("LITEPAN_DATA_DIR")
	_ = os.Unsetenv("LITEPAN_STRM_DIR")
	cfg := Load()
	if cfg.DataDir != "./data" {
		t.Fatalf("got data=%q", cfg.DataDir)
	}
}
