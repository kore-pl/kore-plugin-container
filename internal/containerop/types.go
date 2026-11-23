// Package containerop 实现 container 插件的方法面：容器标识与保护名单校验，
// 通过本机 docker CLI（固定 argv、不经 shell）执行列表/详情/日志/启停/统计，
// 并把输出收敛为显式结构。
//
// 依赖边界（G-17）：只依赖 kore/pkg/pluginapi 与 Go 标准库，不引入 Docker SDK，
// 也不内置 docker 二进制；CLI 由宿主机 PATH 提供，可用 KORE_DOCKER_BIN 指到绝对路径。
package containerop

import (
	"fmt"
	"time"
)

// 运行期边界常量。
const (
	// DefaultDockerBin 是缺省的 docker CLI 名（走 PATH 查找）。
	DefaultDockerBin = "docker"
	// EnvDockerBin / EnvProtectedContainers 是插件读取的环境变量名。
	EnvDockerBin           = "KORE_DOCKER_BIN"
	EnvProtectedContainers = "KORE_PROTECTED_CONTAINERS"
	// DefaultDockerTimeout 是单条 docker 命令的超时：inspect 在容器多/挂载多时可能偏慢。
	DefaultDockerTimeout = 20 * time.Second
	// DefaultLogTail / MaxLogTail 是 container.logs 的缺省与硬上限行数。
	DefaultLogTail = 200
	MaxLogTail     = 2000
	// MaxLogLineBytes 是单行日志的硬上限（含被截断的行）。
	MaxLogLineBytes = 8 << 10
	// MaxLogBytes 是一次 logs 采集的总字节预算，超出即丢弃并标记 truncated。
	MaxLogBytes = 2 << 20
	// MaxStopTimeout 是 stop/restart 的优雅退出等待上限（秒）。
	MaxStopTimeout = 300
)

// FaultKind 标识一次故障注入的类别（与 process / db-mysql 插件同一口径）。
type FaultKind string

// 四类故障注入类别。
const (
	FaultNone    FaultKind = ""
	FaultKill    FaultKind = "kill"
	FaultTimeout FaultKind = "timeout"
	FaultDisk    FaultKind = "disk"
	FaultCgroup  FaultKind = "cgroup"
)

// FaultError 是执行面注入的故障错误，classify 将其映射为协议错误码。
type FaultError struct {
	Kind    FaultKind
	Message string
}

// Error 实现 error 接口，输出 "<类别>: <说明>"。
func (e *FaultError) Error() string { return fmt.Sprintf("%s: %s", e.Kind, e.Message) }

// NewFaultError 构造给定类别的故障错误，供故障注入执行面使用。
func NewFaultError(kind FaultKind, message string) error {
	return &FaultError{Kind: kind, Message: message}
}

// OpError 是执行面带协议错误码的错误，classify 优先用它，避免错误信息二次加工时丢码。
type OpError struct {
	Code    string
	Message string
}

// Error 实现 error 接口。
func (e *OpError) Error() string { return e.Code + ": " + e.Message }

// opErrorf 构造带协议错误码的执行面错误。
func opErrorf(code, format string, args ...any) *OpError {
	return &OpError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// ContainerEntry 是 container.list 返回的单条容器记录；字段显式列出，不使用 any / object。
type ContainerEntry struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Image      string `json:"image"`
	State      string `json:"state"`
	Status     string `json:"status"`
	Ports      string `json:"ports"`
	Created    string `json:"created"`
	RunningFor string `json:"running_for"`
	// Protected 标记该容器命中保护名单：前端据此展示锁标并禁用启停按钮。
	Protected bool `json:"protected"`
}

// ListResult 是 container.list 的结构。
type ListResult struct {
	Containers []ContainerEntry `json:"containers"`
	Count      int              `json:"count"`
	All        bool             `json:"all"`
}

// InspectState 是 container.inspect 的 state 子结构。
type InspectState struct {
	Status       string `json:"status"`
	Running      bool   `json:"running"`
	StartedAt    string `json:"started_at"`
	RestartCount int64  `json:"restart_count"`
}

// InspectConfig 是 container.inspect 的 config 子结构；env 的值已脱敏。
type InspectConfig struct {
	Env    []string          `json:"env"`
	Cmd    []string          `json:"cmd"`
	Labels map[string]string `json:"labels"`
}

// InspectPort 是一条端口映射；容器只暴露未发布时 host_ip/host_port 为空串。
type InspectPort struct {
	ContainerPort string `json:"container_port"`
	Protocol      string `json:"protocol"`
	HostIP        string `json:"host_ip"`
	HostPort      string `json:"host_port"`
}

// InspectNetwork 是 container.inspect 的 network 子结构。
type InspectNetwork struct {
	IP    string        `json:"ip"`
	Ports []InspectPort `json:"ports"`
}

