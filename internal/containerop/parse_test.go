package containerop

import (
	"strings"
	"testing"
)

// TestParsePSOutput 解析 docker ps --format json 的 fixture，字段归一正确。
func TestParsePSOutput(t *testing.T) {
	fixture := `{"Command":"\"/app/docker-entrypo…\"","CreatedAt":"2026-11-22 12:24:02 +0800 CST","ID":"474de2d57df2","Image":"maximhq/bifrost:v2.6.0","Names":"bifrost","Ports":"127.0.0.1:18080->8080/tcp","RunningFor":"15 hours ago","State":"running","Status":"Up 10 hours (healthy)"}
{"CreatedAt":"2026-11-23 03:00:00 +0800 CST","ID":"0123456789ab0123456789ab","Image":"nginx:alpine","Names":"kore-ct-test","Ports":"0.0.0.0:18081->80/tcp","RunningFor":"2 minutes ago","State":"running","Status":"Up 2 minutes"}
`
	entries, err := parsePSOutput([]byte(fixture))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("条目数=%d，期望 2", len(entries))
	}
	first := entries[0]
	if first.ID != "474de2d57df2" || first.Name != "bifrost" || first.Image != "maximhq/bifrost:v2.6.0" {
		t.Fatalf("首条基础字段不符: %+v", first)
	}
	if first.Created != "2026-11-22T12:24:02+08:00" {
		t.Fatalf("created 未归一为 RFC3339: %q", first.Created)
	}
	if first.State != "running" || first.Status == "" || first.RunningFor == "" || first.Ports == "" {
		t.Fatalf("首条状态字段不符: %+v", first)
	}
	if entries[1].ID != "0123456789ab" {
		t.Fatalf("短 ID 未截断为 12 位: %q", entries[1].ID)
	}
	if entries[1].Name != "kore-ct-test" {
		t.Fatalf("名称未归一: %q", entries[1].Name)
	}
}

// TestParsePSOutputEmpty 空输出返回空切片而不是 nil。
func TestParsePSOutputEmpty(t *testing.T) {
	entries, err := parsePSOutput(nil)
	if err != nil {
		t.Fatalf("空输出解析失败: %v", err)
	}
	if entries == nil || len(entries) != 0 {
		t.Fatalf("空输出结果=%v，期望空切片", entries)
	}
}

// TestParsePSOutputInvalid 非 JSON 行必须报错，不静默跳过。
func TestParsePSOutputInvalid(t *testing.T) {
	if _, err := parsePSOutput([]byte("not-json")); err == nil {
		t.Fatal("非法 JSON 未被拒绝")
	}
}

// TestParseInspectOutput 解析 inspect fixture：env 脱敏、网络/端口/挂载/资源/健康。
func TestParseInspectOutput(t *testing.T) {
	fixture := `[{
  "Id":"0123456789ab0123456789ab0123456789ab0123456789ab0123456789ab",
  "Name":"/kore-ct-test",
  "RestartCount":3,
  "State":{"Status":"running","Running":true,"StartedAt":"2026-11-23T03:00:00.123456789Z","Health":{"Status":"healthy","FailingStreak":0}},
  "Config":{"Image":"nginx:alpine","Cmd":["nginx","-g","daemon off;"],
    "Env":["PATH=/usr/sbin","APP_PASSWORD=s3cret","API_TOKEN=abc","MY_KEY=xyz","APP_NAME=demo"],
    "Labels":{"com.example":"1"}},
  "HostConfig":{"Memory":536870912,"CpuQuota":50000},
  "NetworkSettings":{"IPAddress":"",
    "Ports":{"80/tcp":[{"HostIp":"0.0.0.0","HostPort":"18081"}],"443/tcp":null},
    "Networks":{"bridge":{"IPAddress":"172.17.0.2"}}},
  "Mounts":[{"Type":"volume","Source":"/var/lib/docker/volumes/x/_data","Destination":"/data","Mode":"rw","RW":true}]
}]`
	result, err := parseInspectOutput([]byte(fixture))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if result == nil {
		t.Fatal("解析结果为空")
	}
	if result.ID != "0123456789ab0123456789ab0123456789ab0123456789ab0123456789ab" ||
		result.Name != "kore-ct-test" || result.Image != "nginx:alpine" {
		t.Fatalf("基础字段不符: %+v", result)
	}
	if result.State.Status != "running" || !result.State.Running || result.State.RestartCount != 3 ||
		result.State.StartedAt == "" {
		t.Fatalf("state 字段不符: %+v", result.State)
	}
	if result.Health != "healthy" {
		t.Fatalf("health=%q，期望 healthy", result.Health)
	}
	wantEnv := map[string]string{
		"PATH": "/usr/sbin", "APP_PASSWORD": "***", "API_TOKEN": "***",
		"MY_KEY": "***", "APP_NAME": "demo",
	}
	for _, entry := range result.Config.Env {
		name, value, _ := strings.Cut(entry, "=")
		if want, ok := wantEnv[name]; ok && value != want {
			t.Fatalf("env %s=%q，期望 %q（全量 %v）", name, value, want, result.Config.Env)
		}
	}
	if len(result.Config.Cmd) != 3 || result.Config.Labels["com.example"] != "1" {
		t.Fatalf("config.cmd/labels 不符: %+v", result.Config)
	}
	if result.Network.IP != "172.17.0.2" {
		t.Fatalf("network.ip=%q", result.Network.IP)
	}
	if len(result.Network.Ports) != 2 {
		t.Fatalf("ports 数=%d，期望 2: %+v", len(result.Network.Ports), result.Network.Ports)
	}
	published := result.Network.Ports[1]
	if published.ContainerPort != "80" || published.Protocol != "tcp" ||
		published.HostIP != "0.0.0.0" || published.HostPort != "18081" {
		t.Fatalf("发布端口不符: %+v", published)
	}
	if result.Network.Ports[0].ContainerPort != "443" || result.Network.Ports[0].HostPort != "" {
		t.Fatalf("未发布端口不符: %+v", result.Network.Ports[0])
	}
	if len(result.Mounts) != 1 || result.Mounts[0].Destination != "/data" || !result.Mounts[0].RW {
		t.Fatalf("mounts 不符: %+v", result.Mounts)
	}
	if result.Resources.MemoryLimit != 536870912 || result.Resources.CPUQuota != 50000 {
		t.Fatalf("resources 不符: %+v", result.Resources)
	}
}

