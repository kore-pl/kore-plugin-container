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
	// exec / shell / attach 明确不做：即使调用方直接构造方法名，也在触达 docker 前拒绝。
	if isDeniedVerb(verb) {
		return nil, unsupported("container 插件不提供 " + verb +
			"：插件进程以低权身份运行，容器内命令执行等同绕过插件权限模型（ADR-0018）；容器编排不在 v1 范围（G-11）")
	}
	switch verb {
	case "list":
		return r.callList(params)
	case "exec":
		return r.callExec(params)
	case "inspect":
		return r.callInspect(params)
	case "logs":
		return r.callLogs(params)
	case "start", "stop", "restart":
		return r.callAction(verb, params)
	case "stats":
		return r.callStats(params)
	case "images":
		return r.callImages()
	case "pull":
		return r.callPull(params)
	case "rmi":
		return r.callRemoveImage(params)
	case "update":
		return r.callUpdate(params)
	case "health":
		return r.callHealth(params)
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
	case "list", "inspect", "logs", "start", "stop", "restart", "stats",
		"images", "pull", "rmi", "update", "health",
		"exec", "shell", "attach":
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
	// protected 是读操作可以直接给的前端提示：命中保护名单的行在前端锁标 + 禁用启停。
	for i := range containers {
		containers[i].Protected = CheckProtected(
			ContainerRef{ID: containers[i].ID, Name: containers[i].Name}, r.opts.Protected) != nil
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

// callLogs 处理 container.logs：tail 缺省 200，范围 1..2000。
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
	// since 原样作为 docker logs --since 的一个 argv 元素（固定 argv 不经 shell，无注入面），
	// 但仍限制形状，拒绝空白/控制字符等显然的误传。
	since := p.Since
	if sincePerr := ValidateSince(since); sincePerr != nil {
		return nil, sincePerr
	}
	result, err := r.exec.Logs(p.ID, tail, since)
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

// callImages 处理 container.images：只读列出本机镜像，不触达网络。
func (r *Runtime) callImages() (json.RawMessage, *pluginapi.Error) {
	images, err := r.exec.Images()
	if err != nil {
		return nil, classify(err)
	}
	if images == nil {
		images = []ImageEntry{}
	}
	return encode(ImagesResult{Images: images, Count: len(images)})
}

// callPull 处理 container.pull：必须 confirm=true（显式用户触发），ref 形状先校验再触达网络。
func (r *Runtime) callPull(raw json.RawMessage) (json.RawMessage, *pluginapi.Error) {
	var p pullParams
	if perr := decodeParams(raw, &p); perr != nil {
		return nil, perr
	}
	if perr := ValidateImageRef(p.Ref); perr != nil {
		return nil, perr
	}
	if !p.Confirm {
		return nil, invalid("pull 会联网拉取镜像，需要二次确认：请传 confirm=true 后重试")
	}
	result, err := r.exec.Pull(p.Ref)
	if err != nil {
		return nil, classify(err)
	}
	return encode(result)
}

// callRemoveImage 处理 container.rmi：必须 confirm=true，force 可选。
func (r *Runtime) callRemoveImage(raw json.RawMessage) (json.RawMessage, *pluginapi.Error) {
	var p rmiParams
	if perr := decodeParams(raw, &p); perr != nil {
		return nil, perr
	}
	if perr := ValidateImageRef(p.Ref); perr != nil {
		return nil, perr
	}
	if !p.Confirm {
		return nil, invalid("rmi 会删除镜像，需要二次确认：请传 confirm=true 后重试")
	}
	if err := r.exec.RemoveImage(p.Ref, p.Force); err != nil {
		return nil, classify(err)
	}
	return encode(ImageRemoveResult{Ref: p.Ref, OK: true, Force: p.Force})
}

// callUpdate 处理 container.update：改内存 / CPU 限额，必须 confirm=true；
// 写入前先解析容器真实身份并过保护名单，与 start/stop/restart 同口径。
func (r *Runtime) callUpdate(raw json.RawMessage) (json.RawMessage, *pluginapi.Error) {
	var p updateParams
	if perr := decodeParams(raw, &p); perr != nil {
		return nil, perr
	}
	if perr := ValidateID(p.ID); perr != nil {
		return nil, perr
	}
	if !p.Confirm {
		return nil, invalid("update 会修改容器资源限额，需要二次确认：请传 confirm=true 后重试")
	}
	if p.MemoryBytes == nil && p.CPUs == nil {
		return nil, invalid("update 至少需要 memory_bytes 或 cpus 之一")
	}
	if p.MemoryBytes != nil && (*p.MemoryBytes < MinUpdateMemoryBytes || *p.MemoryBytes > MaxUpdateMemoryBytes) {
		return nil, invalid(fmt.Sprintf("memory_bytes 必须在 %d..%d 字节之间（6 MiB..1 TiB）",
			MinUpdateMemoryBytes, MaxUpdateMemoryBytes))
	}
	if p.CPUs != nil && (*p.CPUs < MinUpdateCPUs || *p.CPUs > MaxUpdateCPUs) {
		return nil, invalid(fmt.Sprintf("cpus 必须在 %.2f..%.0f 之间", MinUpdateCPUs, MaxUpdateCPUs))
	}
	ref, err := r.exec.Resolve(p.ID)
	if err != nil {
		return nil, classify(err)
	}
	if perr := CheckProtected(ref, r.opts.Protected); perr != nil {
		return nil, perr
	}
	if err := r.exec.Update(ref.ID, ContainerUpdate{MemoryBytes: p.MemoryBytes, CPUs: p.CPUs}); err != nil {
		return nil, classify(err)
	}
	return encode(UpdateResult{ID: ref.ID, OK: true, MemoryBytes: p.MemoryBytes, CPUs: p.CPUs})
}

// callHealth 处理 container.health：投影 inspect 已有的 state/health 字段，不额外起子进程。
func (r *Runtime) callHealth(raw json.RawMessage) (json.RawMessage, *pluginapi.Error) {
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
	return encode(HealthResult{
		ID:           result.ID,
		Status:       result.State.Status,
		Health:       result.Health,
		Running:      result.State.Running,
		RestartCount: result.State.RestartCount,
		StartedAt:    result.State.StartedAt,
	})
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
		switch opErr.Code {
		case pluginapi.E_UNSUPPORTED:
			// docker CLI 缺失 / daemon 不可达：显式 docker_missing，供前端区分。
			return reasonError(opErr.Code, opErr.Message, ExecReasonDockerMissing)
		case pluginapi.E_NEED_ROOT:
			// ADR-0018 降级矩阵：docker.sock 无权访问 → E_NEED_ROOT。
			// reason 与 docker_missing 分开，前端据此展示「去授权」而不是「去装 docker」。
			return reasonError(opErr.Code, opErr.Message, ExecReasonNeedRoot)
		}
		return &pluginapi.Error{Code: opErr.Code, Message: opErr.Message}
	}
	return internalErr("执行面失败: " + err.Error())
}

func invalid(msg string) *pluginapi.Error {
	return &pluginapi.Error{Code: pluginapi.E_INVALID, Message: msg}
}

// reasonError 构造带显式 detail.reason 的协议错误（ADR-0026 前端契约：不靠 message 字符串判别）。
// reason 取值见 execReason* 常量；Detail 是显式 JSON 字段，不用 any/object（G-24）。
func reasonError(code, msg, reason string) *pluginapi.Error {
	return &pluginapi.Error{Code: code, Message: msg,
		Detail: json.RawMessage(`{"reason":"` + reason + `"}`)}
}

// exec 错误的可判别原因（detail.reason）。
const (
	ExecReasonDenylist        = "denylist"
	ExecReasonEscapeFlag      = "escape_flag"
	ExecReasonConfirmRequired = "confirm_required"
	ExecReasonDockerMissing   = "docker_missing"
	// ExecReasonNeedRoot 表示 docker.sock 无权访问（ADR-0018：需宿主 root 经提权通道授权）。
	ExecReasonNeedRoot = "need_root"
)

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
