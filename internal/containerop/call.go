package containerop

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// Call 执行一个 container 方法：先校验方法名与参数，再调用执行面并结构化结果。
// opts 为运行期配置；exec 为 nil 时使用基于本机 docker CLI 的 Client。
func Call(method string, params json.RawMessage, opts Options, exec Executor) (json.RawMessage, *pluginapi.Error) {
	return NewRuntime(opts, exec).Call(method, params)
}

// Runtime 是 container 方法面的生命周期容器：持有执行面与运行期配置。
type Runtime struct {
	opts Options
	exec Executor
}

// NewRuntime 构造 Runtime；exec 为 nil 时使用真实 docker CLI 执行面。
func NewRuntime(opts Options, exec Executor) *Runtime {
	opts = opts.normalize()
	if opts.GOOS == "" {
		opts.GOOS = runtime.GOOS
	}
	if exec == nil {
		exec = NewClient(opts.DockerBin, opts.Timeout)
	}
	return &Runtime{opts: opts, exec: exec}
}

// Call 执行一个 container.<verb> 方法。
func (r *Runtime) Call(method string, params json.RawMessage) (json.RawMessage, *pluginapi.Error) {
	if r.opts.GOOS != "linux" {
		return nil, unsupported("container 插件只在 Linux 提供真实实现（当前 " + r.opts.GOOS + "）")
	}
	verb, ok := businessVerb(method)
	if !ok {
		return nil, notFound("未知方法: " + method)
	}
	switch verb {
	case "list":
		return r.callList(params)
	case "inspect":
		return r.callInspect(params)
	case "logs":
		return r.callLogs(params)
	case "start", "stop", "restart":
		return r.callAction(verb, params)
	case "stats":
		return r.callStats(params)
	default:
		return nil, notFound("未知方法: " + method)
	}
}

// methodPrefix 是方法名的插件域前缀：协议要求 `<plugin-id>.<action>`（PLUGIN-PROTOCOL §7.2）。
const methodPrefix = "container."

// businessVerb 把 container.<verb> 归一为动词名；域不是本插件 id 的一律拒绝。
func businessVerb(method string) (string, bool) {
	verb, ok := strings.CutPrefix(method, methodPrefix)
	if !ok {
		return "", false
	}
	switch verb {
	case "list", "inspect", "logs", "start", "stop", "restart", "stats":
		return verb, true
	default:
		return "", false
	}
}

// callList 处理 container.list：all=true 时包含已停止容器。
func (r *Runtime) callList(raw json.RawMessage) (json.RawMessage, *pluginapi.Error) {
	var p listParams
	if perr := decodeParams(raw, &p); perr != nil {
		return nil, perr
	}
	containers, err := r.exec.List(p.All)
	if err != nil {
		return nil, classify(err)
	}
	if containers == nil {
		containers = []ContainerEntry{}
	}
	return encode(ListResult{Containers: containers, Count: len(containers), All: p.All})
}

// callInspect 处理 container.inspect：校验 id 形状后读取精简详情。
func (r *Runtime) callInspect(raw json.RawMessage) (json.RawMessage, *pluginapi.Error) {
	var p inspectParams
	if perr := decodeParams(raw, &p); perr != nil {
		return nil, perr
	}
	if perr := ValidateID(p.ID); perr != nil {
		return nil, perr
	}
	result, err := r.exec.Inspect(p.ID)
	if err != nil {
		return nil, classify(err)
	}
	if result == nil {
		return nil, notFound("容器不存在: " + p.ID)
	}
	return encode(*result)
}

// callLogs 处理 container.logs：tail 缺省 100，范围 1..2000。
func (r *Runtime) callLogs(raw json.RawMessage) (json.RawMessage, *pluginapi.Error) {
	var p logsParams
	if perr := decodeParams(raw, &p); perr != nil {
		return nil, perr
	}
	if perr := ValidateID(p.ID); perr != nil {
		return nil, perr
	}
	tail := DefaultLogTail
	if p.Tail != nil {
		tail = *p.Tail
	}
	if tail < 1 || tail > MaxLogTail {
		return nil, invalid(fmt.Sprintf("tail 必须在 1..%d 之间", MaxLogTail))
	}
	result, err := r.exec.Logs(p.ID, tail)
	if err != nil {
		return nil, classify(err)
	}
	if result.ID == "" {
		result.ID = p.ID
	}
	if result.Lines == nil {
		result.Lines = []string{}
	}
	return encode(result)
}

