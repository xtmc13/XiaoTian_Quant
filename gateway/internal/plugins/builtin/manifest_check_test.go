package builtin

import "testing"

// TestManifestHasFeishuDingtalk 前端冻结契约：feishu/dingtalk 导航 + 斜杠命令必须在清单中。
func TestManifestHasFeishuDingtalk(t *testing.T) {
	found := map[string]bool{}
	for _, m := range Manager().Manifest() {
		if m.UI.Nav != nil {
			found[m.UI.Nav.ID] = true
		}
		for _, s := range m.UI.Slash {
			found["/"+s.Name] = true
		}
	}
	for _, want := range []string{"feishu", "/feishu", "dingtalk", "/dingtalk"} {
		if !found[want] {
			t.Errorf("manifest 缺 %s", want)
		}
	}
}
