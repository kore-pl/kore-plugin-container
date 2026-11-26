package containerop

import (
	"encoding/json"
	"errors"
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
	images  []ImageEntry
	updated *ContainerUpdate
	execReq *ExecRequest
	removed []string
	pulled  []string
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

func (e *testExecutor) Images() ([]ImageEntry, error) {
	e.calls = append(e.calls, "images")
	return e.images, e.err
}

func (e *testExecutor) Pull(ref string) (PullResult, error) {
	e.calls = append(e.calls, "pull")
	e.pulled = append(e.pulled, ref)
	if e.err != nil {
		return PullResult{}, e.err
	}
	return PullResult{Ref: ref, OK: true, Message: "pulled"}, nil
}

func (e *testExecutor) RemoveImage(ref string, force bool) error {
	e.calls = append(e.calls, "rmi")
	e.removed = append(e.removed, ref)
	return e.err
}

func (e *testExecutor) Update(id string, update ContainerUpdate) error {
	e.calls = append(e.calls, "update")
	e.updated = &update
	return e.err
}

func (e *testExecutor) Exec(req ExecRequest) (ExecResult, error) {
	e.calls = append(e.calls, "exec")
	e.execReq = &req
	if e.err != nil {
		return ExecResult{}, e.err
	}
	return ExecResult{ExitCode: 0, Stdout: "ok"}, nil
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

// TestBusinessVerb 只接受 container.<verb>，裸动词与其它域一律拒绝；
// shell / attach 属于「已识别但明确拒绝」的方法（由 Call 返回 E_UNSUPPORTED）；exec 已改为受控支持。
func TestBusinessVerb(t *testing.T) {
	for _, method := range []string{"container.list", "container.exec", "container.inspect", "container.logs",
		"container.start", "container.stop", "container.restart", "container.stats",
		"container.images", "container.health", "container.pull", "container.rmi", "container.update"} {
		if _, ok := businessVerb(method); !ok {
			t.Fatalf("%s 未被接受", method)
		}
	}
	for _, method := range []string{"container.shell", "container.attach"} {
		verb, ok := businessVerb(method)
		if !ok || !isDeniedVerb(verb) {
			t.Fatalf("%s 应识别为拒绝方法", method)
		}
	}
	for _, method := range []string{"list", "inspect", "db-mysql.list", "Container.list", "container.nope"} {
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
	var detail struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(perr.Detail, &detail); err != nil || detail.Reason != ExecReasonDockerMissing {
		t.Fatalf("detail.reason = %q (%v)，期望 %q", detail.Reason, err, ExecReasonDockerMissing)
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

// TestUnknownMethod 未知方法 E_NOTFOUND；exec 是明确拒绝而非未知（见 TestDeniedMethods）。
func TestUnknownMethod(t *testing.T) {
	_, perr := callWith(t, healthyExecutor(), "container.nope", `{}`, Options{})
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

// TestDeniedMethods shell / attach 在触达执行面之前返回 E_UNSUPPORTED（exec 已改为受控支持）。
func TestDeniedMethods(t *testing.T) {
	for _, method := range []string{"container.shell", "container.attach"} {
		exec := healthyExecutor()
		result, perr := callWith(t, exec, method, `{}`, Options{})
		if perr == nil || perr.Code != pluginapi.E_UNSUPPORTED {
			t.Fatalf("%s 错误=%v，期望 E_UNSUPPORTED", method, perr)
		}
		if result != nil {
			t.Fatalf("%s 不应带结果", method)
		}
		if len(exec.calls) != 0 {
			t.Fatalf("%s 不应触达执行面，实际调用=%v", method, exec.calls)
		}
		if !strings.Contains(perr.Message, "ADR-0018") || !strings.Contains(perr.Message, "G-11") {
			t.Fatalf("%s 拒绝理由未引用 ADR-0018 / G-11：%q", method, perr.Message)
		}
	}
}

// TestCallImages 镜像列表透传并统计条数。
func TestCallImages(t *testing.T) {
	exec := healthyExecutor()
	exec.images = []ImageEntry{
		{ID: "0123456789ab", Repository: "nginx", Tag: "alpine", Size: "10MB"},
		{ID: "abcdef012345", Repository: "<none>", Tag: "<none>", Dangling: true},
	}
	result, perr := callWith(t, exec, "container.images", `{}`, Options{})
	if perr != nil {
		t.Fatalf("images 错误: %v", perr)
	}
	var parsed ImagesResult
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("解析 images 失败: %v", err)
	}
	if parsed.Count != 2 || len(parsed.Images) != 2 {
		t.Fatalf("images 结果不符: %+v", parsed)
	}
	if parsed.Images[0].Dangling {
		t.Fatalf("首条镜像不应是 dangling: %+v", parsed.Images[0])
	}
	if parsed.Images[1].Dangling != true {
		t.Fatalf("<none>:<none> 应标记 dangling: %+v", parsed.Images[1])
	}
}

// TestCallImagesEmpty 无镜像时返回空数组而非 null。
func TestCallImagesEmpty(t *testing.T) {
	result, perr := callWith(t, healthyExecutor(), "container.images", `{}`, Options{})
	if perr != nil {
		t.Fatalf("images 错误: %v", perr)
	}
	if string(result) != `{"images":[],"count":0}` && !strings.Contains(string(result), `"images":[]`) {
		t.Fatalf("空镜像结果不符: %s", result)
	}
}

// TestCallPull pull 必须 confirm、ref 形状先校验、成功后透传摘要。
func TestCallPull(t *testing.T) {
	cases := []struct {
		name   string
		params string
		code   string
	}{
		{"缺确认", `{"ref":"nginx:alpine"}`, pluginapi.E_INVALID},
		{"空 ref", `{"ref":"","confirm":true}`, pluginapi.E_INVALID},
		{"flag 注入", `{"ref":"--privileged","confirm":true}`, pluginapi.E_INVALID},
		{"空白注入", `{"ref":"nginx alpine","confirm":true}`, pluginapi.E_INVALID},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			exec := healthyExecutor()
			_, perr := callWith(t, exec, "container.pull", c.params, Options{})
			if perr == nil || perr.Code != c.code {
				t.Fatalf("pull 错误=%v，期望 %q", perr, c.code)
			}
			if containsCall(exec.calls, "pull") {
				t.Fatalf("被拒的 pull 不应触达执行面：%v", exec.calls)
			}
		})
	}

	exec := healthyExecutor()
	result, perr := callWith(t, exec, "container.pull", `{"ref":"nginx:alpine","confirm":true}`, Options{})
	if perr != nil {
		t.Fatalf("pull 错误: %v", perr)
	}
	var parsed PullResult
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("解析 pull 失败: %v", err)
	}
	if !parsed.OK || parsed.Ref != "nginx:alpine" {
		t.Fatalf("pull 结果不符: %+v", parsed)
	}
	if len(exec.pulled) != 1 || exec.pulled[0] != "nginx:alpine" {
		t.Fatalf("pull 未按原样传给执行面：%v", exec.pulled)
	}
}

// TestCallRemoveImage rmi 必须 confirm，force 原样透传。
func TestCallRemoveImage(t *testing.T) {
	exec := healthyExecutor()
	if _, perr := callWith(t, exec, "container.rmi", `{"ref":"nginx:alpine"}`, Options{}); perr == nil || perr.Code != pluginapi.E_INVALID {
		t.Fatalf("rmi 缺确认错误=%v，期望 E_INVALID", perr)
	}
	if containsCall(exec.calls, "rmi") {
		t.Fatalf("缺确认的 rmi 不应触达执行面：%v", exec.calls)
	}
	result, perr := callWith(t, exec, "container.rmi", `{"ref":"nginx:alpine","confirm":true,"force":true}`, Options{})
	if perr != nil {
		t.Fatalf("rmi 错误: %v", perr)
	}
	var parsed ImageRemoveResult
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("解析 rmi 失败: %v", err)
	}
	if !parsed.OK || !parsed.Force || parsed.Ref != "nginx:alpine" {
		t.Fatalf("rmi 结果不符: %+v", parsed)
	}
	if len(exec.removed) != 1 || exec.removed[0] != "nginx:alpine" {
		t.Fatalf("rmi 未按原样传给执行面：%v", exec.removed)
	}
}

// TestCallUpdate update 的参数边界、确认与保护名单。
func TestCallUpdate(t *testing.T) {
	invalid := []struct {
		name   string
		params string
	}{
		{"缺确认", `{"id":"web","memory_bytes":134217728}`},
		{"无任何字段", `{"id":"web","confirm":true}`},
		{"内存过小", `{"id":"web","memory_bytes":1048576,"confirm":true}`},
		{"内存过大", `{"id":"web","memory_bytes":1099511627777,"confirm":true}`},
		{"CPU 过小", `{"id":"web","cpus":0.001,"confirm":true}`},
		{"CPU 过大", `{"id":"web","cpus":2048,"confirm":true}`},
		{"ID 非法", `{"id":"../etc","memory_bytes":134217728,"confirm":true}`},
	}
	for _, c := range invalid {
		t.Run(c.name, func(t *testing.T) {
			exec := healthyExecutor()
			if _, perr := callWith(t, exec, "container.update", c.params, Options{}); perr == nil || perr.Code != pluginapi.E_INVALID {
				t.Fatalf("update %s 错误=%v，期望 E_INVALID", c.params, perr)
			}
			if containsCall(exec.calls, "update") {
				t.Fatalf("被拒的 update 不应触达执行面：%v", exec.calls)
			}
		})
	}

	exec := healthyExecutor()
	result, perr := callWith(t, exec, "container.update",
		`{"id":"web","memory_bytes":134217728,"cpus":1.5,"confirm":true}`, Options{})
	if perr != nil {
		t.Fatalf("update 错误: %v", perr)
	}
	var parsed UpdateResult
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("解析 update 失败: %v", err)
	}
	if !parsed.OK || parsed.MemoryBytes == nil || *parsed.MemoryBytes != 134217728 ||
		parsed.CPUs == nil || *parsed.CPUs != 1.5 {
		t.Fatalf("update 结果不符: %+v", parsed)
	}
	if exec.updated == nil || exec.updated.MemoryBytes == nil || *exec.updated.MemoryBytes != 134217728 {
		t.Fatalf("update 未把限额传给执行面：%+v", exec.updated)
	}
}

