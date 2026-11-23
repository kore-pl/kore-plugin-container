package containerop

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// Client 通过本机 docker CLI 执行容器操作（os/exec，固定 argv，不经 shell）。
type Client struct {
	bin     string
	timeout time.Duration
}

// NewClient 用配置构造执行器并补齐缺省值。
func NewClient(bin string, timeout time.Duration) *Client {
	if bin == "" {
		bin = DefaultDockerBin
	}
	if timeout <= 0 {
		timeout = DefaultDockerTimeout
	}
	return &Client{bin: bin, timeout: timeout}
}

// List 执行 docker ps（all=true 时含已停止容器）并解析为结构化条目。
func (c *Client) List(all bool) ([]ContainerEntry, error) {
	args := []string{"ps", "--format", "{{json .}}"}
	if all {
		args = append(args, "--all")
	}
	out, err := c.run(args...)
	if err != nil {
		return nil, err
	}
	entries, err := parsePSOutput(out)
	if err != nil {
		return nil, opErrorf(pluginapi.E_UPSTREAM, "%s", err.Error())
	}
	return entries, nil
}

// Inspect 执行 docker inspect 并收敛为精简详情。
func (c *Client) Inspect(id string) (*InspectResult, error) {
	out, err := c.run("inspect", id)
	if err != nil {
		return nil, err
	}
	result, err := parseInspectOutput(out)
	if err != nil {
		return nil, opErrorf(pluginapi.E_UPSTREAM, "%s", err.Error())
	}
	if result == nil {
		return nil, opErrorf(pluginapi.E_NOTFOUND, "容器不存在: %s", id)
	}
	return result, nil
}

// Resolve 用 docker inspect --format 取完整 ID 与名称（轻量，用于保护名单校验）。
func (c *Client) Resolve(id string) (ContainerRef, error) {
	out, err := c.run("inspect", "--format", "{{.Id}} {{.Name}}", id)
	if err != nil {
		return ContainerRef{}, err
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return ContainerRef{}, opErrorf(pluginapi.E_NOTFOUND, "容器不存在: %s", id)
	}
	return ContainerRef{ID: fields[0], Name: strings.TrimPrefix(fields[1], "/")}, nil
}

// Logs 执行 docker logs --tail N（since 非空时追加 --since），合并 stdout/stderr 并按单行/总字节预算截断。
func (c *Client) Logs(id string, tail int, since string) (LogsResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	args := []string{"logs", "--tail", strconv.Itoa(tail)}
	if since != "" {
		args = append(args, "--since", since)
	}
	args = append(args, id)
	cmd, err := c.command(ctx, args...)
	if err != nil {
		return LogsResult{}, err
	}
	// 容器 stderr 由 docker CLI 写到自己 stderr：两路共用带锁的限长缓冲，避免交错丢字节。
	buffer := newCappedBuffer(MaxLogBytes)
	cmd.Stdout = buffer
	cmd.Stderr = buffer
	runErr := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return LogsResult{}, timeoutError(c.timeout)
	}
	if runErr != nil {
		return LogsResult{}, c.execError(runErr, buffer.String())
	}
	lines, truncated := splitLogLines(buffer.Bytes(), buffer.Truncated())
	return LogsResult{ID: id, Lines: lines, Truncated: truncated}, nil
}

// Start 启动容器。
func (c *Client) Start(id string) error {
	_, err := c.run("start", id)
	return err
}

// Stop 停止容器；timeout<0 时不传 --time，沿用 docker 缺省优雅退出时间。
func (c *Client) Stop(id string, timeout int) error {
	args := []string{"stop"}
	if timeout >= 0 {
		args = append(args, "--time", strconv.Itoa(timeout))
	}
	args = append(args, id)
	_, err := c.run(args...)
	return err
}

// Restart 重启容器；timeout<0 时不传 --time。
func (c *Client) Restart(id string, timeout int) error {
	args := []string{"restart"}
	if timeout >= 0 {
		args = append(args, "--time", strconv.Itoa(timeout))
	}
	args = append(args, id)
	_, err := c.run(args...)
	return err
}

// Stats 执行一次 docker stats --no-stream 并解析 CPU/内存/网络/块设备指标。
func (c *Client) Stats(id string) (StatsResult, error) {
	out, err := c.run("stats", "--no-stream", "--format", "{{json .}}", id)
	if err != nil {
		return StatsResult{}, err
	}
	result, err := parseStatsOutput(out)
	if err != nil {
		return StatsResult{}, opErrorf(pluginapi.E_UPSTREAM, "%s", err.Error())
	}
	if result == nil {
		return StatsResult{}, opErrorf(pluginapi.E_CONFLICT, "容器 %s 暂无实时统计（可能未运行）", id)
	}
	return *result, nil
}

