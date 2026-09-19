package risk

import "testing"

// SetManager 发布后 GetManager 必须返回同一实例（重启后配置值不丢的回归）。
func TestSetManagerPublishesGlobalInstance(t *testing.T) {
	orig := GetManager()
	t.Cleanup(func() { SetManager(orig) })

	cfg := DefaultManagerConfig()
	cfg.MaxPositionPct = 77
	custom := NewManager(cfg)
	SetManager(custom)

	if GetManager() != custom {
		t.Fatal("GetManager must return the published instance")
	}
	if got := GetManager().Config().MaxPositionPct; got != 77 {
		t.Fatalf("MaxPositionPct = %v, want 77", got)
	}
}
