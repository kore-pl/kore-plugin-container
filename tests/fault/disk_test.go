//go:build linux

package fault

import (
	"testing"

	"github.com/pengtang1024/kore-plugin-container/internal/containerop"
	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// TestDiskFault 注入磁盘满，期望七个方法返回 E_INTERNAL。
func TestDiskFault(t *testing.T) {
	assertFault(t, containerop.FaultDisk, pluginapi.E_INTERNAL, "磁盘满")
}
