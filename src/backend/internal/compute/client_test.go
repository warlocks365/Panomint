package compute

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// recorded 假服务端记录到的一次请求。
type recorded struct {
	Path string
	Auth string
	Body map[string]any
}

// fakeServer 起一个记录请求的假控制端。
// handler 为 nil 时对三个端点都返回合理的最小响应。
func fakeServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *[]recorded, *sync.Mutex) {
	t.Helper()
	var (
		mu   sync.Mutex
		seen []recorded
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := map[string]any{}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &body)
		}
		mu.Lock()
		seen = append(seen, recorded{Path: r.URL.Path, Auth: r.Header.Get("Authorization"), Body: body})
		mu.Unlock()

		if handler != nil {
			handler(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/compute-nodes/agent/heartbeat":
			_, _ = w.Write([]byte(`{"node_id":"n1","status":"online","heartbeat_interval_seconds":60,"offline_after_seconds":180}`))
		case "/compute-nodes/agent/poll":
			_, _ = w.Write([]byte(`{"jobs":[]}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &seen, &mu
}

// TestClientPathsAndAuth 三个方法的路径与 Bearer 头必须与协议一致。
func TestClientPathsAndAuth(t *testing.T) {
	srv, seen, mu := fakeServer(t, nil)
	c := NewClient(srv.URL+"/", "tok-123") // 故意带结尾斜杠，验证会被规范化

	ctx := context.Background()
	if _, err := c.Heartbeat(ctx, HeartbeatRequest{Status: string(StatusOnline)}); err != nil {
		t.Fatalf("Heartbeat 出错: %v", err)
	}
	if _, err := c.Poll(ctx, 3); err != nil {
		t.Fatalf("Poll 出错: %v", err)
	}
	if err := c.Submit(ctx, ResultRequest{JobID: "j1", Status: JobStatusDone}); err != nil {
		t.Fatalf("Submit 出错: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(*seen) != 3 {
		t.Fatalf("应收到 3 次请求，实际 %d", len(*seen))
	}
	wantPaths := []string{"/compute-nodes/agent/heartbeat", "/compute-nodes/agent/poll", "/compute-nodes/agent/result"}
	for i, want := range wantPaths {
		if (*seen)[i].Path != want {
			t.Fatalf("第 %d 次请求路径 = %q，期望 %q（注意结尾斜杠是否被正确处理）", i, (*seen)[i].Path, want)
		}
		if (*seen)[i].Auth != "Bearer tok-123" {
			t.Fatalf("第 %d 次请求 Authorization = %q，期望 %q", i, (*seen)[i].Auth, "Bearer tok-123")
		}
	}
	// poll 的 max 必须传出去
	if v, ok := (*seen)[1].Body["max"]; !ok || v.(float64) != 3 {
		t.Fatalf("poll 请求体应含 max=3，实际 %v", (*seen)[1].Body)
	}
	// result 的字段名必须与协议一致（服务端按这些名字解析）
	for _, k := range []string{"job_id", "status"} {
		if _, ok := (*seen)[2].Body[k]; !ok {
			t.Fatalf("result 请求体缺少字段 %q：%v", k, (*seen)[2].Body)
		}
	}
}

// TestClientHeartbeatCapabilityFields 能力声明的 JSON 字段名与零值语义。
//
// 关键：has_nvenc 为 false 时**必须仍出现在 JSON 里**（指针非 nil），
// 否则服务端会把它当成"未上报"而保留旧值——一个从 GPU 换成 CPU 的节点
// 会一直谎报自己还有 NVENC。
func TestClientHeartbeatCapabilityFields(t *testing.T) {
	srv, seen, mu := fakeServer(t, nil)
	c := NewClient(srv.URL, "t")

	codecs := "h264"
	hasNV := false
	vram := 0
	conc := 2
	_, err := c.Heartbeat(context.Background(), HeartbeatRequest{
		Status:      string(StatusOnline),
		Codecs:      &codecs,
		HasNVENC:    &hasNV,
		VRAMMB:      &vram,
		Concurrency: &conc,
	})
	if err != nil {
		t.Fatalf("Heartbeat 出错: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	body := (*seen)[0].Body
	if v, ok := body["has_nvenc"]; !ok || v.(bool) != false {
		t.Fatalf("has_nvenc=false 必须显式出现在 JSON 中，实际 %v", body)
	}
	if v, ok := body["vram_mb"]; !ok || v.(float64) != 0 {
		t.Fatalf("vram_mb=0 必须显式出现，实际 %v", body)
	}
	if v, ok := body["codecs"]; !ok || v.(string) != "h264" {
		t.Fatalf("codecs 应为 h264，实际 %v", body)
	}
}

// TestClientHeartbeatOmitsUnreportedFields 未上报的可选字段不得出现（否则会覆盖服务端旧值）。
func TestClientHeartbeatOmitsUnreportedFields(t *testing.T) {
	srv, seen, mu := fakeServer(t, nil)
	c := NewClient(srv.URL, "t")
	if _, err := c.Heartbeat(context.Background(), HeartbeatRequest{Status: string(StatusOnline)}); err != nil {
		t.Fatalf("Heartbeat 出错: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, k := range []string{"codecs", "has_nvenc", "vram_mb", "concurrency", "gpu_util"} {
		if _, ok := (*seen)[0].Body[k]; ok {
			t.Fatalf("未上报的字段 %q 不该出现在 JSON 中（会让服务端误以为要覆盖）：%v", k, (*seen)[0].Body)
		}
	}
}

// TestClientErrorParsing 非 2xx：解析错误包络、可读错误、IsFatal 判定。
func TestClientErrorParsing(t *testing.T) {
	t.Run("401 → IsFatal 为真且带业务码", func(t *testing.T) {
		srv, _, _ := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":"INVALID_AGENT_TOKEN","message":"节点令牌无效或已过期"}}`))
		})
		_, err := NewClient(srv.URL, "bad").Heartbeat(context.Background(), HeartbeatRequest{})
		if err == nil {
			t.Fatal("401 必须返回错误")
		}
		if !IsFatal(err) {
			t.Fatalf("401 应判为 fatal（重试无意义）：%v", err)
		}
		var he *HTTPError
		if !errors.As(err, &he) {
			t.Fatalf("错误应为 *HTTPError，实际 %T", err)
		}
		if he.Status != 401 || he.Code != "INVALID_AGENT_TOKEN" {
			t.Fatalf("HTTPError 字段不对: %+v", he)
		}
		if !strings.Contains(he.Error(), "INVALID_AGENT_TOKEN") {
			t.Fatalf("错误串应含业务码便于排障：%s", he.Error())
		}
	})

	t.Run("403 → 同样 fatal", func(t *testing.T) {
		srv, _, _ := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		})
		_, err := NewClient(srv.URL, "t").Poll(context.Background(), 1)
		if !IsFatal(err) {
			t.Fatalf("403 应判为 fatal：%v", err)
		}
	})

	t.Run("500 → 非 fatal（网络/服务端问题，值得退避重试）", func(t *testing.T) {
		srv, _, _ := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"code":"INTERNAL","message":"内部错误"}}`))
		})
		_, err := NewClient(srv.URL, "t").Heartbeat(context.Background(), HeartbeatRequest{})
		if err == nil {
			t.Fatal("500 必须返回错误")
		}
		if IsFatal(err) {
			t.Fatalf("500 不该判为 fatal（否则一次抖动就退出）：%v", err)
		}
	})

	t.Run("非 JSON 错误体也要可读", func(t *testing.T) {
		srv, _, _ := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("nginx is sleeping"))
		})
		_, err := NewClient(srv.URL, "t").Poll(context.Background(), 1)
		if err == nil {
			t.Fatal("必须返回错误")
		}
		if IsFatal(err) {
			t.Fatal("502 不该判为 fatal")
		}
		if !strings.Contains(err.Error(), "502") {
			t.Fatalf("错误串应含 HTTP 状态码：%s", err.Error())
		}
	})

	t.Run("网络不可达 → 非 fatal 且错误可读", func(t *testing.T) {
		// 指向一个已关闭的地址
		srv, _, _ := fakeServer(t, nil)
		url := srv.URL
		srv.Close()
		_, err := NewClient(url, "t").Heartbeat(context.Background(), HeartbeatRequest{})
		if err == nil {
			t.Fatal("连接失败必须返回错误")
		}
		if IsFatal(err) {
			t.Fatal("网络错误不该判为 fatal（应退避重试）")
		}
	})
}

// TestClientPollNullJobs Poll 返回 jobs=null 时必须归一化为空切片，
// 让上层只需处理"空切片"一种情况。
func TestClientPollNullJobs(t *testing.T) {
	srv, _, _ := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jobs":null,"server_time":"2026-01-01T00:00:00Z"}`))
	})
	resp, err := NewClient(srv.URL, "t").Poll(context.Background(), 1)
	if err != nil {
		t.Fatalf("Poll 出错: %v", err)
	}
	if resp.Jobs == nil {
		t.Fatal("jobs=null 应被归一化为空切片（非 nil）")
	}
	if len(resp.Jobs) != 0 {
		t.Fatalf("应为空，实际 %d", len(resp.Jobs))
	}
}