// InspectMount 是一条挂载记录。
type InspectMount struct {
	Type        string `json:"type"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Mode        string `json:"mode"`
	RW          bool   `json:"rw"`
}

// InspectResources 是容器的资源限额。
type InspectResources struct {
	MemoryLimit   int64  `json:"memory_limit"`
	CPUQuota      int64  `json:"cpu_quota"`
	RestartPolicy string `json:"restart_policy"`
}

// InspectResult 是 container.inspect 的精简结构；health 为空串表示未配置健康检查。
type InspectResult struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	Image     string           `json:"image"`
	State     InspectState     `json:"state"`
	Config    InspectConfig    `json:"config"`
	Network   InspectNetwork   `json:"network"`
	Mounts    []InspectMount   `json:"mounts"`
	Resources InspectResources `json:"resources"`
	Health    string           `json:"health"`
}

// LogsResult 是 container.logs 的结构；lines 已按单行上限截断，truncated 表示有截断。
type LogsResult struct {
	ID        string   `json:"id"`
	Lines     []string `json:"lines"`
	Truncated bool     `json:"truncated"`
}

// ActionResult 是 start/stop/restart 的结构；state 是动作后的尽力状态（取不到为空串）。
type ActionResult struct {
	ID     string `json:"id"`
	Action string `json:"action"`
	OK     bool   `json:"ok"`
	State  string `json:"state"`
}

// MemoryStats 是 container.stats 的内存子结构（字节）。
type MemoryStats struct {
	Usage   int64   `json:"usage"`
	Limit   int64   `json:"limit"`
	Percent float64 `json:"percent"`
}

// NetStats 是容器网络累计收发字节。
type NetStats struct {
	Input  int64 `json:"input"`
	Output int64 `json:"output"`
}

// BlockStats 是容器块设备累计读写字节。
type BlockStats struct {
	Read  int64 `json:"read"`
	Write int64 `json:"write"`
}

// StatsResult 是 container.stats 的结构。
type StatsResult struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	CPUPercent float64     `json:"cpu_percent"`
	Memory     MemoryStats `json:"memory"`
	Network    NetStats    `json:"network"`
	Block      BlockStats  `json:"block"`
	PIDs       int         `json:"pids"`
}

// ContainerRef 是容器真实身份（完整 ID + 去前导斜杠的名称），用于保护名单比对。
type ContainerRef struct {
	ID   string
	Name string
}

// Executor 是容器操作执行面：真实实现是本机 docker CLI，测试可注入故障或假数据。
type Executor interface {
	// List 列出容器；all=true 时包含已停止容器。
	List(all bool) ([]ContainerEntry, error)
	// Inspect 读取单个容器的精简详情。
	Inspect(id string) (*InspectResult, error)
	// Logs 读取最近 tail 行日志（stdout+stderr 合并）；since 非空时只取该时间之后的日志。
	Logs(id string, tail int, since string) (LogsResult, error)
	// Start 启动容器。
	Start(id string) error
	// Stop 停止容器；timeout<0 表示使用 docker 缺省优雅退出时间。
	Stop(id string, timeout int) error
	// Restart 重启容器；timeout<0 表示使用 docker 缺省优雅退出时间。
	Restart(id string, timeout int) error
	// Stats 读取一次无流式统计。
	Stats(id string) (StatsResult, error)
	// Resolve 解析容器的完整 ID 与名称，用于保护名单校验。
	Resolve(id string) (ContainerRef, error)
}

// Options 是 Runtime 的运行期配置：由 main 从环境变量组装，测试用零值 + 注入项。
type Options struct {
	// DockerBin 是 docker CLI 名或绝对路径；空串取 DefaultDockerBin。
	DockerBin string
	// Timeout 是单条 docker 命令超时；<=0 取 DefaultDockerTimeout。
	Timeout time.Duration
	// Protected 是保护名单（容器名称 / 完整 ID / ≥12 位短 ID），命中即拒绝写操作。
	Protected []string
	// GOOS 为空时取 runtime.GOOS；测试可注入 "darwin" 验证 E_UNSUPPORTED。
	GOOS string
}

// normalize 填充默认值。
func (o Options) normalize() Options {
	if o.DockerBin == "" {
		o.DockerBin = DefaultDockerBin
	}
	if o.Timeout <= 0 {
		o.Timeout = DefaultDockerTimeout
	}
	return o
}

// listParams 是 container.list 入参（all 可选）。
type listParams struct {
	All bool `json:"all"`
}

// inspectParams 是 container.inspect 入参。
type inspectParams struct {
	ID string `json:"id"`
}

// logsParams 是 container.logs 入参；tail 缺省 100，since 可选（docker --since 原样传参）。
type logsParams struct {
	ID    string `json:"id"`
	Tail  *int   `json:"tail"`
	Since string `json:"since"`
}

// actionParams 是 start/stop/restart 的共用入参。
type actionParams struct {
	ID      string `json:"id"`
	Confirm bool   `json:"confirm"`
	Timeout *int   `json:"timeout"`
}
