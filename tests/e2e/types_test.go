//go:build linux

package e2e

import (
	"encoding/json"
	"testing"
)

// listEntry 是 container.list 的条目。
type listEntry struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Image      string `json:"image"`
	State      string `json:"state"`
	Status     string `json:"status"`
	Ports      string `json:"ports"`
	Created    string `json:"created"`
	RunningFor string `json:"running_for"`
}

// listResponse 是 container.list 的结果。
type listResponse struct {
	Containers []listEntry `json:"containers"`
	Count      int         `json:"count"`
	All        bool        `json:"all"`
}

// inspectState 是 container.inspect.state。
type inspectState struct {
	Status       string `json:"status"`
	Running      bool   `json:"running"`
	StartedAt    string `json:"started_at"`
	RestartCount int64  `json:"restart_count"`
}

// inspectConfig 是 container.inspect.config。
type inspectConfig struct {
	Env    []string          `json:"env"`
	Cmd    []string          `json:"cmd"`
	Labels map[string]string `json:"labels"`
}

// inspectPort 是 container.inspect.network.ports 的一项。
type inspectPort struct {
	ContainerPort string `json:"container_port"`
	Protocol      string `json:"protocol"`
	HostIP        string `json:"host_ip"`
	HostPort      string `json:"host_port"`
}

// inspectNetwork 是 container.inspect.network。
type inspectNetwork struct {
	IP    string        `json:"ip"`
	Ports []inspectPort `json:"ports"`
}

// inspectMount 是 container.inspect.mounts 的一项。
type inspectMount struct {
	Type        string `json:"type"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Mode        string `json:"mode"`
	RW          bool   `json:"rw"`
}

// inspectResources 是 container.inspect.resources。
type inspectResources struct {
	MemoryLimit int64 `json:"memory_limit"`
	CPUQuota    int64 `json:"cpu_quota"`
}

// inspectResponse 是 container.inspect 的结果。
type inspectResponse struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	Image     string           `json:"image"`
	State     inspectState     `json:"state"`
	Config    inspectConfig    `json:"config"`
	Network   inspectNetwork   `json:"network"`
	Mounts    []inspectMount   `json:"mounts"`
	Resources inspectResources `json:"resources"`
	Health    string           `json:"health"`
}

// logsResponse 是 container.logs 的结果。
type logsResponse struct {
	ID        string   `json:"id"`
	Lines     []string `json:"lines"`
	Truncated bool     `json:"truncated"`
}

// actionResponse 是 start/stop/restart 的结果。
type actionResponse struct {
	ID     string `json:"id"`
	Action string `json:"action"`
	OK     bool   `json:"ok"`
	State  string `json:"state"`
}

// memoryStats / netStats / blockStats / statsResponse 是 container.stats 的结果。
type memoryStats struct {
	Usage   int64   `json:"usage"`
	Limit   int64   `json:"limit"`
	Percent float64 `json:"percent"`
}

type netStats struct {
	Input  int64 `json:"input"`
	Output int64 `json:"output"`
}

type blockStats struct {
	Read  int64 `json:"read"`
	Write int64 `json:"write"`
}

type statsResponse struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	CPUPercent float64     `json:"cpu_percent"`
	Memory     memoryStats `json:"memory"`
	Network    netStats    `json:"network"`
	Block      blockStats  `json:"block"`
	PIDs       int         `json:"pids"`
}

// parseList 解析 list 结果。
func parseList(t *testing.T, raw string) listResponse {
	t.Helper()
	var result listResponse
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("解析 list 失败: %v (%s)", err, raw)
	}
	return result
}

// parseInspect 解析 inspect 结果。
func parseInspect(t *testing.T, raw string) inspectResponse {
	t.Helper()
	var result inspectResponse
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("解析 inspect 失败: %v (%s)", err, raw)
	}
	return result
}

// parseLogs 解析 logs 结果。
func parseLogs(t *testing.T, raw string) logsResponse {
	t.Helper()
	var result logsResponse
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("解析 logs 失败: %v (%s)", err, raw)
	}
	return result
}

// parseAction 解析 start/stop/restart 结果。
func parseAction(t *testing.T, raw string) actionResponse {
	t.Helper()
	var result actionResponse
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("解析 action 失败: %v (%s)", err, raw)
	}
	return result
}

// parseStats 解析 stats 结果。
func parseStats(t *testing.T, raw string) statsResponse {
	t.Helper()
	var result statsResponse
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("解析 stats 失败: %v (%s)", err, raw)
	}
	return result
}

// findEntry 按名称查找容器条目。
func findEntry(entries []listEntry, name string) (listEntry, bool) {
	for _, entry := range entries {
		if entry.Name == name {
			return entry, true
		}
	}
	return listEntry{}, false
}

// findPort 按容器端口与宿主端口查找映射。
func findPort(ports []inspectPort, containerPort, hostPort string) (inspectPort, bool) {
	for _, port := range ports {
		if port.ContainerPort == containerPort && port.HostPort == hostPort {
			return port, true
		}
	}
	return inspectPort{}, false
}