// TestCallUpdateProtected 保护名单命中时 update 被拒且不触达执行面。
func TestCallUpdateProtected(t *testing.T) {
	exec := healthyExecutor()
	exec.ref = ContainerRef{ID: strings.Repeat("b", 64), Name: "kore"}
	_, perr := callWith(t, exec, "container.update",
		`{"id":"kore","memory_bytes":134217728,"confirm":true}`,
		Options{Protected: []string{"kore"}})
	if perr == nil || perr.Code != pluginapi.E_UNAUTHORIZED {
		t.Fatalf("受保护容器 update 错误=%v，期望 E_UNAUTHORIZED", perr)
	}
	if containsCall(exec.calls, "update") {
		t.Fatalf("受保护容器不应触达 update：%v", exec.calls)
	}
}

// TestCallHealth health 投影 inspect 的 state/health 字段。
func TestCallHealth(t *testing.T) {
	exec := healthyExecutor()
	exec.inspect = &InspectResult{
		ID: "web", Health: "healthy",
		State: InspectState{Status: "running", Running: true, RestartCount: 3, StartedAt: "2026-11-23T00:00:00Z"},
	}
	result, perr := callWith(t, exec, "container.health", `{"id":"web"}`, Options{})
	if perr != nil {
		t.Fatalf("health 错误: %v", perr)
	}
	var parsed HealthResult
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("解析 health 失败: %v", err)
	}
	if parsed.Health != "healthy" || parsed.Status != "running" || !parsed.Running ||
		parsed.RestartCount != 3 || parsed.StartedAt != "2026-11-23T00:00:00Z" {
		t.Fatalf("health 结果不符: %+v", parsed)
	}
}

