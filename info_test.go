package main

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/pengtang1024/kore-plugin-container/internal/containerop"
)

// capabilityManifest 是本测试关心的 info.json 字段。
type capabilityManifest struct {
	ID                 string            `json:"id"`
	Version            string            `json:"version"`
	Protocol           string            `json:"protocol"`
	RequiresKore       string            `json:"requires_kore"`
	Capabilities       []string          `json:"capabilities"`
	MethodCapabilities map[string]string `json:"method_capabilities"`
}

// readCapabilityManifest 读取仓库根的 info.json（go test 的工作目录就是包目录）。
func readCapabilityManifest(t *testing.T) capabilityManifest {
	t.Helper()
	raw, err := os.ReadFile("info.json")
	if err != nil {
		t.Fatalf("读取 info.json 失败: %v", err)
	}
	var m capabilityManifest
	if err = json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("解析 info.json 失败: %v", err)
	}
	return m
}

// configContractMethods 是 main 包 dispatch 实现、info.json 声明的配置契约方法
// （PLUGIN-CONFIG §1）；它们不在 containerop 业务方法白名单里，单独纳入比对。
var configContractMethods = map[string]string{
	"config.get": "system:read",
	"config.set": "system:admin",
}

// TestMethodCapabilitiesMatchCode 守护「info.json 声明 == 代码方法表 + 配置契约」：
// 少一条 → 内核按插件级 capability 放行（授权粒度退化）；多一条 → 声明了不存在的方法。
func TestMethodCapabilitiesMatchCode(t *testing.T) {
	manifest := readCapabilityManifest(t)
	code := containerop.MethodCapabilities()
	expected := len(code) + len(configContractMethods)

	if len(manifest.MethodCapabilities) != expected {
		t.Fatalf("method_capabilities 条数=%d，期望 %d（业务 %d + 配置契约 %d）",
			len(manifest.MethodCapabilities), expected, len(code), len(configContractMethods))
	}
	for verb, wantCapability := range code {
		full := "container." + verb
		got, ok := manifest.MethodCapabilities[full]
		if !ok {
			t.Fatalf("info.json 缺少方法级 capability：%s", full)
		}
		if got != wantCapability {
			t.Fatalf("%s 的 capability=%q，期望 %q", full, got, wantCapability)
		}
	}
	for verb, wantCapability := range configContractMethods {
		full := "container." + verb
		got, ok := manifest.MethodCapabilities[full]
		if !ok {
			t.Fatalf("info.json 缺少配置契约 capability：%s", full)
		}
		if got != wantCapability {
			t.Fatalf("%s 的 capability=%q，期望 %q", full, got, wantCapability)
		}
	}
	for full := range manifest.MethodCapabilities {
		if !strings.HasPrefix(full, "container.") {
			t.Fatalf("method_capabilities 的键 %q 不属于本插件域（应为 container.<action>）", full)
		}
		verb := strings.TrimPrefix(full, "container.")
		if _, ok := code[verb]; !ok {
			if _, ok := configContractMethods[verb]; !ok {
				t.Fatalf("info.json 声明了代码未实现的方法：%s", full)
			}
		}
	}
}

// TestMethodCapabilitiesClassification 逐条钉死读/写方法的能力分类。
func TestMethodCapabilitiesClassification(t *testing.T) {
	manifest := readCapabilityManifest(t)
	read := []string{"list", "inspect", "logs", "stats", "images", "health"}
	write := []string{"exec", "start", "stop", "restart", "pull", "rmi", "update"}

	for _, verb := range read {
		if got := manifest.MethodCapabilities["container."+verb]; got != "system:read" {
			t.Fatalf("读方法 container.%s 的 capability=%q，期望 system:read", verb, got)
		}
	}
	for _, verb := range write {
		if got := manifest.MethodCapabilities["container."+verb]; got != "system:admin" {
			t.Fatalf("写方法 container.%s 的 capability=%q，期望 system:admin", verb, got)
		}
	}

	// 配置契约按 §规范：config.get=system:read、config.set=system:admin。
	for _, c := range []struct {
		method string
		want   string
	}{
		{"container.config.get", "system:read"},
		{"container.config.set", "system:admin"},
	} {
		if got := manifest.MethodCapabilities[c.method]; got != c.want {
			t.Fatalf("配置契约 %s 的 capability=%q，期望 %q", c.method, got, c.want)
		}
	}

	// 方法总数 = 读 + 写 + 配置契约，防止漏声明或重复分类。
	if got, want := len(manifest.MethodCapabilities), len(read)+len(write)+len(configContractMethods); got != want {
		t.Fatalf("method_capabilities 条数=%d，期望 %d（读 %d + 写 %d + 契约 %d）",
			got, want, len(read), len(write), len(configContractMethods))
	}
}

