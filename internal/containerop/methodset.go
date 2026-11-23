package containerop

import "sort"

// 方法能力映射是本插件方法面的唯一事实源：
//   - info.json 的 method_capabilities 必须与本表逐条一致（main 包一致性测试守护）；
//   - 读方法只要求 system:read，写方法要求 system:admin（内核按方法取用，见 kore
//     registry/validate.go 与 httpapi/invoke_capability.go）。
//
// 注意：这里只收录真正实现的方法；shell / attach 属明确不做（见 deniedMethods）。
// exec 0.5.0 起提供**一次性命令**（固定 argv、拒绝清单、非 root 默认），交互式仍不做。
var methodCapabilities = map[string]string{
	"list":    "system:read",
	"inspect": "system:read",
	"logs":    "system:read",
	"stats":   "system:read",
	"images":  "system:read",
	"health":  "system:read",
	"exec":    "system:admin",
	"start":   "system:admin",
	"stop":    "system:admin",
	"restart": "system:admin",
	"pull":    "system:admin",
	"rmi":     "system:admin",
	"update":  "system:admin",
}

// deniedMethods 是明确不提供的入口。
// 理由：attach / shell 是**交互式**通道——插件没有 PTY 转发，做了就是半成品，
// 交互式终端归 terminal 插件；容器编排属 G-11 不做项。exec（一次性命令）0.5.0 起
// 由拒绝清单 + 非 root 默认 + root 需 confirm 三道闸门收口（ADR-0018 低权模型不变）。
var deniedMethods = []string{"attach", "shell"}

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
