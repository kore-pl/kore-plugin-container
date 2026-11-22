package containerop

import (
	"strings"
	"testing"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// TestValidateIDAccept 合法名称 / 短 ID / 完整 ID 被接受。
func TestValidateIDAccept(t *testing.T) {
	cases := []string{
		"a", "web", "my_app-1.2", "kore-ct-test",
		strings.Repeat("a", 64), strings.Repeat("x", 128),
	}
	for _, raw := range cases {
		if perr := ValidateID(raw); perr != nil {
			t.Fatalf("ValidateID(%q) 意外拒绝: %v", raw, perr)
		}
	}
}

// TestValidateIDReject 命令注入与非法形状必须返回 E_INVALID。
func TestValidateIDReject(t *testing.T) {
	bad := []string{
		"", " ", "-web", ".web", "_web", "web/../etc",
		"web name", "web\x00x", "web\nname", "web\tname",
		"web;rm -rf /", "web$(id)", "web`id`", "web&&id",
		"web|id", "web'x", `web"x`, "web\\x",
		strings.Repeat("x", 129),
	}
	for _, raw := range bad {
		perr := ValidateID(raw)
		if perr == nil || perr.Code != pluginapi.E_INVALID {
			t.Fatalf("ValidateID(%q) 错误=%v，期望 E_INVALID", raw, perr)
		}
	}
}

// TestCheckProtected 名称 / 完整 ID / ≥12 位短 ID 命中；其它不误伤。
func TestCheckProtected(t *testing.T) {
	const full = "0123456789ab0123456789ab0123456789ab0123456789ab0123456789ab"
	ref := ContainerRef{ID: full, Name: "kore-ct-test"}

	blocked := []string{"kore-ct-test", full, full[:12], strings.ToUpper(full[:12]), full[:8]}
	for _, token := range blocked {
		perr := CheckProtected(ref, []string{token})
		if perr == nil || perr.Code != pluginapi.E_UNAUTHORIZED {
			t.Fatalf("保护名单 %q 未命中: %v", token, perr)
		}
	}

	allowed := []string{"", "other", "kore-ct", "abcdef", "0123456789zz"}
	for _, token := range allowed {
		if perr := CheckProtected(ref, []string{token}); perr != nil {
			t.Fatalf("保护名单 %q 误伤: %v", token, perr)
		}
	}
}

// TestDetectSelfContainerIDs 容器内 HOSTNAME 为 12 位 hex 时纳入保护。
func TestDetectSelfContainerIDs(t *testing.T) {
	t.Setenv("HOSTNAME", "0123456789ab")
	ids := DetectSelfContainerIDs()
	found := false
	for _, id := range ids {
		if id == "0123456789ab" {
			found = true
		}
	}
	if !found {
		t.Fatalf("DetectSelfContainerIDs()=%v，未包含 HOSTNAME 短 ID", ids)
	}
}
