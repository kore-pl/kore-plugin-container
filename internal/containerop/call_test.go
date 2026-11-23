package containerop

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// testExecutor 是单元测试用执行面：回固定数据、记录调用，不触达 docker。
type testExecutor struct {
	list    []ContainerEntry
	inspect *InspectResult
	logs    LogsResult
	stats   StatsResult
	ref     ContainerRef
	err     error
	calls   []string
}

func (e *testExecutor) List(all bool) ([]ContainerEntry, error) {
	e.calls = append(e.calls, "list")
	return e.list, e.err
}

func (e *testExecutor) Inspect(id string) (*InspectResult, error) {
	e.calls = append(e.calls, "inspect")
	return e.inspect, e.err
}

func (e *testExecutor) Logs(id string, tail int, since string) (LogsResult, error) {
	e.calls = append(e.calls, "logs")
	return e.logs, e.err
}

func (e *testExecutor) Start(id string) error {
	e.calls = append(e.calls, "start")
	return e.err
}

func (e *testExecutor) Stop(id string, timeout int) error {
	e.calls = append(e.calls, "stop")
	return e.err
}

func (e *testExecutor) Restart(id string, timeout int) error {
	e.calls = append(e.calls, "restart")
	return e.err
}

func (e *testExecutor) Stats(id string) (StatsResult, error) {
	e.calls = append(e.calls, "stats")
	return e.stats, e.err
}

func (e *testExecutor) Resolve(id string) (ContainerRef, error) {
	e.calls = append(e.calls, "resolve")
	if e.err != nil {
		return ContainerRef{}, e.err
	}
	if e.ref.ID == "" {
		return ContainerRef{ID: strings.Repeat("a", 64), Name: id}, nil
	}
	return e.ref, nil
}

// healthyExecutor 返回一条 web 容器的完整数据。
func healthyExecutor() *testExecutor {
	const full = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	return &testExecutor{
		list: []ContainerEntry{{ID: "aaaaaaaaaaaa", Name: "web", Image: "nginx:alpine", State: "running"}},
		inspect: &InspectResult{
			ID: full, Name: "web", Image: "nginx:alpine",
			State: InspectState{Status: "running", Running: true},
		},
		logs:  LogsResult{ID: "web", Lines: []string{"line"}},
		stats: StatsResult{ID: "web", Name: "web", PIDs: 3},
		ref:   ContainerRef{ID: full, Name: "web"},
	}
}

func callWith(t *testing.T, exec Executor, method, params string, opts Options) (json.RawMessage, *pluginapi.Error) {
	t.Helper()
	if opts.GOOS == "" {
		opts.GOOS = "linux"
	}
	return Call(method, json.RawMessage(params), opts, exec)
}

// TestBusinessVerb 只接受 container.<verb>，裸动词与其它域一律拒绝。
func TestBusinessVerb(t *testing.T) {
	for _, method := range []string{"container.list", "container.inspect", "container.logs", "container.start", "container.stop", "container.restart", "container.stats"} {
		if _, ok := businessVerb(method); !ok {
			t.Fatalf("%s 未被接受", method)
		}
	}
	for _, method := range []string{"list", "inspect", "container.exec", "db-mysql.list", "Container.list"} {
		if _, ok := businessVerb(method); ok {
			t.Fatalf("%s 不应被接受", method)
		}
	}
}

// TestCallList list 返回执行面条目与 all 标记。
func TestCallList(t *testing.T) {
	exec := healthyExecutor()
	result, perr := callWith(t, exec, "container.list", `{"all":true}`, Options{})
	if perr != nil {
		t.Fatalf("list 错误: %v", perr)
	}
	var parsed ListResult
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("解析 list 结果失败: %v", err)
	}
	if parsed.Count != 1 || !parsed.All || parsed.Containers[0].Name != "web" {
		t.Fatalf("list 结果不符: %+v", parsed)
	}
}

// TestCallInspect inspect 返回精简结构。
func TestCallInspect(t *testing.T) {
	result, perr := callWith(t, healthyExecutor(), "container.inspect", `{"id":"web"}`, Options{})
	if perr != nil {
		t.Fatalf("inspect 错误: %v", perr)
	}
	var parsed InspectResult
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("解析 inspect 结果失败: %v", err)
	}
	if parsed.Name != "web" || !parsed.State.Running {
		t.Fatalf("inspect 结果不符: %+v", parsed)
	}
}

