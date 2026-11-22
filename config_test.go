package main

import "testing"

// TestParseProtectedContainers 合法逗号名单去空白/去重；非法标识启动即报错。
func TestParseProtectedContainers(t *testing.T) {
	got, err := parseProtectedContainers(" web, kore-ct-test ,, web,0123456789ab ")
	if err != nil {
		t.Fatalf("合法名单解析失败: %v", err)
	}
	want := []string{"web", "kore-ct-test", "0123456789ab"}
	if len(got) != len(want) {
		t.Fatalf("解析结果=%v，期望 %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("解析结果=%v，期望 %v", got, want)
		}
	}
}

// TestParseProtectedContainersEmpty 空值返回 nil。
func TestParseProtectedContainersEmpty(t *testing.T) {
	got, err := parseProtectedContainers("  ")
	if err != nil || got != nil {
		t.Fatalf("空名单结果=%v err=%v，期望 nil,nil", got, err)
	}
}

// TestParseProtectedContainersReject 注入形状的名单直接拒绝。
func TestParseProtectedContainersReject(t *testing.T) {
	for _, raw := range []string{"web;rm -rf /", "-web", "web name", "web/../etc"} {
		if _, err := parseProtectedContainers(raw); err == nil {
			t.Fatalf("非法名单 %q 未被拒绝", raw)
		}
	}
}

// TestDedupeStrings 去重保留首次顺序。
func TestDedupeStrings(t *testing.T) {
	got := dedupeStrings([]string{"b", "a", "b", "", "a"})
	if len(got) != 2 || got[0] != "b" || got[1] != "a" {
		t.Fatalf("去重结果=%v", got)
	}
}
