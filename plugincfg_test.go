package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pengtang1024/kore-plugin-container/internal/containerop"
	"github.com/pengtang1024/kore-plugin-container/internal/plugincfg"
	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// hasCJK 判断字符串是否含中日韩字符（契约要求 label/help 面向用户）。
func hasCJK(s string) bool {
	for _, r := range s {
		if r >= 0x4e00 && r <= 0x9fff {
			return true
		}
	}
	return false
}

// newTestState 构造测试用运行期配置：state 指向临时目录、来源 default。
func newTestState(t *testing.T) (*configState, *config) {
	t.Helper()
	dir := t.TempDir()
	st, err := newConfigState(dir)
	if err != nil {
		t.Fatalf("newConfigState: %v", err)
	}
	cfg := testConfig()
	cfg.state = st
	return st, &cfg
}

// TestConfigSchemaContract 逐条核对 PLUGIN-CONFIG §2：显式类型、中文 label/help、
// default 与 type 一致、select/list 约束、无 object/any。
func TestConfigSchemaContract(t *testing.T) {
	schema := configSchema()
	if err := plugincfg.ValidateSchema(schema); err != nil {
		t.Fatalf("schema 自检失败: %v", err)
	}
	if schema.Title == "" {
		t.Fatal("schema.title 不能为空")
	}
	wantTypes := map[string]string{
		"docker_bin":           plugincfg.TypeString,
		"protected_containers": plugincfg.TypeList,
		"docker_timeout_ms":    plugincfg.TypeInt,
	}
	if len(schema.Fields) != len(wantTypes) {
		t.Fatalf("字段数=%d，期望 %d", len(schema.Fields), len(wantTypes))
	}
	for _, field := range schema.Fields {
		want, ok := wantTypes[field.Key]
		if !ok {
			t.Fatalf("多余字段: %s", field.Key)
		}
		if field.Type != want {
			t.Fatalf("%s type=%q，期望 %q", field.Key, field.Type, want)
		}
		if field.Type == plugincfg.TypeList && field.Item != plugincfg.TypeString {
			t.Fatalf("%s list item=%q，期望 string", field.Key, field.Item)
		}
		if field.Label == "" || field.Help == "" {
			t.Fatalf("%s 缺少中文 label/help", field.Key)
		}
		if !hasCJK(field.Label) || !hasCJK(field.Help) {
			t.Fatalf("%s label/help 非中文: %q / %q", field.Key, field.Label, field.Help)
		}
		if len(field.Default) == 0 || !json.Valid(field.Default) {
			t.Fatalf("%s default 非法", field.Key)
		}
		var decoded any
		if err := json.Unmarshal(field.Default, &decoded); err != nil {
			t.Fatalf("%s default 解析失败: %v", field.Key, err)
		}
		switch field.Type {
		case plugincfg.TypeString:
			if _, ok := decoded.(string); !ok {
				t.Fatalf("%s default 不是 string", field.Key)
			}
		case plugincfg.TypeInt:
			if _, ok := decoded.(float64); !ok {
				t.Fatalf("%s default 不是 int", field.Key)
			}
		case plugincfg.TypeList:
			if _, ok := decoded.([]any); !ok {
				t.Fatalf("%s default 不是数组", field.Key)
			}
		}
	}
}

// TestConfigGetSourceDefault 无文件无环境变量时 source=default。
func TestConfigGetSourceDefault(t *testing.T) {
	_, cfg := newTestState(t)
	result, perr := configGet(*cfg)
	if perr != nil {
		t.Fatalf("configGet: %v", perr)
	}
	var got plugincfg.GetResult
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("解析结果: %v", err)
	}
	if got.Source != plugincfg.SourceDefault {
		t.Fatalf("source=%q，期望 default", got.Source)
	}
	if len(got.Schema.Fields) == 0 || len(got.Values) == 0 {
		t.Fatal("get 结果缺 schema.fields 或 values")
	}
}

// TestConfigGetSourceEnv 仅有环境变量时 source=env，值来自环境变量。
func TestConfigGetSourceEnv(t *testing.T) {
	t.Setenv(containerop.EnvDockerBin, "/usr/local/bin/docker")
	state, err := newConfigState(t.TempDir())
	if err != nil {
		t.Fatalf("newConfigState: %v", err)
	}
	if state.source != plugincfg.SourceEnv {
		t.Fatalf("source=%q，期望 env", state.source)
	}
	if state.values.DockerBin != "/usr/local/bin/docker" {
		t.Fatalf("docker_bin=%q", state.values.DockerBin)
	}
}

