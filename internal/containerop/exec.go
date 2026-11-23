package containerop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// execParams 是 container.exec 入参：cmd 是**字符串数组**，逐个作为 argv 传递，
// 不经过任何 shell 解释（无管道/重定向/命令替换面）。
type execParams struct {
	ID        string   `json:"id"`
	Cmd       []string `json:"cmd"`
	User      string   `json:"user,omitempty"`
	Workdir   string   `json:"workdir,omitempty"`
	TimeoutMS int      `json:"timeout_ms,omitempty"`
	Confirm   bool     `json:"confirm,omitempty"`
}

// execResult 是 container.exec 结构；exit_code=-1 表示被超时终止。
type execResult struct {
	Schema          string   `json:"schema"`
	ID              string   `json:"id"`
	Cmd             []string `json:"cmd"`
	ExitCode        int      `json:"exit_code"`
	TimedOut        bool     `json:"timed_out"`
	Stdout          string   `json:"stdout"`
	Stderr          string   `json:"stderr"`
	StdoutTruncated bool     `json:"stdout_truncated"`
	StderrTruncated bool     `json:"stderr_truncated"`
	TimeoutClamped  bool     `json:"timeout_clamped"`
	DurationMS      int64    `json:"duration_ms"`
}

const (
	// execSchema 是 container.exec 的结果标识。
	execSchema = "container.exec.v1"
	// execMaxOutput 是 stdout / stderr 各自的截断上限（256 KiB）。
	execMaxOutput = 256 << 10
	// execDefaultTimeoutMS / execMinTimeoutMS / execMaxTimeoutMS 是超时窗口：
	// 缺省 10s、硬上限 30s（超时 kill docker CLI 并返回已捕获输出）。
	execDefaultTimeoutMS = 10000
	execMinTimeoutMS     = 100
	execMaxTimeoutMS     = 30000
	// execDefaultUser 是缺省执行身份：容器内非 root（数字 uid 不依赖 /etc/passwd）。
	execDefaultUser = "1000:1000"
)

// execDenylist 是拒绝执行的命令清单（按 argv[0] 的 basename 匹配）。
// 每条都写明理由：exec 是「一次性命令」入口，逃逸面与破坏面必须在这里收口。
var execDenylist = map[string]string{
	// 容器/编排运行时：容器里再跑运行时（尤其挂了 docker.sock 时）等于直接控宿主，
	// 且编排属 G-11 明确不做，不允许借 exec 绕进来。
	"docker":         "容器内再调 docker 等于把宿主容器面交出去（可能已挂 docker.sock），且编排属 v1 不做项",
	"podman":         "同 docker：容器内嵌套容器运行时是逃逸面",
	"nerdctl":        "同 docker：容器内嵌套容器运行时是逃逸面",
	"ctr":            "containerd 直连 = 绕过一切容器边界",
	"crictl":         "CRI 直连 = 绕过一切容器边界",
	"kubectl":        "容器内操作编排面属 G-11 不做项",
	"docker-compose": "编排工具属 G-11 不做项",
	// 跨命名空间 / 挂载宿主：经典容器逃逸三件套。
	"nsenter": "进入宿主或其它命名空间 = 直接逃逸出容器",
	"mount":   "挂载宿主文件系统 = 读写宿主盘",
	"umount":  "卸载容器内挂载点可破坏运行中的服务",
	// 宿主级破坏动作：容器内多数无效，但一旦有权限就是不可逆破坏，统一拒绝。
	"modprobe": "加载内核模块作用于宿主内核",
	"insmod":   "加载内核模块作用于宿主内核",
	"sysctl":   "可写 /proc/sys（内核运行参数），不给写入口",
	"mkfs":     "格式化磁盘属不可逆破坏",
	"fdisk":    "分区操作属不可逆破坏",
	"shutdown": "关机类动作作用于宿主",
	"reboot":   "关机类动作作用于宿主",
	"poweroff": "关机类动作作用于宿主",
	// 容器内提权：user 参数是本方法的非 root 默认的唯一闸门，放行 su/sudo 等于自毁。
	"su":   "会绕过 exec 的非 root 默认身份",
	"sudo": "会绕过 exec 的非 root 默认身份",
}

// deniedExecCommand 返回非空表示 cmd[0] 命中拒绝清单（返回理由）。
func deniedExecCommand(cmd []string) string {
	if len(cmd) == 0 || cmd[0] == "" {
		return "cmd 不能为空"
	}
	base := path.Base(cmd[0])
	if reason, ok := execDenylist[base]; ok {
		return base + " 被拒绝：" + reason
	}
	return ""
}

// isRootUser 判断 -u 取值是否解析为容器内 root（root / 0 / root:root / 0:0 / 0:gid …）。
func isRootUser(user string) bool {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(user)), ":")
	for _, p := range parts {
		if p == "root" || p == "0" {
			return true
		}
	}
	return false
}

