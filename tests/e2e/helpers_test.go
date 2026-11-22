//go:build linux

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// buildPlugin 构建真实插件二进制到临时目录。
func buildPlugin(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "container-plugin")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = "../.."
	build.Env = append(os.Environ(), "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("构建插件失败: %v\n%s", err, out)
	}
	return binary
}

// startPlugin 以协议环境变量启动插件进程；stderr 收集起来便于失败时定位。
func startPlugin(t *testing.T, binary, socket, protected string) (*exec.Cmd, *bytes.Buffer) {
	t.Helper()
	stderr := &bytes.Buffer{}
	process := exec.Command(binary)
	process.Env = withEnv(os.Environ(),
		"KORE_PLUGIN_SOCK="+socket,
		"KORE_PLUGIN_ID=container",
		"KORE_PLUGIN_TOKEN=e2e-token",
		"KORE_PROTECTED_CONTAINERS="+protected,
	)
	process.Stdout = io.Discard
	process.Stderr = stderr
	if err := process.Start(); err != nil {
		t.Fatalf("启动插件失败: %v", err)
	}
	t.Cleanup(func() {
		if process.ProcessState == nil {
			_ = process.Process.Kill()
			_, _ = process.Process.Wait()
		}
	})
	return process, stderr
}

// withEnv 先删掉同名环境变量再追加，避免宿主机残留值覆盖测试配置。
func withEnv(base []string, pairs ...string) []string {
	keys := make(map[string]bool, len(pairs))
	for _, pair := range pairs {
		key, _, _ := strings.Cut(pair, "=")
		keys[key] = true
	}
	env := make([]string, 0, len(base)+len(pairs))
	for _, item := range base {
		key, _, _ := strings.Cut(item, "=")
		if !keys[key] {
			env = append(env, item)
		}
	}
	return append(env, pairs...)
}

// listen 监听 UDS 并设置超时。
func listen(t *testing.T, socket string) *net.UnixListener {
	t.Helper()
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("监听 UDS 失败: %v", err)
	}
	unixListener := listener.(*net.UnixListener)
	_ = unixListener.SetDeadline(time.Now().Add(120 * time.Second))
	t.Cleanup(func() { _ = unixListener.Close() })
	return unixListener
}

// accept 接受插件连接并设置帧超时。
func accept(t *testing.T, listener *net.UnixListener, stderr *bytes.Buffer) net.Conn {
	t.Helper()
	conn, err := listener.Accept()
	if err != nil {
		t.Fatalf("接受插件连接失败: %v (stderr=%s)", err, stderr.String())
	}
	_ = conn.SetDeadline(time.Now().Add(120 * time.Second))
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// handshake 读 kore.hello 并回 ok 响应。
func handshake(t *testing.T, conn net.Conn) {
	t.Helper()
	request := readRequest(t, conn)
	if request.Method != "kore.hello" {
		t.Fatalf("首帧应为 kore.hello，实际 %s", request.Method)
	}
	response := &pluginapi.Response{
		Type: pluginapi.TypeResponse, ID: request.ID, ReqID: request.ReqID,
		Result: json.RawMessage(`{"ok":true,"protocol":"kore.plugin.v1"}`),
	}
	if err := pluginapi.WriteJSON(conn, response); err != nil {
		t.Fatalf("回写 hello 失败: %v", err)
	}
}

// request 发送一次方法调用并读回响应。
func request(t *testing.T, conn net.Conn, id, method string, params any) *pluginapi.Response {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("序列化 %s 参数失败: %v", method, err)
	}
	req := &pluginapi.Request{
		Type: pluginapi.TypeRequest, ID: id, ReqID: "r-" + id, Method: method, Params: raw,
	}
	if err := pluginapi.WriteJSON(conn, req); err != nil {
		t.Fatalf("发送 %s 失败: %v", method, err)
	}
	return readResponse(t, conn)
}

// mustCall 要求调用成功并返回 result 原文。
func mustCall(t *testing.T, conn net.Conn, id, method string, params any) string {
	t.Helper()
	response := request(t, conn, id, method, params)
	if response.Error != nil {
		t.Fatalf("%s 失败: %+v", method, response.Error)
	}
	return string(response.Result)
}

// readRequest 读下一帧并断言是 request。
func readRequest(t *testing.T, conn net.Conn) *pluginapi.Request {
	t.Helper()
	env, err := pluginapi.DecodeFrame(readPayload(t, conn))
	if err != nil {
		t.Fatalf("解码请求失败: %v", err)
	}
	request, ok := env.(*pluginapi.Request)
	if !ok {
		t.Fatalf("期望 request，实际 %T", env)
	}
	return request
}

// readResponse 读下一帧并断言是 response。
func readResponse(t *testing.T, conn net.Conn) *pluginapi.Response {
	t.Helper()
	env, err := pluginapi.DecodeFrame(readPayload(t, conn))
	if err != nil {
		t.Fatalf("解码响应失败: %v", err)
	}
	response, ok := env.(*pluginapi.Response)
	if !ok {
		t.Fatalf("期望 response，实际 %T", env)
	}
	return response
}

// readPayload 读一整帧。
func readPayload(t *testing.T, conn net.Conn) []byte {
	t.Helper()
	payload, err := pluginapi.ReadFrame(conn, pluginapi.MaxFrameSize)
	if err != nil {
		t.Fatalf("读帧失败: %v", err)
	}
	return payload
}

// runDocker 直接调用 docker CLI（测试侧交叉核对，不经过插件）。
func runDocker(args ...string) ([]byte, error) {
	cmd := exec.Command("docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docker %s 失败: %v（stderr=%s）", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// dockerOutput 直接调用 docker 并要求成功。
func dockerOutput(t *testing.T, args ...string) string {
	t.Helper()
	out, err := runDocker(args...)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return string(out)
}

// dockerState 读取容器状态；容器不存在时返回空串。
func dockerState(t *testing.T, name string) string {
	t.Helper()
	out, err := runDocker("inspect", "--format", "{{.State.Status}}", name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// waitDockerState 轮询容器状态直到命中或超时。
func waitDockerState(t *testing.T, name, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if dockerState(t, name) == want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("容器 %s 未在 %s 内进入 %s（当前 %q）", name, timeout, want, dockerState(t, name))
}

// dockerNames 返回 `docker ps -a` 的全部容器名。
func dockerNames(t *testing.T) []string {
	t.Helper()
	raw := dockerOutput(t, "ps", "-a", "--format", "{{.Names}}")
	names := make([]string, 0)
	for _, line := range strings.Split(raw, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			names = append(names, line)
		}
	}
	return names
}

// hasDockerName 判断容器名是否仍存在。
func hasDockerName(t *testing.T, want string) bool {
	t.Helper()
	for _, name := range dockerNames(t) {
		if name == want {
			return true
		}
	}
	return false
}

// waitExit 等待插件进程退出。
func waitExit(t *testing.T, process *exec.Cmd) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- process.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("插件退出异常: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("插件未在 5s 内退出")
	}
}
