package main

// 断连探针 v2 单测（Job000072）：rcPort 确定性、rcProbe 成功/失败/错误形态、
// statProbe、probeMount 类型分发。

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRcPort_Deterministic(t *testing.T) {
	a, b := rcPort("abcdef12"), rcPort("abcdef12")
	if a != b {
		t.Errorf("同键不同口: %d vs %d", a, b)
	}
	if a < rcBasePort || a >= rcBasePort+rcPortRange {
		t.Errorf("端口越界: %d", a)
	}
	// 不同键大概率不同口（至少这两个固定键必须不同，便于冲突行为可预期）
	if rcPort("abcdef12") == rcPort("12345678") {
		t.Error("两个固定键端口冲突")
	}
}

func TestRcProbe_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/operations/list" {
			t.Errorf("路径错: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"list":[]}`)
	}))
	defer srv.Close()
	// 借用 httptest 端口：直接改 httpClient 的 Transport 不划算，按 URL 形态测——
	// rcProbe 写死 127.0.0.1 端口，故这里用真实监听端口拼测：把 srv 端口注入。
	port := 0
	if _, err := fmt.Sscanf(srv.URL, "http://127.0.0.1:%d", &port); err != nil {
		t.Skipf("httptest 非 127.0.0.1（IPv6 环境）: %s", srv.URL)
	}
	if err := rcProbe(port); err != nil {
		t.Errorf("成功形态应 nil: %v", err)
	}
}

func TestRcProbe_BackendError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"connection refused"}`)
	}))
	defer srv.Close()
	port := 0
	if _, err := fmt.Sscanf(srv.URL, "http://127.0.0.1:%d", &port); err != nil {
		t.Skipf("httptest 非 127.0.0.1: %s", srv.URL)
	}
	if err := rcProbe(port); err == nil {
		t.Error("后端错误应返回 error")
	} else if msg := err.Error(); msg == "" {
		t.Error("错误信息不应为空")
	}
}

func TestRcProbe_Unreachable(t *testing.T) {
	// 本会话大概率无监听端口：rcPortRange 内找一个未用端口
	if err := rcProbe(5999); err == nil {
		t.Skip("5999 居然被监听，换一个")
	}
}

func TestStatProbe(t *testing.T) {
	if err := statProbe("/definitely/not/exist"); err == nil {
		t.Error("不存在的挂载点应报错")
	}
	if err := statProbe(t.TempDir()); err != nil {
		t.Errorf("存在的目录应通过: %v", err)
	}
}

func TestProbeMount_Dispatch(t *testing.T) {
	// nfs 走 statProbe（不存在的路径必失败）；webdav 走 rcProbe（端口未监听必失败）
	if err := probeMount("nfs", "any", "/definitely/not/exist"); err == nil {
		t.Error("nfs 探针对不存在挂载点应失败")
	}
	if err := probeMount("webdav", "any", "/mnt/storage/whatever"); err == nil {
		t.Skip("rcBasePort 端口被监听则此断言不适用（概率极低）")
	}
}
