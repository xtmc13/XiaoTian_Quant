package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveConfigCreatesMissingDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	oldPath := configPath
	oldCache := configCache
	defer func() {
		configPath = oldPath
		configCache = oldCache
	}()

	// Target a nested directory that does not exist.
	configPath = filepath.Join(tmpDir, "missing", "nested", "config.yaml")

	cfg := map[string]any{
		"default_exchange": "binance",
		"exchanges": map[string]any{
			"binance": map[string]any{
				"enabled": true,
				"testnet": true,
			},
		},
	}

	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig should create missing directory: %v", err)
	}

	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config file was not created: %v", err)
	}
}

func TestWriteConfigCacheLockedCreatesMissingDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	oldPath := configPath
	oldCache := configCache
	defer func() {
		configPath = oldPath
		configCache = oldCache
	}()

	configPath = filepath.Join(tmpDir, "another", "missing", "config.yaml")
	configCache = map[string]any{"version": "1.0.0"}

	configMu.Lock()
	err := writeConfigCacheLocked()
	configMu.Unlock()
	if err != nil {
		t.Fatalf("writeConfigCacheLocked should create missing directory: %v", err)
	}

	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config file was not created: %v", err)
	}
}
