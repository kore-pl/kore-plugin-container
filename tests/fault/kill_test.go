//go:build linux

package fault

import (
	"encoding/json"
	"testing"

	"github.com/pengtang1024/kore-plugin-container/internal/containerop"
	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// TestKillFault 注入 kill -9（进程被信号终止），期望七个方法返回 E_UPSTREAM。
func TestKillFault(t *testing.T) {
	assertFault(t, containerop.FaultKill, pluginapi.E_UPSTREAM, "信号 9")
}

// TestKillFaultContract 单独验证 kill 故障的错误体结构完整（真实调用，非空壳）。
func TestKillFaultContract(t *testing.T) {
	_, perr := containerop.Call("container.inspect", json.RawMessage(`{"id":"web"}`),
		containerop.Options{GOOS: "linux"}, faultExecutor{kind: containerop.FaultKill})
	verifyErrorContract(t, perr)
}

// verifyErrorContract 断言错误体是标准协议结构（code 与 message 均非空）。
func verifyErrorContract(t *testing.T, perr *pluginapi.Error) {
	t.Helper()
	if perr == nil {
		t.Fatal("错误体为 nil")
	}
	if perr.Code == "" || perr.Message == "" {
		t.Fatalf("错误体字段不完整: %+v", perr)
	}
}
