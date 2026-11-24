package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pengtang1024/kore-plugin-container/internal/containerop"
	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// embeddedInfo 是随二进制发布的内嵌清单：握手字段的运行期来源。
// 刻意不含 size_bytes：包体回填只改 info.json，不触碰二进制，避免自指震荡。
//
//go:embed manifest.json
var embeddedInfo []byte

// manifest 是 info.json 中插件运行期真正需要的字段，其余字段归内核校验。
type manifest struct {
	ID           string   `json:"id"`
	Version      string   `json:"version"`
	Protocol     string   `json:"protocol"`
	RequiresKore string   `json:"requires_kore"`
	Capabilities []string `json:"capabilities"`
}

// config 是插件运行期配置：环境变量 + 内嵌清单，不读任何配置文件。
// 容器插件只持有 docker CLI 路径与保护名单，不持有 daemon 连接凭据。
type config struct {
	socket              string
	id                  string
	token               string
	version             string
	requiresKore        string
	capabilities        []string
	dockerBin           string
	protectedContainers []string
	startedAt           time.Time
	state               *configState // 运行期可变配置（config.get/set 读写，指针共享保证热生效）
}

// loadConfig 读取协议环境变量并解析内嵌 info.json。
func loadConfig() (config, error) {
	var m manifest
	if err := json.Unmarshal(embeddedInfo, &m); err != nil {
		return config{}, fmt.Errorf("解析内嵌 info.json: %w", err)
	}
	if m.Protocol != pluginapi.Protocol {
		return config{}, fmt.Errorf("info.json.protocol=%q 与 SDK 协议 %q 不一致", m.Protocol, pluginapi.Protocol)
	}
	protected, err := parseProtectedContainers(os.Getenv(containerop.EnvProtectedContainers))
	if err != nil {
		return config{}, err
	}
	// 默认把承载插件自身的容器纳入保护，避免 manage 的容器把 kore 一起停掉。
	protected = dedupeStrings(append(protected, containerop.DetectSelfContainerIDs()...))
	dockerBin := strings.TrimSpace(os.Getenv(containerop.EnvDockerBin))
	if dockerBin == "" {
		dockerBin = containerop.DefaultDockerBin
	}
	cfg := config{
		socket:              os.Getenv("KORE_PLUGIN_SOCK"),
		id:                  os.Getenv("KORE_PLUGIN_ID"),
		token:               os.Getenv("KORE_PLUGIN_TOKEN"),
		version:             m.Version,
		requiresKore:        m.RequiresKore,
		capabilities:        m.Capabilities,
		dockerBin:           dockerBin,
		protectedContainers: protected,
		startedAt:           time.Now(),
	}
	if cfg.id == "" {
		cfg.id = m.ID
	}
	if cfg.socket == "" {
		return config{}, fmt.Errorf("缺少环境变量 KORE_PLUGIN_SOCK")
	}
	// 启动即加载插件配置：文件损坏或校验失败让插件启动失败，不静默回退默认值（§3）。
	state, err := newConfigState(os.Getenv("KORE_STATE_DIR"))
	if err != nil {
		return config{}, err
	}
	cfg.state = state
	return cfg, nil
}

// parseProtectedContainers 解析 KORE_PROTECTED_CONTAINERS（逗号分隔的名称 / ID）；
// 空值返回 nil，非法标识启动即报错，不静默丢弃。
func parseProtectedContainers(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	seen := make(map[string]bool, 4)
	containers := make([]string, 0, 4)
	for _, token := range strings.Split(raw, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if perr := containerop.ValidateID(token); perr != nil {
			return nil, fmt.Errorf("%s 含非法标识 %q: %s", containerop.EnvProtectedContainers, token, perr.Message)
		}
		if !seen[token] {
			seen[token] = true
			containers = append(containers, token)
		}
	}
	return containers, nil
}

// dedupeStrings 去重并保留首次出现顺序。
func dedupeStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
