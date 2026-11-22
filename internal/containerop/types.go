// Package containerop 实现 container 插件的方法面：标识校验与结构化结果。
// 本包只做逻辑校验与执行面调用，不真连 docker/podman daemon（骨架口径 / G-05）。
package containerop

import "fmt"

// maxNameLen 是容器名称（或短标识）的长度上限，与主流运行时一致。
const maxNameLen = 128

// FaultKind 标识一次故障注入的类别；骨架只覆盖 kill/timeout 两类基础故障。
type FaultKind string

const (
	FaultNone    FaultKind = ""
	FaultKill    FaultKind = "kill"
	FaultTimeout FaultKind = "timeout"
)

// FaultError 是执行面注入的故障错误，Call 将其映射为协议错误码。
type FaultError struct {
	Kind    FaultKind
	Message string
}

// Error 实现 error 接口，输出 "<类别>: <说明>"。
func (e *FaultError) Error() string { return fmt.Sprintf("%s: %s", e.Kind, e.Message) }

// NewFaultError 构造给定类别的故障错误。
func NewFaultError(kind FaultKind, message string) *FaultError {
	return &FaultError{Kind: kind, Message: message}
}

// Container 是容器列表中的一项逻辑条目（结构化占位，不来自真实 daemon）。
type Container struct {
	ID     string   `json:"id"`
	Names  []string `json:"names"`
	Image  string   `json:"image"`
	Status string   `json:"status"`
}

// Executor 是容器操作的执行面；骨架实现为 noop，测试可注入故障执行器。
// 执行面不接收 daemon 地址——本插件不内置、不直连任何容器运行时（G-05）。
type Executor interface {
	List() ([]Container, error)
	Inspect(id string) (*Container, error)
}

// inspectParams / listParams 是方法的显式入参形状。
type inspectParams struct {
	ID string `json:"id"`
}

type listParams struct{}

// ListResult 是 container.list 的结构化结果。managed=false 表示未连接任何真实运行时。
type ListResult struct {
	Containers []Container `json:"containers"`
	Count      int         `json:"count"`
	Managed    bool        `json:"managed"`
}

// InspectResult 是 container.inspect 的结构化结果；applied 表示执行面是否真正应用。
type InspectResult struct {
	ID      string `json:"id"`
	IDKind  string `json:"id_kind"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	Status  string `json:"status"`
	Applied bool   `json:"applied"`
}
