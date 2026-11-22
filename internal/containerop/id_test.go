package containerop

import (
	"strings"
	"testing"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// TestValidateIDAccept 合法 64hex 与名称被接受并返回正确类别。
func TestValidateIDAccept(t *testing.T) {
	cases := map[string]string{
		strings.Repeat("a", 64): "id64", // 小写 64hex → id64
		"web":                   "name", "my_app-1.2": "name", "a": "name",
	}
	for raw, want := range cases {
		kind, perr := ValidateID(raw)
		if perr != nil {
			t.Fatalf("ValidateID(%q) 意外拒绝: %v", raw, perr)
		}
		if kind != want {
			t.Fatalf("ValidateID(%q)=%q，期望 %q", raw, kind, want)
		}
	}
}

// TestValidateIDReject 各类非法标识必须返回 E_INVALID。
func TestValidateIDReject(t *testing.T) {
	bad := []string{
		"", " ", "-web", ".web", "_web", "web/../etc",
		"web name", "web\x00x", strings.Repeat("x", maxNameLen+1),
		"abc@def", "a:b",
	}
	for _, raw := range bad {
		_, perr := ValidateID(raw)
		if perr == nil || perr.Code != pluginapi.E_INVALID {
			t.Fatalf("ValidateID(%q) 错误=%v，期望 E_INVALID", raw, perr)
		}
	}
}