// TestCallHealthMissing 容器不存在时 E_NOTFOUND。
func TestCallHealthMissing(t *testing.T) {
	exec := healthyExecutor()
	exec.inspect = nil
	_, perr := callWith(t, exec, "container.health", `{"id":"web"}`, Options{})
	if perr == nil || perr.Code != pluginapi.E_NOTFOUND {
		t.Fatalf("缺失容器 health 错误=%v，期望 E_NOTFOUND", perr)
	}
}

// TestExecErrorSocketPermissionDeniedMapping 断言 ADR-0018 降级矩阵（container × docker.sock）：
// 无权访问 docker.sock 必须返回 E_NEED_ROOT，且不能被「daemon 不可达」分支吞掉。
func TestExecErrorSocketPermissionDeniedMapping(t *testing.T) {
	c := &Client{}
	stderr := `permission denied while trying to connect to the Docker daemon socket at ` +
		`unix:///var/run/docker.sock: Get "http://%2Fvar%2Frun%2Fdocker.sock/v1.24/containers/json": ` +
		`dial unix /var/run/docker.sock: connect: permission denied`
	err := c.execError(exitStatusStub{}, stderr)
	var perr *OpError
	if !errors.As(err, &perr) {
		t.Fatalf("期望 *OpError，实际 %v", err)
	}
	if perr.Code != pluginapi.E_NEED_ROOT {
		t.Fatalf("sock 无权错误码=%q，期望 %q（不得为 E_UNSUPPORTED）", perr.Code, pluginapi.E_NEED_ROOT)
	}
	// ADR-0018 要求机器码之外还必须给可操作 hint：root 权限 + 内核提权通道；
	// 且严禁引导把插件加入 docker 组（该组等价 root，绕过提权审计）。
	for _, want := range []string{"root", "root_actions"} {
		if !strings.Contains(perr.Message, want) {
			t.Errorf("sock 无权提示缺少 %q：%s", want, perr.Message)
		}
	}
	if strings.Contains(perr.Message, "加入 docker 组") || strings.Contains(perr.Message, "docker 组") {
		t.Errorf("sock 无权提示不得引导加入 docker 组（与 ADR-0018 冲突）：%s", perr.Message)
	}
	// 走完 classify 再核对一次：协议层的 code / detail.reason / message 都要保住口径。
	papiErr := classify(err)
	if papiErr.Code != pluginapi.E_NEED_ROOT {
		t.Fatalf("协议层 sock 无权错误码=%q，期望 %q", papiErr.Code, pluginapi.E_NEED_ROOT)
	}
	if !strings.Contains(papiErr.Message, "root_actions") || strings.Contains(papiErr.Message, "docker 组") {
		t.Errorf("协议层 sock 无权提示不符 ADR-0018：%s", papiErr.Message)
	}
	if !strings.Contains(string(papiErr.Detail), ExecReasonNeedRoot) {
		t.Errorf("协议层 sock 无权 detail.reason 期望 %q，实际 %s", ExecReasonNeedRoot, papiErr.Detail)
	}
}

// TestExecErrorDaemonDownKeepsUnsupported 守护另一半：daemon 真没启动仍为 E_UNSUPPORTED，
// 不因上一个用例把权限分支前移而被误并。
func TestExecErrorDaemonDownKeepsUnsupported(t *testing.T) {
	c := &Client{}
	err := c.execError(exitStatusStub{}, "Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?")
	var perr *OpError
	if !errors.As(err, &perr) || perr.Code != pluginapi.E_UNSUPPORTED {
		t.Fatalf("daemon 不可达错误=%v，期望 E_UNSUPPORTED", err)
	}
}

// exitStatusStub 是 cmd.Run 失败的最小替身（execError 只做类型断言与 stderr 文本判别）。
type exitStatusStub struct{}

func (exitStatusStub) Error() string { return "exit status 1" }
