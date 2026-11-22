package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// testConfig 构造握手与会话循环共用的一致配置（不依赖环境变量）。
func testConfig() config {
	return config{
		id:           "container",
		version:      "0.1.0",
		requiresKore: ">=0.1.0 <0.2.0",
		capabilities: []string{"system:read"},
		startedAt:    time.Now(),
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func readFrame(t *testing.T, conn net.Conn) any {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	payload, err := pluginapi.ReadFrame(conn, pluginapi.MaxFrameSize)
	if err != nil {
		t.Fatalf("读帧失败: %v", err)
	}
	env, err := pluginapi.DecodeFrame(payload)
	if err != nil {
		t.Fatalf("解码帧失败: %v", err)
	}
	return env
}

func readRequest(t *testing.T, conn net.Conn) *pluginapi.Request {
	t.Helper()
	request, ok := readFrame(t, conn).(*pluginapi.Request)
	if !ok {
		t.Fatal("帧类型不是 request")
	}
	return request
}

func writeResponse(t *testing.T, conn net.Conn, id, reqID string, result json.RawMessage) {
	t.Helper()
	if err := pluginapi.WriteJSON(conn, &pluginapi.Response{
		Type: pluginapi.TypeResponse, ID: id, ReqID: reqID, Result: result,
	}); err != nil {
		t.Fatalf("写响应失败: %v", err)
	}
}

// sendShutdown 发送一个 kore.shutdown 请求，并配对读取其唯一响应帧。
// 硬约束：每个 shutdown 请求必须配对读一次响应帧，否则 net.Pipe 写会阻塞、
// 且无法证明优雅退出真的回了 {"ok":true}。
func sendShutdown(t *testing.T, conn net.Conn, id, reqID string) {
	t.Helper()
	request := &pluginapi.Request{
		Type: pluginapi.TypeRequest, ID: id, ReqID: reqID, Method: "kore.shutdown",
	}
	if err := pluginapi.WriteJSON(conn, request); err != nil {
		t.Fatalf("写 shutdown 请求失败: %v", err)
	}
	env := readFrame(t, conn)
	response, ok := env.(*pluginapi.Response)
	if !ok || response.ID != id || response.ReqID != reqID {
		t.Fatalf("shutdown 响应帧异常: %+v", env)
	}
	if response.Error != nil {
		t.Fatalf("shutdown 响应带错误: %v", response.Error)
	}
	if string(response.Result) != `{"ok":true}` {
		t.Fatalf("shutdown 结果=%s，期望 {\"ok\":true}", response.Result)
	}
}

// TestHandshake 校验 hello 首帧字段与三步握手。
func TestHandshake(t *testing.T) {
	pluginConn, kernelConn := net.Pipe()
	defer func() { _ = kernelConn.Close() }()
	done := make(chan error, 1)
	go func() { done <- handshake(pluginConn, testConfig()); _ = pluginConn.Close() }()

	request := readRequest(t, kernelConn)
	if request.Method != helloMethod {
		t.Fatalf("首帧方法=%q，期望 %q", request.Method, helloMethod)
	}
	var params helloParams
	if err := json.Unmarshal(request.Params, &params); err != nil {
		t.Fatalf("解析 hello 参数失败: %v", err)
	}
	if params.PluginID != "container" || params.ID != "container" {
		t.Fatalf("plugin_id/id=%q/%q，期望 container", params.PluginID, params.ID)
	}
	if params.Protocol != pluginapi.Protocol || params.Version != "0.1.0" {
		t.Fatalf("protocol/version=%q/%q 不符合 §5", params.Protocol, params.Version)
	}
	if params.PID != os.Getpid() {
		t.Fatalf("pid=%d，期望 %d", params.PID, os.Getpid())
	}
	result, err := json.Marshal(helloResult{OK: true, Protocol: pluginapi.Protocol})
	if err != nil {
		t.Fatalf("序列化握手响应失败: %v", err)
	}
	writeResponse(t, kernelConn, request.ID, request.ReqID, result)
	if err := <-done; err != nil {
		t.Fatalf("握手返回错误: %v", err)
	}
}

// TestServeShutdown 会话循环收到 kore.shutdown 后回帧并优雅退出。
func TestServeShutdown(t *testing.T) {
	pluginConn, kernelConn := net.Pipe()
	defer func() { _ = kernelConn.Close() }()
	done := make(chan error, 1)
	go func() { done <- serve(pluginConn, testConfig(), testLogger()); _ = pluginConn.Close() }()

	// 每个 shutdown 请求配对读一次响应帧（id/reqID 原样回显校验）。
	sendShutdown(t, kernelConn, "id-1", "r-1")
	if err := <-done; err != nil {
		t.Fatalf("serve 返回错误: %v", err)
	}
}