// TestConfirmRequired stop/restart 缺 confirm 时 E_INVALID，且不触达执行面。
func TestConfirmRequired(t *testing.T) {
	for _, verb := range []string{"stop", "restart"} {
		exec := healthyExecutor()
		_, perr := callWith(t, exec, "container."+verb, `{"id":"web"}`, Options{})
		if perr == nil || perr.Code != pluginapi.E_INVALID {
			t.Fatalf("%s 缺 confirm 错误=%v，期望 E_INVALID", verb, perr)
		}
		if !strings.Contains(perr.Message, "confirm=true") {
			t.Fatalf("%s 错误信息未说明确认参数: %q", verb, perr.Message)
		}
		if len(exec.calls) != 0 {
			t.Fatalf("%s 缺 confirm 却触达执行面: %v", verb, exec.calls)
		}
	}
}

// TestConfirmAccepted stop/restart 带 confirm 时执行。
func TestConfirmAccepted(t *testing.T) {
	for _, verb := range []string{"stop", "restart"} {
		exec := healthyExecutor()
		result, perr := callWith(t, exec, "container."+verb, `{"id":"web","confirm":true,"timeout":5}`, Options{})
		if perr != nil {
			t.Fatalf("%s 错误: %v", verb, perr)
		}
		var parsed ActionResult
		if err := json.Unmarshal(result, &parsed); err != nil {
			t.Fatalf("解析 %s 结果失败: %v", verb, err)
		}
		if !parsed.OK || parsed.Action != verb || parsed.ID == "" {
			t.Fatalf("%s 结果不符: %+v", verb, parsed)
		}
		if !containsCall(exec.calls, verb) {
			t.Fatalf("%s 未调用执行面: %v", verb, exec.calls)
		}
	}
}

// TestStartNeedsNoConfirm start 不需要 confirm，但仍过保护名单。
func TestStartNeedsNoConfirm(t *testing.T) {
	exec := healthyExecutor()
	if _, perr := callWith(t, exec, "container.start", `{"id":"web"}`, Options{}); perr != nil {
		t.Fatalf("start 错误: %v", perr)
	}
	if !containsCall(exec.calls, "start") {
		t.Fatalf("start 未调用执行面: %v", exec.calls)
	}
}

// TestProtectedRejectsWrites 命中保护名单的 start/stop/restart 全部 E_UNAUTHORIZED。
func TestProtectedRejectsWrites(t *testing.T) {
	for _, verb := range []string{"start", "stop", "restart"} {
		exec := healthyExecutor()
		params := `{"id":"web","confirm":true}`
		if verb == "start" {
			params = `{"id":"web"}`
		}
		_, perr := callWith(t, exec, "container."+verb, params, Options{Protected: []string{"web"}})
		if perr == nil || perr.Code != pluginapi.E_UNAUTHORIZED {
			t.Fatalf("%s 保护名单错误=%v，期望 E_UNAUTHORIZED", verb, perr)
		}
		if !containsCall(exec.calls, "resolve") || containsCall(exec.calls, verb) {
			t.Fatalf("%s 应在 resolve 后、动作前拒绝: %v", verb, exec.calls)
		}
	}
}

// TestProtectedListFlag 保护名单命中时 container.list 给出 protected 标记；
// stop 命中名单的 common-mysql 被拒（E_UNAUTHORIZED）。
func TestProtectedListFlag(t *testing.T) {
	exec := healthyExecutor()
	exec.list = []ContainerEntry{{ID: "bbbbbbbbbbbb", Name: "common-mysql", Image: "mysql:8", State: "running"}}
	raw, perr := callWith(t, exec, "container.list", `{}`, Options{Protected: []string{"common-mysql"}})
	if perr != nil {
		t.Fatalf("list 错误: %v", perr)
	}
	var result ListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("解析 list 结果: %v", err)
	}
	if !result.Containers[0].Protected {
		t.Fatalf("common-mysql 应带 protected=true")
	}
	stopExec := healthyExecutor()
	stopExec.ref = ContainerRef{ID: strings.Repeat("b", 64), Name: "common-mysql"}
	_, perr = callWith(t, stopExec, "container.stop",
		`{"id":"common-mysql","confirm":true}`, Options{Protected: []string{"common-mysql"}})
	if perr == nil || perr.Code != pluginapi.E_UNAUTHORIZED {
		t.Fatalf("stop common-mysql 错误=%v，期望 E_UNAUTHORIZED", perr)
	}
}

// TestProtectedAllowsReads 保护名单只拦写操作，读操作照常。
func TestProtectedAllowsReads(t *testing.T) {
	opts := Options{Protected: []string{"web"}}
	for _, method := range []string{"container.list", "container.inspect", "container.logs", "container.stats"} {
		params := `{"id":"web"}`
		if method == "container.list" {
			params = `{}`
		}
		if _, perr := callWith(t, healthyExecutor(), method, params, opts); perr != nil {
			t.Fatalf("%s 被保护名单误拦: %v", method, perr)
		}
	}
}

