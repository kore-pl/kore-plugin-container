package containerop

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// psRow 是 `docker ps --format '{{json .}}'` 单行输出的字段子集。
type psRow struct {
	ID         string `json:"ID"`
	Names      string `json:"Names"`
	Image      string `json:"Image"`
	State      string `json:"State"`
	Status     string `json:"Status"`
	Ports      string `json:"Ports"`
	CreatedAt  string `json:"CreatedAt"`
	RunningFor string `json:"RunningFor"`
}

// parsePSOutput 解析 docker ps 的逐行 JSON 输出（json.Decoder 支持连续对象）。
func parsePSOutput(data []byte) ([]ContainerEntry, error) {
	entries := make([]ContainerEntry, 0)
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var row psRow
		err := decoder.Decode(&row)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("解析 docker ps JSON 行失败: %w", err)
		}
		entries = append(entries, ContainerEntry{
			ID:         shortContainerID(row.ID),
			Name:       containerName(row.Names),
			Image:      row.Image,
			State:      row.State,
			Status:     row.Status,
			Ports:      row.Ports,
			Created:    normalizeCreated(row.CreatedAt),
			RunningFor: row.RunningFor,
		})
	}
	return entries, nil
}

// shortContainerID 返回 12 位短 ID（docker ps 已给短 ID，这里做兜底归一）。
func shortContainerID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// containerName 去掉 docker ps 可能带的前导斜杠；多名称时取第一个。
func containerName(names string) string {
	name := strings.TrimSpace(names)
	if idx := strings.IndexByte(name, ','); idx >= 0 {
		name = name[:idx]
	}
	return strings.TrimPrefix(strings.TrimSpace(name), "/")
}

// normalizeCreated 把 docker ps 的 "2006-01-02 15:04:05 -0700 MST" 归一为 RFC3339；
// 解析失败时保留原串，不编造时间。
func normalizeCreated(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if parsed, err := time.Parse("2006-01-02 15:04:05 -0700 MST", raw); err == nil {
		return parsed.Format(time.RFC3339)
	}
	return raw
}

// imageRow 是 `docker images --format '{{json .}}'` 单行输出的字段子集。
type imageRow struct {
	ID         string `json:"ID"`
	Repository string `json:"Repository"`
	Tag        string `json:"Tag"`
	CreatedAt  string `json:"CreatedAt"`
	Size       string `json:"Size"`
}

// parseImagesOutput 解析 docker images 的逐行 JSON 输出（json.Decoder 支持连续对象）。
func parseImagesOutput(data []byte) ([]ImageEntry, error) {
	images := make([]ImageEntry, 0)
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var row imageRow
		err := decoder.Decode(&row)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("解析 docker images JSON 行失败: %w", err)
		}
		entry := ImageEntry{
			ID:         strings.TrimSpace(row.ID),
			Repository: strings.TrimSpace(row.Repository),
			Tag:        strings.TrimSpace(row.Tag),
			Size:       strings.TrimSpace(row.Size),
			Created:    normalizeCreated(row.CreatedAt),
		}
		entry.Dangling = entry.Repository == "<none>" && entry.Tag == "<none>"
		images = append(images, entry)
	}
	return images, nil
}

// rawInspect 是 `docker inspect` 单容器的字段子集；只解析本插件对外暴露的字段。
type rawInspect struct {
	ID           string            `json:"Id"`
	Name         string            `json:"Name"`
	RestartCount int64             `json:"RestartCount"`
	State        rawInspectState   `json:"State"`
	Config       rawInspectConfig  `json:"Config"`
	HostConfig   rawInspectHost    `json:"HostConfig"`
	Network      rawInspectNetwork `json:"NetworkSettings"`
	Mounts       []rawInspectMount `json:"Mounts"`
}

// rawInspectState 是 State 子结构。
type rawInspectState struct {
	Status    string            `json:"Status"`
	Running   bool              `json:"Running"`
	StartedAt string            `json:"StartedAt"`
	Health    *rawInspectHealth `json:"Health"`
}

// rawInspectHealth 是健康检查状态。
type rawInspectHealth struct {
	Status string `json:"Status"`
}

// rawInspectConfig 是 Config 子结构。
type rawInspectConfig struct {
	Image  string            `json:"Image"`
	Cmd    []string          `json:"Cmd"`
	Env    []string          `json:"Env"`
	Labels map[string]string `json:"Labels"`
}

// rawInspectHost 是 HostConfig 子结构。
type rawInspectHost struct {
	Memory        int64 `json:"Memory"`
	CPUQuota      int64 `json:"CpuQuota"`
	RestartPolicy struct {
		Name string `json:"Name"`
	} `json:"RestartPolicy"`
}

