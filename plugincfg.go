package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pengtang1024/kore-plugin-container/internal/containerop"
	"github.com/pengtang1024/kore-plugin-container/internal/plugincfg"
	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// 配置值边界：docker 命令超时 1s–5min（默认沿用 containerop.DefaultDockerTimeout=20s）。
const (
	minDockerTimeoutMS = 1_000
	maxDockerTimeoutMS = 300_000
)

// fileValues 是 config.json 的全量形状；字段与 schema.fields 一一对应。
type fileValues struct {
	DockerBin           string   `json:"docker_bin"`
	ProtectedContainers []string `json:"protected_containers"`
	DockerTimeoutMS     int      `json:"docker_timeout_ms"`
}

// configState 是运行期可变配置：值、来源与 0600 原子存储。
// config 值被 dispatch 按值传递，state 以指针共享，config.set 更新后同进程后续调用立即读到新值。
type configState struct {
	values fileValues
	source string
	store  *plugincfg.Store
}

// defaultValues 返回代码默认值（PLUGIN-CONFIG §4 优先级最低档）。
func defaultValues() fileValues {
	return fileValues{
		DockerBin:           containerop.DefaultDockerBin,
		ProtectedContainers: []string{},
		DockerTimeoutMS:     int(containerop.DefaultDockerTimeout / time.Millisecond),
	}
}

// configSchema 是 config.get 返回的表单描述（PLUGIN-CONFIG §2）。
// container 只通过本机 docker CLI 管理用户已有容器，不持有 daemon 连接凭据，
// 因此没有 socket 路径 / 默认网络 / 密码类字段——不臆造，也不虚标 sensitive（G-30）。
func configSchema() plugincfg.Schema {
	defaults := defaultValues()
	return plugincfg.Schema{
		Title: "容器管理配置",
		Fields: []plugincfg.Field{
			{
				Key: "docker_bin", Label: "Docker 命令行路径", Type: plugincfg.TypeString,
				Default:     mustJSON(defaults.DockerBin),
				Required:    true,
				Help:        "docker CLI 的命令名或绝对路径；留空用 docker（从 PATH 查找）。改动后下一次调用即生效。",
				Placeholder: "docker",
			},
			{
				Key: "protected_containers", Label: "保护容器名单", Type: plugincfg.TypeList, Item: plugincfg.TypeString,
				Default:     mustJSON(defaults.ProtectedContainers),
				Required:    false,
				Help:        "命中的容器名 / 完整 ID / ≥12 位短 ID 一律拒绝启停与删除；留空表示只保护插件自身容器。",
				Placeholder: "web",
			},
			{
				Key: "docker_timeout_ms", Label: "Docker 命令超时（毫秒）", Type: plugincfg.TypeInt,
				Default:  mustJSON(defaults.DockerTimeoutMS),
				Required: true,
				Help:     fmt.Sprintf("单条 docker 命令的等待上限，范围 %d–%d 毫秒。", minDockerTimeoutMS, maxDockerTimeoutMS),
				Min:      intPtr(minDockerTimeoutMS),
				Max:      intPtr(maxDockerTimeoutMS),
			},
		},
	}
}

// newConfigState 启动时构造运行期配置：校验顺序 file > env > default（§4）。
// 配置文件损坏或校验失败直接返回错误，由 loadConfig 让插件启动失败（§3）。
func newConfigState(stateDir string) (*configState, error) {
	store := plugincfg.NewStore(stateDir)
	base, envSet := envValues()
	if raw, exists, err := store.Read(); err != nil {
		return nil, err
	} else if exists {
		file, perr := decodeValues(raw)
		if perr != nil {
			return nil, perr
		}
		merged := fillValuesDefaults(file, base)
		validated, perr := validateValues(merged)
		if perr != nil {
			return nil, perr
		}
		return &configState{values: validated, source: plugincfg.SourceFile, store: store}, nil
	}
	validated, perr := validateValues(fillValuesDefaults(base, defaultValues()))
	if perr != nil {
		return nil, perr
	}
	source := plugincfg.SourceDefault
	if envSet {
		source = plugincfg.SourceEnv
	}
	return &configState{values: validated, source: source, store: store}, nil
}