// TestPluginCapabilitiesCoverMethods 校验插件级 capabilities 覆盖所有方法级取值。
func TestPluginCapabilitiesCoverMethods(t *testing.T) {
	manifest := readCapabilityManifest(t)
	declared := make(map[string]bool, len(manifest.Capabilities))
	for _, capability := range manifest.Capabilities {
		declared[capability] = true
	}
	for method, capability := range manifest.MethodCapabilities {
		if !declared[capability] {
			t.Fatalf("%s 要求能力 %q，但插件级 capabilities 未声明", method, capability)
		}
	}
}

// TestDeniedMethodsNotDeclared exec/shell/attach 明确不做：不得出现在 info.json，
// 也不得出现在已实现方法表里（代码里由 deny 分支在触达 docker 前拒绝）。
func TestDeniedMethodsNotDeclared(t *testing.T) {
	manifest := readCapabilityManifest(t)
	code := containerop.MethodCapabilities()
	for _, verb := range containerop.DeniedMethods() {
		full := "container." + verb
		if _, ok := manifest.MethodCapabilities[full]; ok {
			t.Fatalf("明确不做的方法不应声明 capability：%s", full)
		}
		if _, ok := code[verb]; ok {
			t.Fatalf("明确不做的方法不应出现在已实现方法表：%s", full)
		}
	}
}

// TestSupportedMethodsInventory 方法清单可复现：13 个业务方法 + 2 个拒绝方法。
func TestSupportedMethodsInventory(t *testing.T) {
	supported := containerop.SupportedMethods()
	if len(supported) != 13 {
		t.Fatalf("SupportedMethods 数量=%d，期望 13：%v", len(supported), supported)
	}
	if !sort.SliceIsSorted(supported, func(i, j int) bool { return supported[i] < supported[j] }) {
		t.Fatalf("SupportedMethods 未按字典序返回：%v", supported)
	}
	denied := containerop.DeniedMethods()
	wantDenied := []string{"attach", "shell"}
	if strings.Join(denied, ",") != strings.Join(wantDenied, ",") {
		t.Fatalf("DeniedMethods=%v，期望 %v", denied, wantDenied)
	}
}

// TestManifestVersionMatchVersion 防止 info.json / manifest.json 版本漂移。
func TestManifestVersionMatchVersion(t *testing.T) {
	manifest := readCapabilityManifest(t)
	raw, err := os.ReadFile("manifest.json")
	if err != nil {
		t.Fatalf("读取 manifest.json 失败: %v", err)
	}
	var embedded capabilityManifest
	if err = json.Unmarshal(raw, &embedded); err != nil {
		t.Fatalf("解析 manifest.json 失败: %v", err)
	}
	if manifest.Version != embedded.Version {
		t.Fatalf("info.json 版本 %q != manifest.json 版本 %q", manifest.Version, embedded.Version)
	}
	if manifest.ID != embedded.ID || manifest.Protocol != embedded.Protocol ||
		manifest.RequiresKore != embedded.RequiresKore ||
		strings.Join(manifest.Capabilities, ",") != strings.Join(embedded.Capabilities, ",") {
		t.Fatalf("info.json 与 manifest.json 的握手字段不一致")
	}
}