// Images 执行 docker images --format '{{json .}}' 并解析为结构化条目。
// 只读、无联网：镜像拉取是独立方法 container.pull。
func (c *Client) Images() ([]ImageEntry, error) {
	out, err := c.run("images", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	images, err := parseImagesOutput(out)
	if err != nil {
		return nil, opErrorf(pluginapi.E_UPSTREAM, "%s", err.Error())
	}
	return images, nil
}

// Pull 执行 docker pull <ref>；输出受 MaxPullOutputBytes 预算限制并取末行作为摘要。
// 只在用户显式确认（confirm=true）后由 Call 触发，插件自身不做后台拉取。
func (c *Client) Pull(ref string) (PullResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	cmd, err := c.command(ctx, "pull", ref)
	if err != nil {
		return PullResult{}, err
	}
	buffer := newCappedBuffer(MaxPullOutputBytes)
	cmd.Stdout = buffer
	cmd.Stderr = buffer
	runErr := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return PullResult{}, timeoutError(c.timeout)
	}
	if runErr != nil {
		return PullResult{}, c.execError(runErr, buffer.String())
	}
	return PullResult{Ref: ref, OK: true, Message: lastNonEmptyLine(buffer.String()), Truncated: buffer.Truncated()}, nil
}

// RemoveImage 执行 docker rmi [--force] <ref>。
func (c *Client) RemoveImage(ref string, force bool) error {
	args := []string{"rmi"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, ref)
	_, err := c.run(args...)
	return err
}

// Update 执行 docker update 修改容器内存 / CPU 限额；两个字段至少一个由 Call 保证非空。
func (c *Client) Update(id string, update ContainerUpdate) error {
	args := []string{"update"}
	if update.MemoryBytes != nil {
		args = append(args, "--memory", strconv.FormatInt(*update.MemoryBytes, 10))
	}
	if update.CPUs != nil {
		args = append(args, "--cpus", strconv.FormatFloat(*update.CPUs, 'f', -1, 64))
	}
	args = append(args, id)
	_, err := c.run(args...)
	return err
}

// lastNonEmptyLine 取多行文本的最后一个非空行并截断，作为 pull 的结果摘要。
func lastNonEmptyLine(raw string) string {
	lines := strings.Split(raw, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return truncateRunes(line, 300)
		}
	}
	return ""
}

