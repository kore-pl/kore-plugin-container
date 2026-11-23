package containerop

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// containerIDRE 是容器标识的唯一合法形状（名称 / 完整 ID / 短 ID 共用）：
// 首字符必须是字母或数字，其后仅允许字母、数字、下划线、点与连字符，总长 1..128。
// 该正则同时封死命令注入：空白、斜杠、分号、$()、引号、NUL、以 "-" 开头全部不匹配。
var containerIDRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

// hex64RE 从 cgroup / mountinfo 文本里提取 64 位小写 hex 容器 ID。
var hex64RE = regexp.MustCompile(`[0-9a-f]{64}`)

// hex12RE 匹配 Docker 默认 HOSTNAME 形态（12 位小写 hex 短 ID）。
var hex12RE = regexp.MustCompile(`^[0-9a-f]{12}$`)

// ValidateID 校验容器标识形状；不访问 daemon、不执行任何运行时调用。
func ValidateID(raw string) *pluginapi.Error {
	if raw == "" {
		return invalid("容器标识不能为空")
	}
	if !containerIDRE.MatchString(raw) {
		return invalid("容器标识非法：须匹配 ^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$（拒绝空白、斜杠、分号、$() 等注入字符，且不得以 - 开头）")
	}
	return nil
}

// sinceRE 是 container.logs 的 since 形状：docker --since 接受 Go 时长（10m）、
// Unix 秒（1730000000）或 RFC3339（2026-11-23T08:00:00+08:00）——共用的合法字符集
// 为字母数字与 . + : T - ；限制形状只防明显误传，注入面由固定 argv 本身封死。
var sinceRE = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+:T-]{0,63}$`)

// ValidateSince 校验 since 参数形状；空串表示未指定，直接放行。
func ValidateSince(raw string) *pluginapi.Error {
	if raw == "" || sinceRE.MatchString(raw) {
		return nil
	}
	return invalid("since 非法：支持 Go 时长（如 10m）、Unix 秒或 RFC3339 时间戳")
}

// CheckProtected 判断容器是否命中保护名单。
// 名单项可以是名称、完整 64 位 ID，或 ≥6 位的十六进制短 ID；命中返回 E_UNAUTHORIZED。
func CheckProtected(ref ContainerRef, protected []string) *pluginapi.Error {
	for _, raw := range protected {
		token := strings.TrimPrefix(strings.TrimSpace(raw), "/")
		if token == "" {
			continue
		}
		if token == ref.Name || token == ref.ID {
			return protectedError(ref)
		}
		// 短 ID：只对 ≥6 位十六进制做双向前缀匹配——既覆盖「名单给短 ID、命中完整 ID」，
		// 也覆盖「名单给完整 ID、list 只拿到 12 位短 ID」的保护标记场景，
		// 同时避免单字符名单误伤全部容器。
		if len(token) >= 6 && isHex(token) && isHex(ref.ID) &&
			(strings.HasPrefix(ref.ID, strings.ToLower(token)) ||
				len(ref.ID) >= 6 && strings.HasPrefix(strings.ToLower(token), ref.ID)) {
			return protectedError(ref)
		}
	}
	return nil
}

// protectedError 构造保护名单拒绝错误；只回容器名，不回宿主机路径。
func protectedError(ref ContainerRef) *pluginapi.Error {
	return unauthorized(fmt.Sprintf("拒绝操作容器 %q：命中保护名单 %s", ref.Name, EnvProtectedContainers))
}

// DetectSelfContainerIDs 从 /proc/self/cgroup 与 HOSTNAME 推断「承载插件自身的容器」ID。
// 插件不在容器内（宿主进程）时返回空列表，不猜测、不误伤无关容器。
func DetectSelfContainerIDs() []string {
	seen := make(map[string]bool, 2)
	ids := make([]string, 0, 2)
	add := func(id string) {
		id = strings.ToLower(strings.TrimSpace(id))
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if data, err := os.ReadFile("/proc/self/cgroup"); err == nil {
		for _, id := range hex64RE.FindAllString(string(data), -1) {
			add(id)
		}
	}
	if hostname := strings.TrimSpace(os.Getenv("HOSTNAME")); hex12RE.MatchString(hostname) ||
		len(hostname) == 64 && isHex(hostname) {
		add(hostname)
	}
	return ids
}

// isHex 判断字符串是否全为十六进制字符（大小写不敏感）。
func isHex(raw string) bool {
	if raw == "" {
		return false
	}
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' {
			continue
		}
		return false
	}
	return true
}
