//go:build linux

// Package e2e 对真实 docker 做端到端验证：列表 → 详情 → 日志 → 统计 → 停止 →
// 启动 → 重启 → 护栏拒绝（confirm / 保护名单 / 注入 ID）→ 清理。
//
// 默认跳过；显式开启：
//
//	KORE_CONTAINER_E2E=1 go test -timeout 180s -v ./tests/e2e
package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

const (
	// testContainer 是 E2E 独占的容器名；开始前清理、结束后删除。
	testContainer = "kore-ct-test"
	// testPort 是本机发布端口（仅测试用）。
	testPort = "18081"
	// testSecret 用于验证 inspect 的 env 脱敏。
	testSecret = "e2e-secret"
)

// TestDockerContainerE2E 真实 docker 全流程。
func TestDockerContainerE2E(t *testing.T) {
	if os.Getenv("KORE_CONTAINER_E2E") != "1" {
		t.Skip("设置 KORE_CONTAINER_E2E=1 后运行真实 docker E2E")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("未找到 docker CLI")
	}

	// 预清理：同名容器若残留先删掉，保证从干净状态开始。
	if _, err := runDocker("rm", "-f", testContainer); err != nil {
		t.Logf("E2E_PRECLEAN 无同名容器或删除失败（忽略）: %v", err)
	}
	t.Cleanup(func() { _, _ = runDocker("rm", "-f", testContainer) })

	runOut := dockerOutput(t, "run", "-d", "--name", testContainer,
		"-p", testPort+":80", "-e", "APP_PASSWORD="+testSecret, "nginx:alpine")
	t.Logf("E2E_DOCKER_RUN name=%s output=%s", testContainer, strings.TrimSpace(runOut))
	waitDockerState(t, testContainer, "running", 30*time.Second)

	// 启动真实插件二进制 + 假内核 UDS。
	binary := buildPlugin(t)
	socket := filepath.Join(t.TempDir(), "container.sock")
	listener := listen(t, socket)
	process, stderr := startPlugin(t, binary, socket, "")
	conn := accept(t, listener, stderr)
	handshake(t, conn)

	// 1) list{all:true}：能看到测试容器且字段完整。
	listRaw := mustCall(t, conn, "e2e-list-all", "container.list", map[string]any{"all": true})
	t.Logf("E2E_LIST_JSON %s", listRaw)
	listed := parseList(t, listRaw)
	if !listed.All || listed.Count != len(listed.Containers) || listed.Count == 0 {
		t.Fatalf("list 元数据不符: all=%v count=%d len=%d", listed.All, listed.Count, len(listed.Containers))
	}
	entry, ok := findEntry(listed.Containers, testContainer)
	if !ok {
		t.Fatalf("list 未包含 %s（count=%d）", testContainer, listed.Count)
	}
	if len(entry.ID) != 12 {
		t.Fatalf("list.id 不是 12 位短 ID: %q", entry.ID)
	}
	if entry.Name != testContainer || entry.Image != "nginx:alpine" || entry.State != "running" {
		t.Fatalf("list 条目基础字段不符: %+v", entry)
	}
	if entry.Status == "" || entry.RunningFor == "" || !strings.Contains(entry.Ports, testPort) {
		t.Fatalf("list 条目状态/端口字段不符: %+v", entry)
	}
	if _, err := time.Parse(time.RFC3339, entry.Created); err != nil {
		t.Fatalf("list.created 非 RFC3339: %q", entry.Created)
	}

	// 2) inspect：状态 / 端口 / env 脱敏 / 资源 / 挂载 / 健康。
	inspectRaw := mustCall(t, conn, "e2e-inspect", "container.inspect", map[string]any{"id": testContainer})
	t.Logf("E2E_INSPECT_JSON %s", inspectRaw)
	inspected := parseInspect(t, inspectRaw)
	if len(inspected.ID) != 64 || inspected.Name != testContainer || inspected.Image != "nginx:alpine" {
		t.Fatalf("inspect 基础字段不符: id=%q name=%q image=%q", inspected.ID, inspected.Name, inspected.Image)
	}
	if inspected.State.Status != "running" || !inspected.State.Running || inspected.State.StartedAt == "" {
		t.Fatalf("inspect.state 不符: %+v", inspected.State)
	}
	if inspected.State.RestartCount != 0 {
		t.Fatalf("inspect.state.restart_count=%d，期望 0", inspected.State.RestartCount)
	}
	if inspected.Network.IP == "" {
		t.Fatalf("inspect.network.ip 为空: %+v", inspected.Network)
	}
	if port, ok := findPort(inspected.Network.Ports, "80", testPort); !ok || port.Protocol != "tcp" {
		t.Fatalf("inspect.network.ports 未包含 80->%s/tcp: %+v", testPort, inspected.Network.Ports)
	}
	if len(inspected.Mounts) != 0 {
		t.Fatalf("inspect.mounts 期望为空: %+v", inspected.Mounts)
	}
	if inspected.Resources.MemoryLimit < 0 || inspected.Resources.CPUQuota < 0 {
		t.Fatalf("inspect.resources 异常: %+v", inspected.Resources)
	}
	if inspected.Health != "" {
		t.Fatalf("nginx:alpine 未配置健康检查，health 应为空: %q", inspected.Health)
	}
	envJoined := strings.Join(inspected.Config.Env, "\n")
	if !strings.Contains(envJoined, "APP_PASSWORD=***") {
		t.Fatalf("inspect.config.env 未脱敏 APP_PASSWORD: %v", inspected.Config.Env)
	}
	if strings.Contains(envJoined, testSecret) {
		t.Fatalf("inspect.config.env 泄漏明文密码: %v", inspected.Config.Env)
	}
	if len(inspected.Config.Cmd) == 0 {
		t.Fatalf("inspect.config.cmd 为空: %+v", inspected.Config)
	}

	// 3) logs：有 nginx 启动日志；tail 截断生效。
	logsRaw := mustCall(t, conn, "e2e-logs", "container.logs", map[string]any{"id": testContainer, "tail": 100})
	t.Logf("E2E_LOGS_JSON %s", logsRaw)
	logs := parseLogs(t, logsRaw)
	if logs.ID != testContainer || len(logs.Lines) == 0 || logs.Truncated {
		t.Fatalf("logs 结果不符: id=%q lines=%d truncated=%v", logs.ID, len(logs.Lines), logs.Truncated)
	}
	joinedLogs := strings.Join(logs.Lines, "\n")
	if !strings.Contains(joinedLogs, "ready for start up") && !strings.Contains(joinedLogs, "start worker processes") {
		t.Fatalf("logs 未包含 nginx 启动日志: %q", joinedLogs)
	}
	tailRaw := mustCall(t, conn, "e2e-logs-tail", "container.logs", map[string]any{"id": testContainer, "tail": 3})
	tailLogs := parseLogs(t, tailRaw)
	if len(tailLogs.Lines) > 3 {
		t.Fatalf("tail=3 返回 %d 行: %v", len(tailLogs.Lines), tailLogs.Lines)
	}
	t.Logf("E2E_LOGS_TAIL lines=%d", len(tailLogs.Lines))

	// 4) stats：CPU/内存/网络/块 IO/PIDs。
	statsRaw := mustCall(t, conn, "e2e-stats", "container.stats", map[string]any{"id": testContainer})
	t.Logf("E2E_STATS_JSON %s", statsRaw)
	stats := parseStats(t, statsRaw)
	if stats.Name != testContainer || stats.CPUPercent < 0 {
		t.Fatalf("stats 基础字段不符: %+v", stats)
	}
	if stats.Memory.Usage <= 0 || stats.Memory.Limit <= 0 || stats.Memory.Percent < 0 {
		t.Fatalf("stats.memory 不符: %+v", stats.Memory)
	}
	if stats.Network.Input < 0 || stats.Network.Output < 0 || stats.Block.Read < 0 || stats.Block.Write < 0 {
		t.Fatalf("stats 网络/块设备不符: %+v", stats)
	}
	if stats.PIDs < 1 {
		t.Fatalf("stats.pids=%d，期望 ≥1", stats.PIDs)
	}

	// 5) stop 缺 confirm：E_INVALID，且容器仍在运行（护栏在触达 docker 前生效）。
	noConfirm := request(t, conn, "e2e-stop-no-confirm", "container.stop", map[string]any{"id": testContainer})
	if noConfirm.Error == nil || noConfirm.Error.Code != pluginapi.E_INVALID {
		t.Fatalf("stop 缺 confirm 错误=%+v，期望 E_INVALID", noConfirm.Error)
	}
	if !strings.Contains(noConfirm.Error.Message, "confirm=true") {
		t.Fatalf("stop 缺 confirm 错误信息未说明确认参数: %q", noConfirm.Error.Message)
	}
	if state := dockerState(t, testContainer); state != "running" {
		t.Fatalf("缺 confirm 的 stop 改变了容器状态: %q", state)
	}
	t.Logf("E2E_STOP_NO_CONFIRM_REJECTED %s: %s", noConfirm.Error.Code, noConfirm.Error.Message)

	// 6) stop{confirm:true}：状态变 exited；默认 list 不再包含、all list 包含。
	stopRaw := mustCall(t, conn, "e2e-stop", "container.stop", map[string]any{
		"id": testContainer, "confirm": true, "timeout": 10,
	})
	t.Logf("E2E_STOP_JSON %s", stopRaw)
	stopped := parseAction(t, stopRaw)
	if !stopped.OK || stopped.Action != "stop" || stopped.State != "exited" {
		t.Fatalf("stop 结果不符: %+v", stopped)
	}
	waitDockerState(t, testContainer, "exited", 20*time.Second)
	stoppedInspect := parseInspect(t, mustCall(t, conn, "e2e-inspect-stopped", "container.inspect", map[string]any{"id": testContainer}))
	if stoppedInspect.State.Running || stoppedInspect.State.Status != "exited" {
		t.Fatalf("stop 后 inspect 状态不符: %+v", stoppedInspect.State)
	}
	runningList := parseList(t, mustCall(t, conn, "e2e-list-running", "container.list", map[string]any{}))
	if _, exists := findEntry(runningList.Containers, testContainer); exists {
		t.Fatalf("默认 list（不含已停止）仍包含 %s", testContainer)
	}
	allList := parseList(t, mustCall(t, conn, "e2e-list-all-stopped", "container.list", map[string]any{"all": true}))
	stoppedEntry, exists := findEntry(allList.Containers, testContainer)
	if !exists || stoppedEntry.State != "exited" {
		t.Fatalf("all list 未包含已停止容器: %+v (exists=%v)", stoppedEntry, exists)
	}
	t.Logf("E2E_LIST_ALL_FLAG default_count=%d all_count=%d stopped_state=%s",
		runningList.Count, allList.Count, stoppedEntry.State)

	// 7) start：恢复 running。
	startRaw := mustCall(t, conn, "e2e-start", "container.start", map[string]any{"id": testContainer})
	t.Logf("E2E_START_JSON %s", startRaw)
	started := parseAction(t, startRaw)
	if !started.OK || started.Action != "start" || started.State != "running" {
		t.Fatalf("start 结果不符: %+v", started)
	}
	waitDockerState(t, testContainer, "running", 20*time.Second)

	// 8) restart{confirm:true}：状态 running 且 StartedAt 变化。
	before := parseInspect(t, mustCall(t, conn, "e2e-inspect-before-restart", "container.inspect", map[string]any{"id": testContainer}))
	restartRaw := mustCall(t, conn, "e2e-restart", "container.restart", map[string]any{
		"id": testContainer, "confirm": true, "timeout": 10,
	})
	t.Logf("E2E_RESTART_JSON %s", restartRaw)
	restarted := parseAction(t, restartRaw)
	if !restarted.OK || restarted.Action != "restart" || restarted.State != "running" {
		t.Fatalf("restart 结果不符: %+v", restarted)
	}
	waitDockerState(t, testContainer, "running", 20*time.Second)
	after := parseInspect(t, mustCall(t, conn, "e2e-inspect-after-restart", "container.inspect", map[string]any{"id": testContainer}))
	if after.State.StartedAt == before.State.StartedAt {
		t.Fatalf("restart 后 started_at 未变化: %q", after.State.StartedAt)
	}
	t.Logf("E2E_RESTART_STARTED_AT before=%s after=%s", before.State.StartedAt, after.State.StartedAt)

	// 9) ID 注入：在触达 docker 前返回 E_INVALID。
	injection := request(t, conn, "e2e-injection", "container.inspect", map[string]any{"id": testContainer + "; rm -rf /"})
	if injection.Error == nil || injection.Error.Code != pluginapi.E_INVALID {
		t.Fatalf("注入 ID 错误=%+v，期望 E_INVALID", injection.Error)
	}
	t.Logf("E2E_INJECTION_REJECTED %s: %s", injection.Error.Code, injection.Error.Message)

	// 10) 优雅退出，stderr 不得出现 panic。
	shutdown := mustCall(t, conn, "e2e-shutdown", "kore.shutdown", map[string]any{})
	if !strings.Contains(shutdown, `"ok":true`) {
		t.Fatalf("shutdown 结果不符: %s", shutdown)
	}
	waitExit(t, process)
	if strings.Contains(stderr.String(), "panic:") {
		t.Fatalf("插件 stderr 出现 panic: %s", stderr.String())
	}
	_ = conn.Close()
	_ = listener.Close()
	t.Logf("E2E_SHUTDOWN %s", shutdown)

	// 11) 保护名单：第二个插件实例把测试容器列入 KORE_PROTECTED_CONTAINERS，
	//     stop/restart/start 全部 E_UNAUTHORIZED 且容器保持运行。
	protectedSocket := filepath.Join(t.TempDir(), "container-protected.sock")
	protectedListener := listen(t, protectedSocket)
	protectedProcess, protectedStderr := startPlugin(t, binary, protectedSocket, testContainer)
	protectedConn := accept(t, protectedListener, protectedStderr)
	handshake(t, protectedConn)
	for _, verb := range []string{"start", "stop", "restart"} {
		params := map[string]any{"id": testContainer, "confirm": true}
		if verb == "start" {
			params = map[string]any{"id": testContainer}
		}
		resp := request(t, protectedConn, "e2e-protected-"+verb, "container."+verb, params)
		if resp.Error == nil || resp.Error.Code != pluginapi.E_UNAUTHORIZED {
			t.Fatalf("保护名单 %s 错误=%+v，期望 E_UNAUTHORIZED", verb, resp.Error)
		}
		if !strings.Contains(resp.Error.Message, "KORE_PROTECTED_CONTAINERS") {
			t.Fatalf("保护名单 %s 错误信息未点名环境变量: %q", verb, resp.Error.Message)
		}
		t.Logf("E2E_PROTECTED_REJECTED %s %s: %s", verb, resp.Error.Code, resp.Error.Message)
	}
	if state := dockerState(t, testContainer); state != "running" {
		t.Fatalf("保护名单拒绝后容器状态异常: %q", state)
	}
	mustCall(t, protectedConn, "e2e-protected-shutdown", "kore.shutdown", map[string]any{})
	waitExit(t, protectedProcess)
	_ = protectedConn.Close()
	_ = protectedListener.Close()

	// 12) 清理并确认无残留（docker ps -a 中不存在 kore-ct-test）。
	dockerOutput(t, "rm", "-f", testContainer)
	if hasDockerName(t, testContainer) {
		t.Fatalf("清理后 docker ps -a 仍包含 %s", testContainer)
	}
	t.Logf("E2E_CLEANUP container=%s removed=true names_contains=%v", testContainer, hasDockerName(t, testContainer))
}