// envValues 读取兼容的老环境变量，返回覆盖后的基线与「是否有字段被显式设置」。
func envValues() (fileValues, bool) {
	values := defaultValues()
	set := false
	if bin := strings.TrimSpace(os.Getenv(containerop.EnvDockerBin)); bin != "" {
		values.DockerBin = bin
		set = true
	}
	if raw := os.Getenv(containerop.EnvProtectedContainers); strings.TrimSpace(raw) != "" {
		protected, err := parseProtectedContainers(raw)
		if err == nil {
			values.ProtectedContainers = protected
			set = true
		}
	}
	return values, set
}

// decodeValues 解析 config.json 全量对象；未知字段忽略，类型不符报 E_INVALID。
func decodeValues(raw json.RawMessage) (fileValues, *pluginapi.Error) {
	var values fileValues
	if len(raw) == 0 || raw[0] != '{' {
		return fileValues{}, invalidConfig("config.json 必须是 JSON 对象")
	}
	if err := json.Unmarshal(raw, &values); err != nil {
		return fileValues{}, invalidConfig("config.json 字段类型不符: " + err.Error())
	}
	return values, nil
}

// fillValuesDefaults 用 fallback 补齐缺省字段（0 / nil / 空串视为未提供）。
func fillValuesDefaults(values, fallback fileValues) fileValues {
	if strings.TrimSpace(values.DockerBin) == "" {
		values.DockerBin = fallback.DockerBin
	}
	if values.ProtectedContainers == nil {
		values.ProtectedContainers = fallback.ProtectedContainers
	}
	if values.DockerTimeoutMS == 0 {
		values.DockerTimeoutMS = fallback.DockerTimeoutMS
	}
	return values
}

// validateValues 校验并归一化配置；启动加载与 config.set 共用同一套规则（§8）。
func validateValues(values fileValues) (fileValues, *pluginapi.Error) {
	bin := strings.TrimSpace(values.DockerBin)
	if bin == "" || strings.ContainsAny(bin, " \t\r\n\x00") {
		return fileValues{}, invalidConfig("docker_bin 必须是非空的命令名或绝对路径（不含空白字符）")
	}
	if strings.Contains(bin, "/") && !filepath.IsAbs(bin) {
		return fileValues{}, invalidConfig("docker_bin 含 / 时必须是绝对路径: " + bin)
	}
	values.DockerBin = bin

	protected := make([]string, 0, len(values.ProtectedContainers))
	seen := make(map[string]bool, len(values.ProtectedContainers))
	for _, name := range values.ProtectedContainers {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if perr := containerop.ValidateID(name); perr != nil {
			return fileValues{}, invalidConfig("protected_containers 含非法容器标识 " + name + ": " + perr.Message)
		}
		if !seen[name] {
			seen[name] = true
			protected = append(protected, name)
		}
	}
	values.ProtectedContainers = protected

	if values.DockerTimeoutMS < minDockerTimeoutMS || values.DockerTimeoutMS > maxDockerTimeoutMS {
		return fileValues{}, invalidConfig(fmt.Sprintf("docker_timeout_ms=%d 超出 [%d, %d]",
			values.DockerTimeoutMS, minDockerTimeoutMS, maxDockerTimeoutMS))
	}
	return values, nil
}

// invalidConfig 构造配置校验错误（E_INVALID）。
func invalidConfig(message string) *pluginapi.Error {
	return &pluginapi.Error{Code: pluginapi.E_INVALID, Message: message}
}

// mustJSON 把标量编成 json.RawMessage（schema 的 default 字段）。
func mustJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return encoded
}

// intPtr 返回整数指针（schema 的 min/max）。
func intPtr(value int) *int { return &value }

// configGet 组装 config.get 结果：schema + 当前值 + 来源；敏感字段统一掩码（§6）。
func configGet(cfg config) (json.RawMessage, *pluginapi.Error) {
	st, err := ensureConfigState(&cfg)
	if err != nil {
		return nil, err
	}
	values, marshalErr := json.Marshal(st.values)
	if marshalErr != nil {
		return nil, &pluginapi.Error{Code: pluginapi.E_INTERNAL, Message: "序列化当前配置失败: " + marshalErr.Error()}
	}
	masked, maskErr := plugincfg.MaskValues(configSchema(), values)
	if maskErr != nil {
		return nil, &pluginapi.Error{Code: pluginapi.E_INTERNAL, Message: "掩码配置值失败: " + maskErr.Error()}
	}
	result := plugincfg.GetResult{Schema: configSchema(), Values: masked, Source: st.source}
	encoded, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, &pluginapi.Error{Code: pluginapi.E_INTERNAL, Message: "序列化 config.get 结果失败: " + marshalErr.Error()}
	}
	return encoded, nil
}