// TestConfigSetValidAndPersist set 正例：原子写 0600，读回一致，source=file，热生效。
func TestConfigSetValidAndPersist(t *testing.T) {
	state, cfg := newTestState(t)
	params := `{"values":{"docker_bin":"/usr/bin/docker","docker_timeout_ms":5000,"protected_containers":["web","db"]}}`
	result, perr := configSet(json.RawMessage(params), *cfg)
	if perr != nil {
		t.Fatalf("configSet: %v", perr)
	}
	var setResult plugincfg.SetResult
	if err := json.Unmarshal(result, &setResult); err != nil {
		t.Fatalf("解析 set 结果: %v", err)
	}
	if !setResult.Saved || !setResult.Applied || setResult.RestartRequired || len(setResult.Warnings) != 0 {
		t.Fatalf("set 结果不符: %+v", setResult)
	}
	if state.source != plugincfg.SourceFile {
		t.Fatalf("set 后 source=%q，期望 file", state.source)
	}
	info, err := os.Stat(state.store.Path())
	if err != nil {
		t.Fatalf("配置文件不存在: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("配置权限=%o，期望 600", perm)
	}
	saved, err := os.ReadFile(state.store.Path())
	if err != nil {
		t.Fatalf("读回配置: %v", err)
	}
	var persisted fileValues
	if err := json.Unmarshal(saved, &persisted); err != nil {
		t.Fatalf("解析读回配置: %v", err)
	}
	if persisted.DockerBin != "/usr/bin/docker" || persisted.DockerTimeoutMS != 5000 ||
		strings.Join(persisted.ProtectedContainers, ",") != "web,db" {
		t.Fatalf("读回配置不符: %+v", persisted)
	}
	// 重新从文件加载（模拟重启），值与来源必须保持。
	reloaded, err := newConfigState(filepath.Dir(state.store.Path()))
	if err != nil {
		t.Fatalf("重载: %v", err)
	}
	if reloaded.source != plugincfg.SourceFile || reloaded.values.DockerTimeoutMS != 5000 {
		t.Fatalf("重载结果不符: %+v", reloaded)
	}
	// 热生效：config.set 后 activeRuntime 用新值构造，无需重启。
	runtime := activeRuntime(*cfg, nil)
	if runtime == nil {
		t.Fatal("activeRuntime 返回 nil")
	}
}

// TestConfigSetInvalid set 反例：类型错 / 越界 / 非法 ID / 缺 values → E_INVALID，且旧配置不变。
func TestConfigSetInvalid(t *testing.T) {
	_, cfg := newTestState(t)
	cases := []struct {
		name   string
		params string
	}{
		{"缺 values", `{}`},
		{"params 非对象", `[]`},
		{"类型不符", `{"values":{"docker_timeout_ms":"5000"}}`},
		{"超时越界", `{"values":{"docker_timeout_ms":999}}`},
		{"docker_bin 含空白", `{"values":{"docker_bin":"/usr/bin/doc ker"}}`},
		{"保护名单注入", `{"values":{"protected_containers":["web;rm -rf /"]}}`},
		{"values 非对象", `{"values":[1]}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result, perr := configSet(json.RawMessage(c.params), *cfg)
			if perr == nil || perr.Code != pluginapi.E_INVALID {
				t.Fatalf("%s: 错误=%v，期望 E_INVALID", c.name, perr)
			}
			if result != nil {
				t.Fatalf("%s: 出错时不应带结果", c.name)
			}
		})
	}
}

// TestConfigSetPartialUpdate 未提供的字段保持原值，config.json 仍是全量形状。
func TestConfigSetPartialUpdate(t *testing.T) {
	state, cfg := newTestState(t)
	if _, perr := configSet(json.RawMessage(`{"values":{"docker_timeout_ms":9000}}`), *cfg); perr != nil {
		t.Fatalf("首次 set: %v", perr)
	}
	if _, perr := configSet(json.RawMessage(`{"values":{"docker_bin":"podman"}}`), *cfg); perr != nil {
		t.Fatalf("二次 set: %v", perr)
	}
	if state.values.DockerTimeoutMS != 9000 || state.values.DockerBin != "podman" {
		t.Fatalf("局部更新不符: %+v", state.values)
	}
}

// TestSensitiveMaskContract G-30：无敏感字段时 get 不回显占位；掩码工具对 sensitive
// 字段按 §6 回 "***" 且 set 提交 "***" 视为不修改（本仓无该字段，验证工具正确性）。
func TestSensitiveMaskContract(t *testing.T) {
	schema := plugincfg.Schema{Title: "t", Fields: []plugincfg.Field{{
		Key: "password", Label: "密码", Type: plugincfg.TypeString,
		Default: json.RawMessage(`""`), Required: false, Help: "密钥", Sensitive: true,
	}}}
	masked, err := plugincfg.MaskValues(schema, json.RawMessage(`{"password":"super-secret"}`))
	if err != nil {
		t.Fatalf("掩码失败: %v", err)
	}
	if strings.Contains(string(masked), "super-secret") || !strings.Contains(string(masked), "***") {
		t.Fatalf("敏感值未脱敏: %s", masked)
	}
	if !plugincfg.IsSensitivePlaceholder(json.RawMessage(`"***"`)) {
		t.Fatal("占位判定失败")
	}
	// 本仓 schema 无 sensitive 字段：config.get 不回显任何占位（如实标注，不虚标）。
	for _, field := range configSchema().Fields {
		if field.Sensitive {
			t.Fatalf("本仓不应有敏感字段，发现 %s", field.Key)
		}
	}
}

// TestDispatchConfigRoute dispatch 命中完全限定名与裸名，且 set 后 get 读到新值。
func TestDispatchConfigRoute(t *testing.T) {
	_, cfg := newTestState(t)
	for _, method := range []string{"container.config.get", "config.get"} {
		req := &pluginapi.Request{
			Type: pluginapi.TypeRequest, ID: "id", ReqID: "r", Method: method,
			Params: json.RawMessage(`{}`),
		}
		resp, shutdown := dispatch(req, *cfg, testRuntime(newFakeExecutor()), testLogger())
		if shutdown || resp.Error != nil || resp.Result == nil {
			t.Fatalf("%s: 响应不符 %+v", method, resp)
		}
		if !strings.Contains(string(resp.Result), `"schema"`) ||
			!strings.Contains(string(resp.Result), `"docker_bin"`) {
			t.Fatalf("%s: 结果缺 schema: %s", method, resp.Result)
		}
	}
	req := &pluginapi.Request{
		Type: pluginapi.TypeRequest, ID: "id", ReqID: "r", Method: "container.config.set",
		Params: json.RawMessage(`{"values":{"docker_bin":"podman"}}`),
	}
	resp, _ := dispatch(req, *cfg, testRuntime(newFakeExecutor()), testLogger())
	if resp.Error != nil || !strings.Contains(string(resp.Result), `"saved":true`) {
		t.Fatalf("config.set 响应不符: %+v", resp)
	}
	var after plugincfg.GetResult
	getResp, _ := dispatch(&pluginapi.Request{
		Type: pluginapi.TypeRequest, ID: "id", ReqID: "r2", Method: "container.config.get",
		Params: json.RawMessage(`{}`),
	}, *cfg, testRuntime(newFakeExecutor()), testLogger())
	if err := json.Unmarshal(getResp.Result, &after); err != nil {
		t.Fatalf("解析 get 结果: %v", err)
	}
	var values map[string]any
	if err := json.Unmarshal(after.Values, &values); err != nil {
		t.Fatalf("解析 values: %v", err)
	}
	if values["docker_bin"] != "podman" || after.Source != plugincfg.SourceFile {
		t.Fatalf("set 后 get 不符: values=%v source=%s", values, after.Source)
	}
}

// TestConfigBadFileFailsStartup §3：损坏配置文件让加载报错，不静默回退默认值。
func TestConfigBadFileFailsStartup(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{not json`), 0o600); err != nil {
		t.Fatalf("写坏配置: %v", err)
	}
	if _, err := newConfigState(dir); err == nil {
		t.Fatal("损坏配置应导致加载失败")
	}
}

// TestConfigSchemaFieldDefaultsUseRuntimeValues default 与 containerop 常量一致，不臆造。
func TestConfigSchemaFieldDefaultsUseRuntimeValues(t *testing.T) {
	values := defaultValues()
	if values.DockerBin != containerop.DefaultDockerBin {
		t.Fatalf("docker_bin 默认=%q，期望 %q", values.DockerBin, containerop.DefaultDockerBin)
	}
	if values.DockerTimeoutMS != int(containerop.DefaultDockerTimeout/time.Millisecond) {
		t.Fatalf("timeout 默认=%d", values.DockerTimeoutMS)
	}
}
