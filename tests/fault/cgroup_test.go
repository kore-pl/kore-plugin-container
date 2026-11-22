//go:build linux

package fault

import (
	"testing"

	"github.com/pengtang1024/kore-plugin-container/internal/containerop"
	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// TestCgroupFault 注入 cgroup 限额，期望七个方法返回 E_UNSUPPORTED。
func TestCgroupFault(t *testing.T) {
	assertFault(t, containerop.FaultCgroup, pluginapi.E_UNSUPPORTED, "cgroup")
}
