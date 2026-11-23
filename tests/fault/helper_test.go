//go:build linux

// Package fault 对 container 插件做四类故障注入（kill / timeout / disk / cgroup）：
// 每个用例把可控执行面替换为始终返回 FaultError 的实现，断言十二个方法都把执行面
// 故障映射成结构化协议错误码，而不是 panic 或伪成功。
package fault

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pengtang1024/kore-plugin-container/internal/containerop"
)

// faultExecutor 按注入类别让每个执行面方法都失败。
type faultExecutor struct {
	kind containerop.FaultKind
}

func (f faultExecutor) List(bool) ([]containerop.ContainerEntry, error) {
	return nil, containerop.NewFaultError(f.kind, "list 执行面故障")
}

func (f faultExecutor) Inspect(string) (*containerop.InspectResult, error) {
	return nil, containerop.NewFaultError(f.kind, "inspect 执行面故障")
}

func (f faultExecutor) Logs(string, int, string) (containerop.LogsResult, error) {
	return containerop.LogsResult{}, containerop.NewFaultError(f.kind, "logs 执行面故障")
}

func (f faultExecutor) Start(string) error {
	return containerop.NewFaultError(f.kind, "start 执行面故障")
}

func (f faultExecutor) Exec(containerop.ExecRequest) (containerop.ExecResult, error) {
	return containerop.ExecResult{}, containerop.NewFaultError(f.kind, "exec 执行面故障")
}

func (f faultExecutor) Stop(string, int) error {
	return containerop.NewFaultError(f.kind, "stop 执行面故障")
}

func (f faultExecutor) Restart(string, int) error {
	return containerop.NewFaultError(f.kind, "restart 执行面故障")
}

func (f faultExecutor) Stats(string) (containerop.StatsResult, error) {
	return containerop.StatsResult{}, containerop.NewFaultError(f.kind, "stats 执行面故障")
}

func (f faultExecutor) Images() ([]containerop.ImageEntry, error) {
	return nil, containerop.NewFaultError(f.kind, "images 执行面故障")
}

func (f faultExecutor) Pull(string) (containerop.PullResult, error) {
	return containerop.PullResult{}, containerop.NewFaultError(f.kind, "pull 执行面故障")
}

func (f faultExecutor) RemoveImage(string, bool) error {
	return containerop.NewFaultError(f.kind, "rmi 执行面故障")
}

func (f faultExecutor) Update(string, containerop.ContainerUpdate) error {
	return containerop.NewFaultError(f.kind, "update 执行面故障")
}

func (f faultExecutor) Resolve(string) (containerop.ContainerRef, error) {
	return containerop.ContainerRef{}, containerop.NewFaultError(f.kind, "resolve 执行面故障")
}

// methods 是故障注入覆盖的七个方法及其合法参数。
var methods = []struct {
	name   string
	method string
	params string
}{
	{"list", "container.list", `{"all":true}`},
	{"inspect", "container.inspect", `{"id":"web"}`},
	{"logs", "container.logs", `{"id":"web","tail":10}`},
	{"start", "container.start", `{"id":"web"}`},
	{"stop", "container.stop", `{"id":"web","confirm":true}`},
	{"restart", "container.restart", `{"id":"web","confirm":true}`},
	{"stats", "container.stats", `{"id":"web"}`},
	{"images", "container.images", `{}`},
	{"health", "container.health", `{"id":"web"}`},
	{"pull", "container.pull", `{"ref":"nginx:alpine","confirm":true}`},
	{"rmi", "container.rmi", `{"ref":"nginx:alpine","confirm":true}`},
	{"update", "container.update", `{"id":"web","memory_bytes":134217728,"confirm":true}`},
}

// assertFault 对十二个方法注入同一类故障，断言 nil 结果 + 预期错误码与类别关键词。
func assertFault(t *testing.T, kind containerop.FaultKind, wantCode, wantHint string) {
	t.Helper()
	for _, m := range methods {
		t.Run(m.name, func(t *testing.T) {
			result, perr := containerop.Call(m.method, json.RawMessage(m.params),
				containerop.Options{GOOS: "linux"}, faultExecutor{kind: kind})
			if perr == nil {
				t.Fatalf("%s 注入 %s 后未返回错误（结果=%s）", m.method, kind, result)
			}
			if result != nil {
				t.Fatalf("%s 出错时不应带结果: %s", m.method, result)
			}
			if perr.Code != wantCode {
				t.Fatalf("%s 错误码=%q，期望 %q（message=%q）", m.method, perr.Code, wantCode, perr.Message)
			}
			if !strings.Contains(perr.Message, wantHint) {
				t.Fatalf("%s 错误信息 %q 未含类别关键词 %q", m.method, perr.Message, wantHint)
			}
		})
	}
}

// healthyExecutor 是无故障执行面：证明故障断言不是"永远失败"的假通过。
type healthyExecutor struct{}

func (healthyExecutor) List(bool) ([]containerop.ContainerEntry, error) {
	return []containerop.ContainerEntry{{ID: "web", Name: "web", State: "running"}}, nil
}

func (healthyExecutor) Inspect(string) (*containerop.InspectResult, error) {
	return &containerop.InspectResult{ID: strings.Repeat("a", 64), Name: "web", State: containerop.InspectState{Status: "running"}}, nil
}

func (healthyExecutor) Logs(string, int, string) (containerop.LogsResult, error) {
	return containerop.LogsResult{ID: "web", Lines: []string{"ok"}}, nil
}

func (healthyExecutor) Start(string) error { return nil }

func (healthyExecutor) Stop(string, int) error { return nil }

func (healthyExecutor) Restart(string, int) error { return nil }

func (healthyExecutor) Stats(string) (containerop.StatsResult, error) {
	return containerop.StatsResult{ID: "web", PIDs: 1}, nil
}

func (healthyExecutor) Images() ([]containerop.ImageEntry, error) {
	return []containerop.ImageEntry{{ID: "0123456789ab", Repository: "nginx", Tag: "alpine"}}, nil
}

func (healthyExecutor) Pull(string) (containerop.PullResult, error) {
	return containerop.PullResult{Ref: "nginx:alpine", OK: true, Message: "pulled"}, nil
}

func (healthyExecutor) RemoveImage(string, bool) error { return nil }

func (healthyExecutor) Update(string, containerop.ContainerUpdate) error { return nil }

func (healthyExecutor) Exec(containerop.ExecRequest) (containerop.ExecResult, error) {
	return containerop.ExecResult{ExitCode: 0, Stdout: "ok"}, nil
}

func (healthyExecutor) Resolve(string) (containerop.ContainerRef, error) {
	return containerop.ContainerRef{ID: strings.Repeat("a", 64), Name: "web"}, nil
}

// sanityNoFault 验证无故障时十二个方法均正常返回。
func sanityNoFault(t *testing.T) {
	t.Helper()
	for _, m := range methods {
		result, perr := containerop.Call(m.method, json.RawMessage(m.params),
			containerop.Options{GOOS: "linux"}, healthyExecutor{})
		if perr != nil {
			t.Fatalf("无故障时 %s 意外返回错误: %+v", m.method, perr)
		}
		if len(result) == 0 {
			t.Fatalf("无故障时 %s 返回空结果", m.method)
		}
	}
}