// callExec 校验入参、拒绝清单与 root 确认，再交执行面跑一次性命令。
func (r *Runtime) callExec(raw json.RawMessage) (json.RawMessage, *pluginapi.Error) {
	var p execParams
	if perr := decodeParams(raw, &p); perr != nil {
		return nil, perr
	}
	if perr := ValidateID(p.ID); perr != nil {
		return nil, perr
	}
	if len(p.Cmd) == 0 {
		return nil, invalid("缺少 cmd：cmd 是字符串数组，逐项作为参数传递（不经过 shell）")
	}
	if reason := deniedExecCommand(p.Cmd); reason != "" {
		return nil, unsupported(reason)
	}
	// timeout_ms 按协议夹取到 [100, 30000]，超上限标 timeout_clamped（不拒绝调用）。
	timeout := p.TimeoutMS
	timeoutClamped := false
	if timeout == 0 {
		timeout = execDefaultTimeoutMS
	}
	if timeout > execMaxTimeoutMS {
		timeout = execMaxTimeoutMS
		timeoutClamped = true
	}
	if timeout < execMinTimeoutMS {
		timeout = execMinTimeoutMS
		timeoutClamped = true
	}
	if reason := deniedExecArgs(p.Cmd); reason != "" {
		return nil, unsupported(reason)
	}
	user := strings.TrimSpace(p.User)
	if user == "" {
		user = execDefaultUser // 默认容器内非 root
	}
	if isRootUser(user) && !p.Confirm {
		return nil, invalid("以 root 执行容器内命令需要 user:\"root\" + confirm:true；hint：默认以容器内非 root（1000:1000）执行，确需 root 请显式确认")
	}
	// 保护名单复用 start/stop 的策略：kore 组件容器不允许借 exec 操作。
	ref, err := r.exec.Resolve(p.ID)
	if err != nil {
		return nil, classify(err)
	}
	if perr := CheckProtected(ref, r.opts.Protected); perr != nil {
		return nil, perr
	}
	result, xerr := r.exec.Exec(ExecRequest{
		ID: ref.ID, Cmd: p.Cmd, User: user, Workdir: strings.TrimSpace(p.Workdir),
		Timeout: time.Duration(timeout) * time.Millisecond,
	})
	if xerr != nil {
		return nil, classify(xerr)
	}
	return encode(execResult{
		Schema: execSchema, ID: p.ID, Cmd: p.Cmd,
		ExitCode: result.ExitCode, TimedOut: result.TimedOut,
		Stdout: result.Stdout, Stderr: result.Stderr,
		StdoutTruncated: result.StdoutTruncated, StderrTruncated: result.StderrTruncated,
		TimeoutClamped: timeoutClamped, DurationMS: result.DurationMS,
	})
}

// ExecRequest 是 Executor.Exec 的入参（执行面无关的统一形状）。
type ExecRequest struct {
	ID      string
	Cmd     []string
	User    string
	Workdir string
	Timeout time.Duration
}

// ExecResult 是 Executor.Exec 的结果；TimedOut=true 时 Stdout/Stderr 是 kill 前已捕获的部分。
type ExecResult struct {
	ExitCode        int
	TimedOut        bool
	Stdout          string
	Stderr          string
	StdoutTruncated bool
	StderrTruncated bool
	DurationMS      int64
}

// Exec 在容器内执行一次性命令：固定 argv（docker exec [opts] id cmd…，cmd 段不经过 shell），
// 超时 kill docker CLI 并回传已捕获输出；两路输出各自截断到 execMaxOutput。
func (c *Client) Exec(req ExecRequest) (ExecResult, error) {
	args := []string{"exec", "-u", req.User}
	if req.Workdir != "" {
		args = append(args, "-w", req.Workdir)
	}
	args = append(args, req.ID)
	args = append(args, req.Cmd...)

	ctx, cancel := context.WithTimeout(context.Background(), req.Timeout)
	defer cancel()
	cmd, err := c.command(ctx, args...)
	if err != nil {
		return ExecResult{}, err
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	started := time.Now()
	runErr := cmd.Run()
	result := ExecResult{
		DurationMS: time.Since(started).Milliseconds(),
		ExitCode:   0,
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		result.TimedOut = true
		result.ExitCode = -1
	} else if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			// docker CLI 本身启动失败（非命令退出码）：按上游错误返回，不伪装成 exit_code。
			return ExecResult{}, c.execError(runErr, stderr.String())
		}
	}
	result.Stdout, result.StdoutTruncated = truncateOutput(stdout.String())
	result.Stderr, result.StderrTruncated = truncateOutput(stderr.String())
	return result, nil
}

// truncateOutput 按字节截断到 execMaxOutput；只在合法 UTF-8 边界处切，避免输出半个多字节。
func truncateOutput(s string) (string, bool) {
	if len(s) <= execMaxOutput {
		return s, false
	}
	cut := execMaxOutput
	for cut > 0 && !isUTF8Start(s[cut]) {
		cut--
	}
	return s[:cut], true
}

// isUTF8Start 判断字节是否为 UTF-8 首字节（非续字节）。
func isUTF8Start(b byte) bool { return b&0xC0 != 0x80 }

// execEscapeFlags 是禁止出现在 cmd 任一位置的容器逃逸参数（--privileged 类）。
// 这些参数本身不改 argv[0]，但会放大容器内进程对宿主的影响面，统一按 E_UNSUPPORTED 拒绝。
var execEscapeFlags = []string{
	"--privileged", "--pid=host", "--net=host", "--userns=host", "--ipc=host",
	"--cap-add", "--device", "--mount", "--security-opt", "--volume", "--volume=",
}

// deniedExecArgs 返回非空表示 cmd 命中逃逸参数（返回理由）。
func deniedExecArgs(cmd []string) string {
	for _, arg := range cmd {
		for _, flag := range execEscapeFlags {
			if arg == flag || strings.HasPrefix(arg, flag) {
				return "拒绝逃逸/提权参数 " + flag + "：exec 只做容器内一次性命令，不允许借参数扩大宿主影响面"
			}
		}
		if strings.HasPrefix(arg, "-v") && len(arg) > 2 && strings.Contains(arg, ":") {
			return "拒绝 -v 宿主路径挂载：exec 不允许改容器挂载面（请用镜像/编排声明）"
		}
	}
	return ""
}
