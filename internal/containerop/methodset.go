package containerop

import "sort"

// 方法能力映射是本插件方法面的唯一事实源：
//   - info.json 的 method_capabilities 必须与本表逐条一致（main 包一致性测试守护）；
//   - 读方法只要求 system:read，写方法要求 system:admin（内核按方法取用，见 kore
//     registry/validate.go 与 httpapi/invoke_capability.go）。
//
// 注意：这里只收录真正实现的方法；exec / shell / attach 属明确不做（见 DeniedMethods）。
var methodCapabilities = map[string]string{
	"list":    "system:read",
	"inspect": "system:read",
	"logs":    "system:read",
	"stats":   "system:read",
	"images":  "system:read",
	"health":  "system:read",
	"start":   "system:admin",
	"stop":    "system:admin",
	"restart": "system:admin",
	"pull":    "system:admin",
	"rmi":     "system:admin",
	"update":  "system:admin",
}

// deniedMethods 是明确不提供的容器内命令执行入口。
// 理由（ADR-0018 / G-11）：插件进程必须保持低权；exec 等于把容器当宿主 shell 用，
// 绕过插件权限模型与内核提权代理；容器编排本身也在 G-11 的不做清单内。
var deniedMethods = []string{"attach", "exec", "shell"}

// MethodCapabilities 返回方法名 → 内核特权能力的副本（调用方不可改内部表）。
func MethodCapabilities() map[string]string {
	copied := make(map[string]string, len(methodCapabilities))
	for method, capability := range methodCapabilities {
		copied[method] = capability
	}
	return copied
}

// SupportedMethods 返回已实现业务方法名（不带 container. 前缀），按字典序排列。
func SupportedMethods() []string {
	methods := make([]string, 0, len(methodCapabilities))
	for method := range methodCapabilities {
		methods = append(methods, method)
	}
	sort.Strings(methods)
	return methods
}

// DeniedMethods 返回明确拒绝执行的业务方法名，按字典序排列。
func DeniedMethods() []string {
	methods := append([]string(nil), deniedMethods...)
	sort.Strings(methods)
	return methods
}

// isDeniedVerb 判断业务动词是否属于明确拒绝的方法。
func isDeniedVerb(verb string) bool {
	for _, denied := range deniedMethods {
		if verb == denied {
			return true
		}
	}
	return false
}