// callAction 处理 start/stop/restart：stop/restart 必须 confirm=true，写操作前先过保护名单。
func (r *Runtime) callAction(verb string, raw json.RawMessage) (json.RawMessage, *pluginapi.Error) {
	var p actionParams
	if perr := decodeParams(raw, &p); perr != nil {
		return nil, perr
	}
	if perr := ValidateID(p.ID); perr != nil {
		return nil, perr
	}
	if verb != "start" && !p.Confirm {
		return nil, invalid(verb + " 需要二次确认：请传 confirm=true 后重试")
	}
	timeout := -1
	if p.Timeout != nil {
		if *p.Timeout < 0 || *p.Timeout > MaxStopTimeout {
			return nil, invalid(fmt.Sprintf("timeout 必须在 0..%d 秒之间", MaxStopTimeout))
		}
		timeout = *p.Timeout
	}
	ref, err := r.exec.Resolve(p.ID)
	if err != nil {
		return nil, classify(err)
	}
	if perr := CheckProtected(ref, r.opts.Protected); perr != nil {
		return nil, perr
	}
	switch verb {
	case "start":
		err = r.exec.Start(ref.ID)
	case "stop":
		err = r.exec.Stop(ref.ID, timeout)
	case "restart":
		err = r.exec.Restart(ref.ID, timeout)
	}
	if err != nil {
		return nil, classify(err)
	}
	// 动作已成功：状态是尽力补充信息，取不到（如 --rm 容器已被删除）不把成功改判为失败。
	state := ""
	if inspect, inspectErr := r.exec.Inspect(ref.ID); inspectErr == nil && inspect != nil {
		state = inspect.State.Status
	}
	return encode(ActionResult{ID: ref.ID, Action: verb, OK: true, State: state})
}

// callStats 处理 container.stats：一次无流式采样。
func (r *Runtime) callStats(raw json.RawMessage) (json.RawMessage, *pluginapi.Error) {
	var p inspectParams
	if perr := decodeParams(raw, &p); perr != nil {
		return nil, perr
	}
	if perr := ValidateID(p.ID); perr != nil {
		return nil, perr
	}
	result, err := r.exec.Stats(p.ID)
	if err != nil {
		return nil, classify(err)
	}
	return encode(result)
}

// classify 把执行面错误映射为协议错误码：四类故障各有对应，其次 OpError，最后 E_INTERNAL。
func classify(err error) *pluginapi.Error {
	var faultErr *FaultError
	if errors.As(err, &faultErr) {
		switch faultErr.Kind {
		case FaultKill:
			return upstream("docker 进程被信号 9 终止: " + faultErr.Message)
		case FaultTimeout:
			return upstream("调用超时: " + faultErr.Message)
		case FaultDisk:
			return internalErr("磁盘满: " + faultErr.Message)
		case FaultCgroup:
			return unsupported("cgroup 限额触发: " + faultErr.Message)
		}
	}
	var opErr *OpError
	if errors.As(err, &opErr) {
		return &pluginapi.Error{Code: opErr.Code, Message: opErr.Message}
	}
	return internalErr("执行面失败: " + err.Error())
}

func invalid(msg string) *pluginapi.Error {
	return &pluginapi.Error{Code: pluginapi.E_INVALID, Message: msg}
}

func notFound(msg string) *pluginapi.Error {
	return &pluginapi.Error{Code: pluginapi.E_NOTFOUND, Message: msg}
}

func unauthorized(msg string) *pluginapi.Error {
	return &pluginapi.Error{Code: pluginapi.E_UNAUTHORIZED, Message: msg}
}

func unsupported(msg string) *pluginapi.Error {
	return &pluginapi.Error{Code: pluginapi.E_UNSUPPORTED, Message: msg}
}

func upstream(msg string) *pluginapi.Error {
	return &pluginapi.Error{Code: pluginapi.E_UPSTREAM, Message: msg}
}

func internalErr(msg string) *pluginapi.Error {
	return &pluginapi.Error{Code: pluginapi.E_INTERNAL, Message: msg}
}

// decodeParams 校验 params 是 JSON 对象并反序列化到目标形状；空值/null 视为空对象。
func decodeParams(raw json.RawMessage, target any) *pluginapi.Error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	if trimmed[0] != '{' {
		return invalid("params 必须是 JSON 对象")
	}
	if err := json.Unmarshal(trimmed, target); err != nil {
		return invalid("params 解析失败: " + err.Error())
	}
	return nil
}

// encode 把结果编码为 json.RawMessage；序列化失败归 E_INTERNAL。
func encode(v any) (json.RawMessage, *pluginapi.Error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, internalErr("序列化结果失败: " + err.Error())
	}
	return data, nil
}