// TestUnsupportedGOOS 非 Linux 平台显式 E_UNSUPPORTED。
func TestUnsupportedGOOS(t *testing.T) {
	_, perr := Call("container.list", json.RawMessage(`{}`), Options{GOOS: "darwin"}, healthyExecutor())
	if perr == nil || perr.Code != pluginapi.E_UNSUPPORTED {
		t.Fatalf("非 Linux 错误=%v，期望 E_UNSUPPORTED", perr)
	}
}

// TestDockerMissing 找不到 docker CLI 时 E_UNSUPPORTED 且说明安装路径。
func TestDockerMissing(t *testing.T) {
	_, perr := Call("container.list", json.RawMessage(`{}`),
		Options{DockerBin: "kore-definitely-missing-docker-xyz", GOOS: "linux"}, nil)
	if perr == nil || perr.Code != pluginapi.E_UNSUPPORTED {
		t.Fatalf("缺 docker 错误=%v，期望 E_UNSUPPORTED", perr)
	}
	if !strings.Contains(perr.Message, DefaultDockerBin) && !strings.Contains(perr.Message, EnvDockerBin) {
		t.Fatalf("错误信息未给出安装/覆盖提示: %q", perr.Message)
	}
}

// TestLogsTailBounds tail 缺省 200，越界 E_INVALID；since 形状非法 E_INVALID。
func TestLogsTailBounds(t *testing.T) {
	for _, params := range []string{`{"id":"web","tail":0}`, `{"id":"web","tail":2001}`, `{"id":"web","tail":-1}`} {
		if _, perr := callWith(t, healthyExecutor(), "container.logs", params, Options{}); perr == nil || perr.Code != pluginapi.E_INVALID {
			t.Fatalf("tail 参数 %s 错误=%v，期望 E_INVALID", params, perr)
		}
	}
	if _, perr := callWith(t, healthyExecutor(), "container.logs", `{"id":"web","tail":100}`, Options{}); perr != nil {
		t.Fatalf("合法 tail 错误: %v", perr)
	}
	if _, perr := callWith(t, healthyExecutor(), "container.logs", `{"id":"web","since":"10m"}`, Options{}); perr != nil {
		t.Fatalf("合法 since 错误: %v", perr)
	}
	for _, bad := range []string{`{"id":"web","since":"1 2"}`, `{"id":"web","since":"a;b"}`, `{"id":"web","since":"-x"}`} {
		if _, perr := callWith(t, healthyExecutor(), "container.logs", bad, Options{}); perr == nil || perr.Code != pluginapi.E_INVALID {
			t.Fatalf("since 参数 %s 错误=%v，期望 E_INVALID", bad, perr)
		}
	}
}

// TestActionTimeoutBounds timeout 越界 E_INVALID。
func TestActionTimeoutBounds(t *testing.T) {
	_, perr := callWith(t, healthyExecutor(), "container.stop", `{"id":"web","confirm":true,"timeout":301}`, Options{})
	if perr == nil || perr.Code != pluginapi.E_INVALID {
		t.Fatalf("timeout 越界错误=%v，期望 E_INVALID", perr)
	}
}

// TestInvalidParams 非对象 params 与注入 ID 均在执行前拒绝。
func TestInvalidParams(t *testing.T) {
	cases := []struct{ method, params string }{
		{"container.list", `[]`},
		{"container.inspect", `not-json`},
		{"container.inspect", `{"id":"web;rm -rf /"}`},
		{"container.stop", `{"id":"web$(id)","confirm":true}`},
		{"container.stats", `{"id":"../../etc"}`},
	}
	for _, c := range cases {
		if _, perr := callWith(t, healthyExecutor(), c.method, c.params, Options{}); perr == nil || perr.Code != pluginapi.E_INVALID {
			t.Fatalf("%s %s 错误=%v，期望 E_INVALID", c.method, c.params, perr)
		}
	}
}

// TestUnknownMethod 未知方法 E_NOTFOUND。
func TestUnknownMethod(t *testing.T) {
	_, perr := callWith(t, healthyExecutor(), "container.exec", `{}`, Options{})
	if perr == nil || perr.Code != pluginapi.E_NOTFOUND {
		t.Fatalf("未知方法错误=%v，期望 E_NOTFOUND", perr)
	}
}

// TestCallStats stats 返回解析后的结构。
func TestCallStats(t *testing.T) {
	result, perr := callWith(t, healthyExecutor(), "container.stats", `{"id":"web"}`, Options{})
	if perr != nil {
		t.Fatalf("stats 错误: %v", perr)
	}
	var parsed StatsResult
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("解析 stats 失败: %v", err)
	}
	if parsed.PIDs != 3 {
		t.Fatalf("stats 结果不符: %+v", parsed)
	}
}

// containsCall 判断调用序列是否包含某方法。
func containsCall(calls []string, want string) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}
	return false
}