// TestParseInspectOutputEmpty 空数组返回 nil（由调用方映射 E_NOTFOUND）。
func TestParseInspectOutputEmpty(t *testing.T) {
	result, err := parseInspectOutput([]byte(`[]`))
	if err != nil || result != nil {
		t.Fatalf("空数组结果=%v err=%v，期望 nil,nil", result, err)
	}
}

// TestMaskEnv 名称含 PASS/TOKEN/SECRET/KEY 的值替换 ***，其它保持原样。
func TestMaskEnv(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"APP_PASSWORD=s3cret",
		"ACCESS_TOKEN=abc",
		"CLIENT_SECRET=def",
		"PRIVATE_KEY=ghi",
		"NO_VALUE",
	}
	masked := maskEnv(env)
	if masked[0] != "PATH=/usr/bin" {
		t.Fatalf("PATH 被误脱敏: %q", masked[0])
	}
	for _, i := range []int{1, 2, 3, 4} {
		if !strings.HasSuffix(masked[i], "=***") {
			t.Fatalf("敏感变量未脱敏: %q", masked[i])
		}
	}
	if masked[5] != "NO_VALUE" {
		t.Fatalf("无值条目被改写: %q", masked[5])
	}
}

// TestSplitLogLines 单行超过 8 KiB 时截断并标记 truncated；短行原样。
func TestSplitLogLines(t *testing.T) {
	long := strings.Repeat("a", MaxLogLineBytes+1024)
	lines, truncated := splitLogLines([]byte(long+"\nshort\n"), false)
	if len(lines) != 2 {
		t.Fatalf("行数=%d，期望 2", len(lines))
	}
	if len(lines[0]) != MaxLogLineBytes || lines[1] != "short" {
		t.Fatalf("截断结果不符: first_len=%d second=%q", len(lines[0]), lines[1])
	}
	if !truncated {
		t.Fatal("超长行未标记 truncated")
	}
}

// TestSplitLogLinesCRLF 兼容 CRLF 且不额外产出尾部空行。
func TestSplitLogLinesCRLF(t *testing.T) {
	lines, truncated := splitLogLines([]byte("a\r\nb\r\n"), false)
	if len(lines) != 2 || lines[0] != "a" || lines[1] != "b" || truncated {
		t.Fatalf("CRLF 结果=%v truncated=%v", lines, truncated)
	}
}

// TestParseStatsOutput 解析 docker stats 的人类可读数值。
func TestParseStatsOutput(t *testing.T) {
	fixture := `{"BlockIO":"680kB / 6.05MB","CPUPerc":"0.37%","Container":"474de2d57df2","ID":"474de2d57df2","MemPerc":"11.56%","MemUsage":"118.4MiB / 1GiB","Name":"bifrost","NetIO":"5.43MB / 477kB","PIDs":"31"}`
	result, err := parseStatsOutput([]byte(fixture))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if result == nil {
		t.Fatal("解析结果为空")
	}
	if result.ID != "474de2d57df2" || result.Name != "bifrost" || result.CPUPercent != 0.37 {
		t.Fatalf("基础字段不符: %+v", result)
	}
	if result.Memory.Usage != 124151398 { // 118.4 MiB，按字节截断
		t.Fatalf("memory.usage=%d", result.Memory.Usage)
	}
	if result.Memory.Limit != 1<<30 || result.Memory.Percent != 11.56 {
		t.Fatalf("memory.limit/percent 不符: %+v", result.Memory)
	}
	if result.Network.Input != 5_430_000 || result.Network.Output != 477_000 {
		t.Fatalf("network 不符: %+v", result.Network)
	}
	if result.Block.Read != 680_000 || result.Block.Write != 6_050_000 {
		t.Fatalf("block 不符: %+v", result.Block)
	}
	if result.PIDs != 31 {
		t.Fatalf("pids=%d", result.PIDs)
	}
}

// TestParseStatsOutputEmpty 空输出返回 nil（由调用方映射状态冲突）。
func TestParseStatsOutputEmpty(t *testing.T) {
	result, err := parseStatsOutput(nil)
	if err != nil || result != nil {
		t.Fatalf("空输出结果=%v err=%v，期望 nil,nil", result, err)
	}
}

// TestParseSize 人类可读尺寸换算。
func TestParseSize(t *testing.T) {
	cases := map[string]int64{
		"0B": 0, "1kB": 1000, "477kB": 477000, "5.43MB": 5430000,
		"1KiB": 1024, "1.5MiB": 1572864, "1GiB": 1 << 30,
	}
	for raw, want := range cases {
		got, err := parseSize(raw)
		if err != nil {
			t.Fatalf("parseSize(%q) 失败: %v", raw, err)
		}
		if got != want {
			t.Fatalf("parseSize(%q)=%d，期望 %d", raw, got, want)
		}
	}
	if _, err := parseSize("12XB"); err == nil {
		t.Fatal("未知单位未被拒绝")
	}
}
