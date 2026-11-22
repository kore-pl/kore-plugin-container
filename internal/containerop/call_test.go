package containerop

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// TestBusinessVerb 方法名归一化：container.<verb> 与裸 <verb> 都接受。
func TestBusinessVerb(t *testing.T) {
	cases := map[string]string{
		"container.list": "list", "container.inspect": "inspect",
		"list": "list", "inspect": "inspect",
	}
	for method, want := range cases {
		got, ok := businessVerb(method)
		if !ok || got != want {
			t.Fatalf("businessVerb(%q)=(%q,%v)，期望 (%q,true)", method, got, ok, want)
		}
	}
	if _, ok := businessVerb("container.exec"); ok {
		t.Fatal("container.exec 不应被接受")
	}
}

// TestCallResults 两方法正常路径返回结构化结果。
func TestCallResults(t *testing.T) {
	list, perr := Call("container.list", json.RawMessage(`{}`), nil)
	if perr != nil || !strings.Contains(string(list), `"count":0`) {
		t.Fatalf("container.list 结果=%s err=%v", list, perr)
	}
	inspect, perr := Call("container.inspect", json.RawMessage(`{"id":"web"}`), nil)
	if perr != nil || !strings.Contains(string(inspect), `"id_kind":"name"`) {
		t.Fatalf("container.inspect 结果=%s err=%v", inspect, perr)
	}
}

// TestCallInspectID64 64 位 hex 标识被识别为 id64。
func TestCallInspectID64(t *testing.T) {
	id := strings.Repeat("a", 64)
	result, perr := Call("container.inspect", json.RawMessage(`{"id":"`+id+`"}`), nil)
	if perr != nil || !strings.Contains(string(result), `"id_kind":"id64"`) {
		t.Fatalf("64hex inspect 结果=%s err=%v", result, perr)
	}
}

// TestCallParamValidation 非法参数形状与非法 ID 均返回 E_INVALID。
func TestCallParamValidation(t *testing.T) {
	bad := []struct {
		method string
		params string
	}{
		{"container.list", `[]`},
		{"container.inspect", `not-json`},
		{"container.inspect", `{"id":"../etc"}`},
		{"container.inspect", `{"id":""}`},
	}
	for _, c := range bad {
		_, perr := Call(c.method, json.RawMessage(c.params), nil)
		if perr == nil || perr.Code != pluginapi.E_INVALID {
			t.Fatalf("Call(%s,%s) 错误=%v，期望 E_INVALID", c.method, c.params, perr)
		}
	}
}

// TestCallUnknownMethod 未知方法返回 E_NOTFOUND。
func TestCallUnknownMethod(t *testing.T) {
	_, perr := Call("container.exec", json.RawMessage(`{}`), nil)
	if perr == nil || perr.Code != pluginapi.E_NOTFOUND {
		t.Fatalf("未知方法错误=%v，期望 E_NOTFOUND", perr)
	}
}