// command 构造固定 argv 的子进程；docker 未安装时返回 E_UNSUPPORTED。
func (c *Client) command(ctx context.Context, args ...string) (*exec.Cmd, error) {
	path, err := exec.LookPath(c.bin)
	if err != nil {
		return nil, opErrorf(pluginapi.E_UNSUPPORTED,
			"未找到 docker 命令行 %q：请安装 Docker CLI，或用 %s 指定绝对路径", c.bin, EnvDockerBin)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = os.Environ()
	return cmd, nil
}

// run 执行一条 docker 命令并返回 stdout；超时统一映射为 E_UPSTREAM。
func (c *Client) run(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	cmd, err := c.command(ctx, args...)
	if err != nil {
		return nil, err
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, timeoutError(c.timeout)
	}
	if runErr != nil {
		return nil, c.execError(runErr, stderr.String())
	}
	return stdout.Bytes(), nil
}

// timeoutError 统一超时错误文案（含实际生效超时）。
func timeoutError(timeout time.Duration) error {
	return opErrorf(pluginapi.E_UPSTREAM, "docker 命令超时（%s），已终止", timeout)
}

// execError 把 CLI 启动/执行失败映射为协议错误；stderr 先清洗再截断。
func (c *Client) execError(err error, stderr string) error {
	var execErr *exec.Error
	var pathErr *fs.PathError
	if errors.As(err, &execErr) || errors.As(err, &pathErr) {
		return opErrorf(pluginapi.E_UNSUPPORTED,
			"无法执行 docker 命令行 %q：请安装 Docker CLI，或用 %s 指定绝对路径", c.bin, EnvDockerBin)
	}
	text := sanitizeStderr(stderr)
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "cannot connect to the docker daemon"),
		strings.Contains(lower, "is the docker daemon running"),
		strings.Contains(lower, "docker daemon is not running"):
		return opErrorf(pluginapi.E_UNSUPPORTED,
			"Docker daemon 不可达：请确认 dockerd 已启动，且当前用户可访问 docker socket（%s）", truncateRunes(text, 300))
	case strings.Contains(lower, "permission denied") &&
		(strings.Contains(lower, "docker.sock") || strings.Contains(lower, "unix:///var/run/docker")):
		// 刻意不提供「把用户 / 插件加入 docker 组」这类引导：docker 组成员可挂载宿主
		// 文件系统，等价 root，引导加组等于绕过 ADR-0018 的内核提权通道与审计。
		// 这里只说清「需要什么访问权」，授权方式交给内核通道或管理员按最小权限决定。
		return opErrorf(pluginapi.E_UNSUPPORTED,
			"无权访问 Docker socket：容器操作需要访问 Docker socket；请通过内核提权通道授权，或由管理员按最小权限原则自行配置（%s）", truncateRunes(text, 300))
	case strings.Contains(lower, "no such container"), strings.Contains(lower, "no such object"),
		strings.Contains(lower, "no such image"), strings.Contains(lower, "manifest unknown"):
		return opErrorf(pluginapi.E_NOTFOUND, "容器或镜像不存在（%s）", truncateRunes(text, 300))
	case strings.Contains(lower, "pull access denied"), strings.Contains(lower, "authentication required"),
		strings.Contains(lower, "unauthorized: authentication"):
		return opErrorf(pluginapi.E_UNAUTHORIZED,
			"镜像拉取被拒：私有镜像需先 docker login，或确认镜像名拼写（%s）", truncateRunes(text, 300))
	case strings.Contains(lower, "image is being used by running container"),
		strings.Contains(lower, "image is being used by stopped container"):
		return opErrorf(pluginapi.E_CONFLICT,
			"镜像仍被容器引用，无法删除：先删除引用它的容器，或改用 force=true（%s）", truncateRunes(text, 300))
	case strings.Contains(lower, "invalid reference format"):
		return opErrorf(pluginapi.E_INVALID, "镜像引用格式不合法（%s）", truncateRunes(text, 300))
	}
	if text == "" {
		text = err.Error()
	}
	return opErrorf(pluginapi.E_UPSTREAM, "docker 命令失败：%s", truncateRunes(text, 500))
}

// sanitizeStderr 去掉空行、用 " | " 拼接非空行，避免多行错误体污染协议响应。
func sanitizeStderr(raw string) string {
	lines := strings.Split(raw, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, " | ")
}

// truncateRunes 按 rune 截断并追加省略号，避免把超长 stderr 塞进错误体。
func truncateRunes(raw string, limit int) string {
	runes := []rune(raw)
	if len(runes) <= limit {
		return raw
	}
	return string(runes[:limit]) + "…"
}

// splitLogLines 按行切分并执行单行 8 KiB 上限；尾部空行不额外产出空条目。
func splitLogLines(data []byte, capped bool) ([]string, bool) {
	truncated := capped
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	rawLines := strings.Split(text, "\n")
	if len(rawLines) > 0 && rawLines[len(rawLines)-1] == "" {
		rawLines = rawLines[:len(rawLines)-1]
	}
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		line = strings.TrimSuffix(line, "\r")
		if len(line) > MaxLogLineBytes {
			line = strings.ToValidUTF8(line[:MaxLogLineBytes], "")
			truncated = true
		}
		lines = append(lines, line)
	}
	return lines, truncated
}

// cappedBuffer 是 stdout/stderr 共用、并发安全、总量限长的缓冲。
// 超出预算时继续接受但丢弃并标记 truncated，避免子进程因写管道阻塞。
type cappedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int
	truncated bool
}

// newCappedBuffer 构造总量上限为 limit 字节的缓冲。
func newCappedBuffer(limit int) *cappedBuffer {
	return &cappedBuffer{limit: limit}
}

// Write 实现 io.Writer。
func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	total := len(p)
	remaining := b.limit - b.buf.Len()
	if remaining <= 0 {
		b.truncated = true
		return total, nil
	}
	if total > remaining {
		_, _ = b.buf.Write(p[:remaining])
		b.truncated = true
		return total, nil
	}
	_, _ = b.buf.Write(p)
	return total, nil
}

// Bytes 返回缓冲副本（调用方持有，不与写入竞争）。
func (b *cappedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buf.Bytes()...)
}

// String 返回缓冲文本。
func (b *cappedBuffer) String() string { return string(b.Bytes()) }

// Truncated 报告是否发生过丢弃。
func (b *cappedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

// 编译期断言：Client 实现 Executor。
var _ Executor = (*Client)(nil)
