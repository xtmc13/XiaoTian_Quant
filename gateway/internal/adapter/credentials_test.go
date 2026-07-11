package adapter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/store"
)

func TestGetCredentialReadsFromConfigFallback(t *testing.T) {
	// Ensure no env vars interfere.
	for _, key := range []string{"GATE_API_KEY", "GATE_API_SECRET", "GATE_PASSPHRASE"} {
		os.Unsetenv(key)
	}

	tmpDir := t.TempDir()
	oldPath := store.GetConfigPath()
	store.SetConfigPath(filepath.Join(tmpDir, "config.yaml"))
	defer store.SetConfigPath(oldPath)

	store.SaveConfig(map[string]any{
		"exchanges": map[string]any{
			"gate": map[string]any{
				"api_key":    "cfg-key",
				"secret":     "cfg-secret",
				"passphrase": "cfg-pass",
			},
		},
	})

	apiKey, secret, passphrase := GetCredential("gate")
	if apiKey != "cfg-key" || secret != "cfg-secret" || passphrase != "cfg-pass" {
		t.Fatalf("expected credentials from config, got key=%q secret=%q passphrase=%q", apiKey, secret, passphrase)
	}
}

func TestGetCredentialEnvOverridesConfig(t *testing.T) {
	t.Setenv("GATE_API_KEY", "env-key")
	t.Setenv("GATE_API_SECRET", "env-secret")

	tmpDir := t.TempDir()
	oldPath := store.GetConfigPath()
	store.SetConfigPath(filepath.Join(tmpDir, "config.yaml"))
	defer store.SetConfigPath(oldPath)

	store.SaveConfig(map[string]any{
		"exchanges": map[string]any{
			"gate": map[string]any{
				"api_key": "cfg-key",
				"secret":  "cfg-secret",
			},
		},
	})

	apiKey, secret, _ := GetCredential("gate")
	if apiKey != "env-key" || secret != "env-secret" {
		t.Fatalf("env should override config, got key=%q secret=%q", apiKey, secret)
	}
}

func TestGetCredentialGateAlias(t *testing.T) {
	for _, key := range []string{"GATE_API_KEY", "GATE_API_SECRET"} {
		os.Unsetenv(key)
	}

	tmpDir := t.TempDir()
	oldPath := store.GetConfigPath()
	store.SetConfigPath(filepath.Join(tmpDir, "config.yaml"))
	defer store.SetConfigPath(oldPath)

	store.SaveConfig(map[string]any{
		"exchanges": map[string]any{
			"gate": map[string]any{
				"api_key": "cfg-key",
				"secret":  "cfg-secret",
			},
		},
	})

	apiKey, secret, _ := GetCredential("gateio")
	if apiKey != "cfg-key" || secret != "cfg-secret" {
		t.Fatalf("gateio alias should resolve to gate config, got key=%q secret=%q", apiKey, secret)
	}
}

func TestHasCredentialTrueWhenConfigured(t *testing.T) {
	for _, key := range []string{"GATE_API_KEY", "GATE_API_SECRET"} {
		os.Unsetenv(key)
	}

	tmpDir := t.TempDir()
	oldPath := store.GetConfigPath()
	store.SetConfigPath(filepath.Join(tmpDir, "config.yaml"))
	defer store.SetConfigPath(oldPath)

	store.SaveConfig(map[string]any{
		"exchanges": map[string]any{
			"gate": map[string]any{
				"api_key": "cfg-key",
				"secret":  "cfg-secret",
			},
		},
	})

	if !HasCredential("gate") {
		t.Fatal("HasCredential should return true when config has credentials")
	}
	if !HasCredential("gateio") {
		t.Fatal("HasCredential should return true for gateio alias")
	}
}
