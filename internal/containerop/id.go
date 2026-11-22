package containerop

import (
	"encoding/hex"
	"strings"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// ValidateID 校验容器标识：接受 64 位小写 hex 的完整 ID，或合法的容器名称/短标识。
// 名称规则：1-128 位 [a-zA-Z0-9][a-zA-Z0-9_.-]*，不允许空白与路径穿越字符。
// 只做字符串校验，不访问 daemon、不做任何运行时调用。
func ValidateID(raw string) (kind string, perr *pluginapi.Error) {
	if raw == "" {
		return "", invalid("容器标识不能为空")
	}
	if len(raw) == 64 {
		if _, err := hex.DecodeString(raw); err == nil && strings.ToLower(raw) == raw {
			return "id64", nil
		}
	}
	if perr := validateName(raw); perr != nil {
		return "", perr
	}
	return "name", nil
}

// validateName 校验容器名称/短标识形状。
func validateName(raw string) *pluginapi.Error {
	if len(raw) > maxNameLen {
		return invalid("容器名称超过 " + itoa(maxNameLen) + " 字符上限")
	}
	first := raw[0]
	if !isAlnum(first) {
		return invalid("容器名称必须以字母或数字开头")
	}
	for i := 1; i < len(raw); i++ {
		c := raw[i]
		if !isAlnum(c) && c != '_' && c != '.' && c != '-' {
			return invalid("容器名称含非法字符")
		}
	}
	return nil
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// itoa 是本包内最小的正整数转字符串，避免为一个上限值引入 strconv。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
