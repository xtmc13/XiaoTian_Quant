package ml_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/ml"
)

// testExportedModel 构造一个合法导出模型（单树桩 + 指定叶值）。
func testExportedModel(modelID string, leftLeaf, rightLeaf float64) *ml.ExportedModel {
	return &ml.ExportedModel{
		Success:      true,
		ModelID:      modelID,
		ModelType:    "lightgbm",
		TaskType:     "regression",
		FeatureNames: []string{"return_5"},
		Trees: []ml.TreeNode{{
			Feature:   "return_5",
			Threshold: 0,
			Left:      &ml.TreeNode{Leaf: &leftLeaf},
			Right:     &ml.TreeNode{Leaf: &rightLeaf},
		}},
	}
}

func TestModelRegistryHotLoadAndPersist(t *testing.T) {
	dir := t.TempDir()
	reg := ml.NewModelRegistry(dir)

	info, err := reg.HotLoad("m1", testExportedModel("m1", -0.001, 0.002))
	if err != nil {
		t.Fatalf("hotload: %v", err)
	}
	if info.Version == "" || info.Trees != 1 || info.Features != 1 {
		t.Fatalf("bad info: %+v", info)
	}

	p, ok := reg.Get("m1")
	if !ok || !p.IsLoaded() {
		t.Fatalf("predictor not loaded")
	}
	pred, err := p.PredictFromMap(map[string]float64{"return_5": 0.01})
	if err != nil || pred != 0.002 {
		t.Fatalf("predict: %v err=%v", pred, err)
	}

	// 落盘文件存在且是合法导出 JSON
	if _, err := os.Stat(filepath.Join(dir, "m1.json")); err != nil {
		t.Fatalf("persist file: %v", err)
	}

	// 重启恢复
	reg2 := ml.NewModelRegistry(dir)
	if n := reg2.LoadFromDir(); n != 1 {
		t.Fatalf("restore: %d", n)
	}
	info2, ok := reg2.Info("m1")
	if !ok || info2.Version != info.Version {
		t.Fatalf("restored version mismatch: %+v vs %+v", info2, info)
	}
}

func TestModelRegistryHotLoadAtomicity(t *testing.T) {
	dir := t.TempDir()
	reg := ml.NewModelRegistry(dir)

	if _, err := reg.HotLoad("m1", testExportedModel("m1", -0.001, 0.001)); err != nil {
		t.Fatalf("seed hotload: %v", err)
	}
	// 坏模型（无树）不得覆盖旧模型
	if _, err := reg.HotLoad("m1", &ml.ExportedModel{Success: true, ModelID: "m1", FeatureNames: []string{"x"}}); err == nil {
		t.Fatalf("expected error for treeless model")
	}
	p, _ := reg.Get("m1")
	pred, _ := p.PredictFromMap(map[string]float64{"return_5": 0.01})
	if pred != 0.001 {
		t.Fatalf("old model clobbered: pred=%v", pred)
	}

	// 非法 model id 拒绝
	if _, err := reg.HotLoad("../evil", testExportedModel("../evil", 0, 0)); err == nil {
		t.Fatalf("expected error for unsafe model id")
	}
}

func TestModelRegistryLoadFromDirSkipsCorrupt(t *testing.T) {
	dir := t.TempDir()
	reg := ml.NewModelRegistry(dir)
	if _, err := reg.HotLoad("good", testExportedModel("good", 0, 0.001)); err != nil {
		t.Fatalf("hotload: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg2 := ml.NewModelRegistry(dir)
	if n := reg2.LoadFromDir(); n != 1 {
		t.Fatalf("restore: got %d, want 1 (corrupt skipped)", n)
	}
	if _, ok := reg2.Get("good"); !ok {
		t.Fatalf("good model missing after restore")
	}
}
