// Package main 是 kore 的 container 插件：通过本机 docker CLI 真实管理已有容器。
//
// container 只做协议握手 + 参数/护栏校验 + 固定 argv 子进程调用 + 结构化结果，
// 不引入 Docker SDK、不内置任何容器运行时二进制（G-05 / G-17）；它管理用户已有的
// 容器运行时，自身对内核零依赖。docker CLI 缺失、daemon 不可达或非 Linux 平台
// 一律返回 E_UNSUPPORTED，不静默降级、不伪造数据。
//
// 协议面（kore.plugin.v1）：UDS → kore.hello 握手 → 会话循环读 request、
// 按 method 分发并回 response；stdout 留给协议，日志一律写 stderr。
package main

import (
	"fmt"
	"log/slog"
	"net"
	"os"

	"github.com/pengtang1024/kore-plugin-container/internal/containerop"
)

// main 按退出码区分失败：0 优雅退出、1 启动或协议失败。
func main() {
	logger := newLogger(os.Getenv("KORE_LOG_LEVEL"))
	if err := run(logger); err != nil {
		logger.Error("插件退出", "err", err)
		os.Exit(1)
	}
}

// run 依次完成建连、hello 握手与会话循环；收到 kore.shutdown 后返回 nil。
func run(logger *slog.Logger) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	runtime := containerop.NewRuntime(containerop.Options{
		DockerBin: cfg.dockerBin,
		Protected: cfg.protectedContainers,
	}, nil)
	conn, err := net.Dial("unix", cfg.socket)
	if err != nil {
		return fmt.Errorf("连接主壳 socket %s: %w", cfg.socket, err)
	}
	defer func() { _ = conn.Close() }()
	if err := handshake(conn, cfg); err != nil {
		return err
	}
	logger.Info("hello 完成", "plugin_id", cfg.id, "version", cfg.version)
	return serve(conn, cfg, runtime, logger)
}