// TestClientPollParsesJobs 任务字段名必须按协议解析出来（服务端按 job_id/media_id 发送）。
func TestClientPollParsesJobs(t *testing.T) {
	srv, _, _ := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jobs":[{"job_id":"j1","kind":"noop","media_id":"m1","profile":"1080p","input_path":"/x/y.jpg"}]}`))
	})
	resp, err := NewClient(srv.URL, "t").Poll(context.Background(), 5)
	if err != nil {
		t.Fatalf("Poll 出错: %v", err)
	}
	if len(resp.Jobs) != 1 {
		t.Fatalf("应解析出 1 个任务，实际 %d", len(resp.Jobs))
	}
	j := resp.Jobs[0]
	if j.JobID != "j1" || j.Kind != "noop" || j.MediaID != "m1" || j.Profile != "1080p" || j.InputPath != "/x/y.jpg" {
		t.Fatalf("字段解析不对: %+v", j)
	}
}

// TestNewClientTrimsInputs 服务端地址与令牌的空白处理。
func TestNewClientTrimsInputs(t *testing.T) {
	c := NewClient("  http://h:1///  ", "  tok  ")
	if c.baseURL != "http://h:1" {
		t.Fatalf("baseURL 未规范化: %q", c.baseURL)
	}
	if c.token != "tok" {
		t.Fatalf("token 未去空白: %q", c.token)
	}
}
