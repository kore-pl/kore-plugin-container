package containerop

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// execRuntime 返回带假执行面的 Runtime（GOOS=linux，保护名单空）。
func execRuntime() (*Runtime, *testExecutor) {
	fake := &testExecutor{}
	return NewRuntime(Options{GOOS: "linux"}, fake), fake
}

// TestExecArgvPassthrough 锁定「cmd 是字符串数组、不经 shell」：带空格/分号/引号的
// 一个参数必须原样作为**一个** argv 传给 docker exec，不拼接、不拆分。
func TestExecArgvPassthrough(t *testing.T) {
	r, fake := execRuntime()
	const tricky = `echo hi; rm -rf /tmp/x && echo "a b"`
	raw, _ := json.Marshal(execParams{ID: "abc123", Cmd: []string{"sh", "-c", tricky}, TimeoutMS: 1000})
	if _, perr := r.Call("container.exec", raw); perr != nil {
		t.Fatalf("Call: %v", perr)
	}
	if fake.execReq == nil {
		t.Fatal("未触达 Executor.Exec")
	}
	cmd := fake.execReq.Cmd
	if len(cmd) != 3 || cmd[0] != "sh" || cmd[1] != "-c" || cmd[2] != tricky {
		t.Fatalf("argv 被改写：%#v", cmd)
	}
	if fake.execReq.User != execDefaultUser {
		t.Fatalf("默认 user = %q，期望非 root %q", fake.execReq.User, execDefaultUser)
	}
}

// TestExecTimeoutClamped 验证 timeout_ms 超上限夹取到 30s 并标 timeout_clamped。
func TestExecTimeoutClamped(t *testing.T) {
	r, fake := execRuntime()
	raw, _ := json.Marshal(execParams{ID: "abc123", Cmd: []string{"true"}, TimeoutMS: 999999})
	out, perr := r.Call("container.exec", raw)
	if perr != nil {
		t.Fatalf("Call: %v", perr)
	}
	if fake.execReq.Timeout != 30*time.Second {
		t.Fatalf("夹取后 timeout = %v，期望 30s", fake.execReq.Timeout)
	}
	var result struct {
		TimeoutClamped bool `json:"timeout_clamped"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("结果不是 JSON: %v", err)
	}
	if !result.TimeoutClamped {
		t.Fatal("timeout_clamped 应为 true")
	}
}

// TestExecRootRequiresConfirm 验证容器内 root 必须显式 confirm:true。
func TestExecRootRequiresConfirm(t *testing.T) {
	r, _ := execRuntime()
	raw, _ := json.Marshal(execParams{ID: "abc123", Cmd: []string{"id"}, User: "root"})
	_, perr := r.Call("container.exec", raw)
	if perr == nil || perr.Code != "E_INVALID" {
		t.Fatalf("root 无 confirm 应 E_INVALID，实际 %v", perr)
	}
	if got := reasonOf(t, perr); got != ExecReasonConfirmRequired {
		t.Fatalf("detail.reason = %q，期望 %q", got, ExecReasonConfirmRequired)
	}
	raw, _ = json.Marshal(execParams{ID: "abc123", Cmd: []string{"id"}, User: "root", Confirm: true})
	if _, perr := r.Call("container.exec", raw); perr != nil {
		t.Fatalf("root + confirm 应放行，实际 %v", perr)
	}
}

// reasonOf 解出协议错误的 detail.reason（前端契约字段）。
func reasonOf(t *testing.T, perr *pluginapi.Error) string {
	t.Helper()
	if perr == nil || len(perr.Detail) == 0 {
		return ""
	}
	var detail struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(perr.Detail, &detail); err != nil {
		t.Fatalf("detail 不是合法 JSON: %v", err)
	}
	return detail.Reason
}

// TestExecRejectsDenylistAndEscapeFlags 验证命令拒绝清单与逃逸参数拒绝。
func TestExecRejectsDenylistAndEscapeFlags(t *testing.T) {
	r, fake := execRuntime()
	cases := []struct {
		name   string
		cmd    []string
		want   string
		reason string
	}{
		{"nsenter", []string{"nsenter", "--target", "1"}, "nsenter", ExecReasonDenylist},
		{"mount", []string{"mount", "/dev/sda1", "/mnt"}, "mount", ExecReasonDenylist},
		{"modprobe", []string{"modprobe", "xfs"}, "modprobe", ExecReasonDenylist},
		{"sysctl", []string{"sysctl", "-w", "net.ipv4.ip_forward=1"}, "sysctl", ExecReasonDenylist},
		{"privileged 参数", []string{"sh", "-c", "id", "--privileged"}, "逃逸/提权参数", ExecReasonEscapeFlag},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(execParams{ID: "abc123", Cmd: tc.cmd, TimeoutMS: 1000})
			_, perr := r.Call("container.exec", raw)
			if perr == nil || perr.Code != "E_UNSUPPORTED" {
				t.Fatalf("期望 E_UNSUPPORTED，实际 %v", perr)
			}
			if !strings.Contains(perr.Message, tc.want) {
				t.Fatalf("理由不含 %q：%s", tc.want, perr.Message)
			}
			if got := reasonOf(t, perr); got != tc.reason {
				t.Fatalf("detail.reason = %q，期望 %q", got, tc.reason)
			}
		})
	}
	if fake.execReq != nil {
		t.Fatal("被拒绝的调用不应触达 Executor.Exec")
	}
}

// TestExecTruncateOutput 验证 stdout/stderr 各自截断到 256 KiB 并标 truncated。
func TestExecTruncateOutput(t *testing.T) {
	big := strings.Repeat("a", execMaxOutput+1024)
	got, truncated := truncateOutput(big)
	if !truncated || len(got) != execMaxOutput {
		t.Fatalf("截断 = %v / %d，期望 true / %d", truncated, len(got), execMaxOutput)
	}
	if small, cut := truncateOutput("ok"); cut || small != "ok" {
		t.Fatalf("小输出不应截断：%q %v", small, cut)
	}
}