// configSet 按「校验 → 原子写 → 更新内存 → 返回 applied」顺序保存配置（§3）。
// 三项配置都通过 activeRuntime 在每次方法调用时重读，无需重启 kored（§5）。
func configSet(raw json.RawMessage, cfg config) (json.RawMessage, *pluginapi.Error) {
	st, stateErr := ensureConfigState(&cfg)
	if stateErr != nil {
		return nil, stateErr
	}
	var params plugincfg.SetParams
	if len(raw) == 0 || string(raw) == "null" {
		return nil, invalidConfig("缺少必填字段 values")
	}
	if raw[0] != '{' {
		return nil, invalidConfig("params 必须是 JSON 对象")
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, invalidConfig("params 解析失败: " + err.Error())
	}
	if len(params.Values) == 0 || string(params.Values) == "null" {
		return nil, invalidConfig("缺少必填字段 values")
	}
	// 以当前值为基线做局部更新：未提供的字段保持原值。
	candidate := st.values
	if err := json.Unmarshal(params.Values, &candidate); err != nil {
		return nil, invalidConfig("values 字段类型不符: " + err.Error())
	}
	normalized, perr := validateValues(fillValuesDefaults(candidate, st.values))
	if perr != nil {
		return nil, perr
	}
	if st.store.Path() == "" {
		return nil, &pluginapi.Error{
			Code:    pluginapi.E_INTERNAL,
			Message: "KORE_STATE_DIR 未设置，无法保存插件配置",
			Detail:  json.RawMessage(`{"hint":"插件由内核启动时会注入 KORE_STATE_DIR；本地单独调试请先设置该环境变量。"}`),
		}
	}
	payload, marshalErr := json.Marshal(normalized)
	if marshalErr != nil {
		return nil, &pluginapi.Error{Code: pluginapi.E_INTERNAL, Message: "序列化配置失败: " + marshalErr.Error()}
	}
	if writeErr := st.store.Write(payload); writeErr != nil {
		return nil, &pluginapi.Error{
			Code:    pluginapi.E_INTERNAL,
			Message: "保存插件配置失败: " + writeErr.Error(),
			Detail:  json.RawMessage(`{"hint":"检查 KORE_STATE_DIR 是否可写（目录需 0700 且属主为运行用户）。"}`),
		}
	}
	st.values = normalized
	st.source = plugincfg.SourceFile
	result := plugincfg.SetResult{Saved: true, Applied: true, RestartRequired: false, Warnings: []string{}}
	encoded, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, &pluginapi.Error{Code: pluginapi.E_INTERNAL, Message: "序列化 config.set 结果失败: " + marshalErr.Error()}
	}
	return encoded, nil
}

// ensureConfigState 兜底初始化：loadConfig 正常情况下已建好 state；
// 测试或独立调用未建时按环境变量 + 默认构造一次性 state，避免空指针。
func ensureConfigState(cfg *config) (*configState, *pluginapi.Error) {
	if cfg.state != nil {
		return cfg.state, nil
	}
	st, err := newConfigState(os.Getenv("KORE_STATE_DIR"))
	if err != nil {
		return nil, &pluginapi.Error{Code: pluginapi.E_INTERNAL, Message: "加载插件配置失败: " + err.Error()}
	}
	cfg.state = st
	return st, nil
}

// activeRuntime 按当前配置重建 Runtime：config.set 保存后下一次业务调用立即用新值（§5）。
// 没有 state（测试注入 fake）时沿用调用方传入的 runtime。
func activeRuntime(cfg config, fallback *containerop.Runtime) *containerop.Runtime {
	if cfg.state == nil {
		return fallback
	}
	values := cfg.state.values
	return containerop.NewRuntime(containerop.Options{
		DockerBin: values.DockerBin,
		Timeout:   time.Duration(values.DockerTimeoutMS) * time.Millisecond,
		Protected: values.ProtectedContainers,
	}, nil)
}