// rawInspectNetwork 是 NetworkSettings 子结构。
type rawInspectNetwork struct {
	IPAddress string                               `json:"IPAddress"`
	Ports     map[string][]rawInspectPortBinding   `json:"Ports"`
	Networks  map[string]rawInspectNetworkEndpoint `json:"Networks"`
}

// rawInspectPortBinding 是一条端口绑定。
type rawInspectPortBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

// rawInspectNetworkEndpoint 是单个网络端点的 IP。
type rawInspectNetworkEndpoint struct {
	IPAddress string `json:"IPAddress"`
}

// rawInspectMount 是一条挂载。
type rawInspectMount struct {
	Type        string `json:"Type"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
	Mode        string `json:"Mode"`
	RW          bool   `json:"RW"`
}

// parseInspectOutput 解析 docker inspect 的数组输出并收敛为精简结构。
// 空数组返回 (nil, nil)，由调用方映射为 E_NOTFOUND。
func parseInspectOutput(data []byte) (*InspectResult, error) {
	var rows []rawInspect
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("解析 docker inspect JSON 失败: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	raw := rows[0]
	env := maskEnv(raw.Config.Env)
	if env == nil {
		env = []string{}
	}
	cmd := append([]string(nil), raw.Config.Cmd...)
	if cmd == nil {
		cmd = []string{}
	}
	labels := make(map[string]string, len(raw.Config.Labels))
	for key, value := range raw.Config.Labels {
		labels[key] = value
	}
	mounts := make([]InspectMount, 0, len(raw.Mounts))
	for _, mount := range raw.Mounts {
		mounts = append(mounts, InspectMount{
			Type:        mount.Type,
			Source:      mount.Source,
			Destination: mount.Destination,
			Mode:        mount.Mode,
			RW:          mount.RW,
		})
	}
	health := ""
	if raw.State.Health != nil {
		health = raw.State.Health.Status
	}
	return &InspectResult{
		ID:    raw.ID,
		Name:  strings.TrimPrefix(raw.Name, "/"),
		Image: raw.Config.Image,
		State: InspectState{
			Status:       raw.State.Status,
			Running:      raw.State.Running,
			StartedAt:    raw.State.StartedAt,
			RestartCount: raw.RestartCount,
		},
		Config: InspectConfig{Env: env, Cmd: cmd, Labels: labels},
		Network: InspectNetwork{
			IP:    networkIP(raw.Network),
			Ports: buildPorts(raw.Network.Ports),
		},
		Mounts: mounts,
		Resources: InspectResources{MemoryLimit: raw.HostConfig.Memory, CPUQuota: raw.HostConfig.CPUQuota,
			RestartPolicy: raw.HostConfig.RestartPolicy.Name},
		Health: health,
	}, nil
}

// sensitiveEnvMarkers 是 env 名称里出现即脱敏的片段（大小写不敏感），与需求口径一致。
var sensitiveEnvMarkers = []string{"PASS", "TOKEN", "SECRET", "KEY"}

// maskEnv 把敏感环境变量的值替换为 ***；只改 "NAME=value" 形态，保留变量名。
func maskEnv(env []string) []string {
	if env == nil {
		return nil
	}
	masked := make([]string, len(env))
	for i, entry := range env {
		name, _, hasValue := strings.Cut(entry, "=")
		if !hasValue {
			masked[i] = entry
			continue
		}
		upper := strings.ToUpper(name)
		sensitive := false
		for _, marker := range sensitiveEnvMarkers {
			if strings.Contains(upper, marker) {
				sensitive = true
				break
			}
		}
		if sensitive {
			masked[i] = name + "=***"
		} else {
			masked[i] = entry
		}
	}
	return masked
}

// networkIP 取容器 IP：优先按网络名排序后的首个非空 IP，其次 NetworkSettings.IPAddress。
func networkIP(settings rawInspectNetwork) string {
	names := make([]string, 0, len(settings.Networks))
	for name := range settings.Networks {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if ip := strings.TrimSpace(settings.Networks[name].IPAddress); ip != "" {
			return ip
		}
	}
	return strings.TrimSpace(settings.IPAddress)
}

// buildPorts 把 map 形态的端口绑定收敛为按容器端口排序的数组。
func buildPorts(bindings map[string][]rawInspectPortBinding) []InspectPort {
	ports := make([]InspectPort, 0, len(bindings))
	keys := make([]string, 0, len(bindings))
	for key := range bindings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		port, protocol, _ := strings.Cut(key, "/")
		items := append([]rawInspectPortBinding(nil), bindings[key]...)
		sort.Slice(items, func(i, j int) bool {
			if items[i].HostIP != items[j].HostIP {
				return items[i].HostIP < items[j].HostIP
			}
			return items[i].HostPort < items[j].HostPort
		})
		if len(items) == 0 {
			ports = append(ports, InspectPort{ContainerPort: port, Protocol: protocol})
			continue
		}
		for _, item := range items {
			ports = append(ports, InspectPort{
				ContainerPort: port,
				Protocol:      protocol,
				HostIP:        item.HostIP,
				HostPort:      item.HostPort,
			})
		}
	}
	return ports
}

// rawStats 是 `docker stats --no-stream --format '{{json .}}'` 的字段子集。
type rawStats struct {
	Name     string `json:"Name"`
	ID       string `json:"ID"`
	CPUPerc  string `json:"CPUPerc"`
	MemUsage string `json:"MemUsage"`
	MemPerc  string `json:"MemPerc"`
	NetIO    string `json:"NetIO"`
	BlockIO  string `json:"BlockIO"`
	PIDs     string `json:"PIDs"`
}

// parseStatsOutput 解析单条 stats JSON；空输出返回 (nil, nil)，由调用方映射为状态冲突。
func parseStatsOutput(data []byte) (*StatsResult, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var row rawStats
	if err := decoder.Decode(&row); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, fmt.Errorf("解析 docker stats JSON 失败: %w", err)
	}
	cpu, err := parsePercent(row.CPUPerc)
	if err != nil {
		return nil, err
	}
	memPercent, err := parsePercent(row.MemPerc)
	if err != nil {
		return nil, err
	}
	usage, limit, err := parsePair(row.MemUsage)
	if err != nil {
		return nil, err
	}
	netIn, netOut, err := parsePair(row.NetIO)
	if err != nil {
		return nil, err
	}
	blockRead, blockWrite, err := parsePair(row.BlockIO)
	if err != nil {
		return nil, err
	}
	pids, err := strconv.Atoi(strings.TrimSpace(row.PIDs))
	if err != nil {
		return nil, fmt.Errorf("无法解析 PIDs=%q: %w", row.PIDs, err)
	}
	return &StatsResult{
		ID:         row.ID,
		Name:       row.Name,
		CPUPercent: cpu,
		Memory:     MemoryStats{Usage: usage, Limit: limit, Percent: memPercent},
		Network:    NetStats{Input: netIn, Output: netOut},
		Block:      BlockStats{Read: blockRead, Write: blockWrite},
		PIDs:       pids,
	}, nil
}

// parsePercent 解析 "0.37%" 形态的百分比。
func parsePercent(raw string) (float64, error) {
	trimmed := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(raw), "%"))
	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, fmt.Errorf("无法解析百分比 %q: %w", raw, err)
	}
	return value, nil
}

// parsePair 解析 "A / B" 形态的统计对（如 "118.4MiB / 1GiB"）。
func parsePair(raw string) (int64, int64, error) {
	parts := strings.SplitN(raw, "/", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("无法解析统计对 %q（期望 \"A / B\"）", raw)
	}
	first, err := parseSize(parts[0])
	if err != nil {
		return 0, 0, err
	}
	second, err := parseSize(parts[1])
	if err != nil {
		return 0, 0, err
	}
	return first, second, nil
}

// sizeRE 匹配 docker 输出的人类可读尺寸数值与单位。
var sizeRE = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)\s*([A-Za-z]*)$`)

// sizeUnits 是 docker 的十进制（kB/MB/GB）与二进制（KiB/MiB/GiB）单位。
var sizeUnits = map[string]float64{
	"": 1, "B": 1,
	"kB": 1e3, "KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12, "PB": 1e15,
	"KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30, "TiB": 1 << 40, "PiB": 1 << 50,
}

// parseSize 把 "118.4MiB" / "477kB" / "0B" 换算为字节；未知单位直接报错，不静默当 0。
func parseSize(raw string) (int64, error) {
	trimmed := strings.TrimSpace(raw)
	matches := sizeRE.FindStringSubmatch(trimmed)
	if matches == nil {
		return 0, fmt.Errorf("无法解析尺寸 %q", raw)
	}
	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, fmt.Errorf("无法解析尺寸数值 %q: %w", raw, err)
	}
	unit, ok := sizeUnits[matches[2]]
	if !ok {
		return 0, fmt.Errorf("未知尺寸单位 %q（来自 %q）", matches[2], raw)
	}
	return int64(value * unit), nil
}
