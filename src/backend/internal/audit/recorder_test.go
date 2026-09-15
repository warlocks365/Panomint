package audit

// Recorder 的「尽力而为」语义单测。
//
// 这是本包最重要的一组测试：验收标准里写死的「审计写入失败不得影响业务」
// 必须由**会返回 error 的假 store** 与**会 panic 的假 store** 双重断言，
// 而不是靠"看代码觉得没问题"。
//
// 断言方式说明：Record 没有返回值，所以"不返回错"体现为**不 panic**
// （panic 会被 recover 吞掉吗？—— 会，但我们用 t.Fatal 型的 recover 断言把它揪出来：
// 见 mustNotPanic）。任何未来把 Record 改成会 panic 的改动都会在这里失败。

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// mustNotPanic 执行 fn 并断言其不 panic。
func mustNotPanic(t *testing.T, what string, fn func()) {
	t.Helper()
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("%s 不得 panic（审计不能成为新的故障点），实际 panic: %v", what, rec)
		}
	}()
	fn()
}

func validEntry() Entry {
	return Entry{
		ActorUserID: "7c6b1b9c-cba2-4394-b982-67d9038421f3",
		Action:      ActionShareRevoke,
		TargetType:  TargetShare,
		TargetID:    "3c404f62-4ed4-4c7b-97c6-f7e2cf845c0e",
		IP:          "192.168.1.115",
		UserAgent:   "Mozilla/5.0",
		Detail:      map[string]any{"before": "active", "password": "must-not-land"},
	}
}

// TestRecordSwallowsStoreError 核心用例：store 返回 error 时 Record 必须静默通过。
func TestRecordSwallowsStoreError(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	fs := &fakeStore{insertErr: errBoom}
	r := NewWithStore(fs, zap.New(core))

	mustNotPanic(t, "Record（store 报错）", func() {
		r.Record(context.Background(), validEntry())
	})

	// 必须留下 warn 日志 —— 审计失败要留痕，只是不能外溢成业务失败。
	if logs.Len() != 1 {
		t.Fatalf("应恰好产生 1 条 warn 日志，实际 %d 条", logs.Len())
	}
	msg := logs.All()[0].Message
	if !strings.Contains(msg, "审计写入失败") {
		t.Fatalf("warn 日志文案不符：%q", msg)
	}
	if logs.All()[0].Level != zap.WarnLevel {
		t.Fatalf("日志级别应为 warn，实际 %v", logs.All()[0].Level)
	}
}

// TestRecordSwallowsStorePanic store 自身 panic 也不能穿透到业务（最后一道闸门）。
func TestRecordSwallowsStorePanic(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	r := NewWithStore(&fakeStore{insertPanics: true}, zap.New(core))

	mustNotPanic(t, "Record（store panic）", func() {
		r.Record(context.Background(), validEntry())
	})
	if logs.Len() != 1 || !strings.Contains(logs.All()[0].Message, "panic") {
		t.Fatalf("store panic 应被吞掉并记一条 warn，实际：%v", logs.All())
	}
}

// TestRecordOnNilRecorder nil 接收者不得 panic。防御的不是"我们会写错"，
// 而是"装配代码在某个分支忘了初始化 Recorder"这类真实事故。
func TestRecordOnNilReceiver(t *testing.T) {
	var r *Recorder
	mustNotPanic(t, "nil *Recorder.Record", func() {
		r.Record(context.Background(), validEntry())
	})
	mustNotPanic(t, "nil *Recorder.Log", func() {
		_ = r.Log(context.Background(), validEntry())
	})
}

// TestRecordWithoutStore 未装配 store 时同样静默（而非 panic 出 nil 解引用）。
func TestRecordWithoutStore(t *testing.T) {
	r := NewWithStore(nil, nil)
	mustNotPanic(t, "无 store 的 Record", func() {
		r.Record(context.Background(), validEntry())
	})
	// Log 是给测试/诊断用的，故它必须把错误暴露出来。
	if err := r.Log(context.Background(), validEntry()); err == nil {
		t.Fatal("未装配 store 时 Log 应返回错误（便于诊断装配遗漏）")
	}
}

// TestRecordRejectsInvalidEntry 非法记录被拒写、但不影响调用方。
//
// 取舍：宁可拒写也不落脏数据 —— 一条 action 非法的记录既过滤不到也解释不了，
// 留在表里只会在追责时误导人。
func TestRecordRejectsInvalidEntry(t *testing.T) {
	fs := &fakeStore{}
	r := NewWithStore(fs, zap.NewNop())

	bad := []Entry{
		{Action: ""},            // 空动作
		{Action: "admin/audit"}, // 非法字符
		{Action: ActionLogin, ActorUserID: "不是 UUID"}, // actor 非 UUID（会撞 users 外键）
		{Action: ActionShareCreate, TargetID: "x"},    // 有 id 没 type
	}
	for i, e := range bad {
		mustNotPanic(t, "Record(非法 Entry)", func() {
			r.Record(context.Background(), e)
		})
		if err := r.Log(context.Background(), e); err == nil {
			t.Fatalf("第 %d 条非法 Entry 应被 Log 拒绝: %+v", i, e)
		}
	}
	if got := len(fs.entries()); got != 0 {
		t.Fatalf("非法记录不应写入 store，实际写入 %d 条", got)
	}
}

// TestRecordNormalizesAndRedactsBeforeStore store 收到的必须是**归一化 + 脱敏后**的记录。
//
// 关键点：脱敏必须发生在**写入之前**且与 store 实现无关 ——
// 否则换一个 store 实现就可能把明文密码写进审计表。
func TestRecordNormalizesAndRedactsBeforeStore(t *testing.T) {
	fs := &fakeStore{}
	r := NewWithStore(fs, zap.NewNop())

	e := validEntry()
	e.Action = "  SHARE.REVOKE "
	e.ActorUserID = "7C6B1B9C-CBA2-4394-B982-67D9038421F3"
	e.UserAgent = "Mozilla/5.0\r\nX-Injected: 1"
	e.At = time.Time{}
	mustNotPanic(t, "Record(需归一化)", func() { r.Record(context.Background(), e) })

	got := fs.entries()
	if len(got) != 1 {
		t.Fatalf("应写入 1 条，实际 %d", len(got))
	}
	saved := got[0]
	if saved.Action != ActionShareRevoke {
		t.Fatalf("action 应归一小写：%q", saved.Action)
	}
	if saved.ActorUserID != "7c6b1b9c-cba2-4394-b982-67d9038421f3" {
		t.Fatalf("actor 应归一小写：%q", saved.ActorUserID)
	}
	if _, hit := saved.Detail["password"]; hit {
		t.Fatalf("脱敏必须发生在 store 之前，实际 detail=%+v", saved.Detail)
	}
	if saved.Detail["before"] != "active" {
		t.Fatalf("非敏感字段应保留：%+v", saved.Detail)
	}
	if strings.ContainsAny(saved.UserAgent, "\r\n") {
		t.Fatalf("UA 控制字符应已剔除：%q", saved.UserAgent)
	}
	if saved.At.IsZero() {
		t.Fatal("At 应被补为当前时间")
	}
	if _, err := NormalizeAction(saved.Action); err != nil {
		t.Fatalf("落库的 action 必须能通过写入侧校验（否则过滤永远查不到）：%v", err)
	}
}

// TestLogDecouplesFromRequestContext 请求上下文取消不得让审计静默丢失。
//
// 场景真实存在：管理端发起了一个较慢的查询，客户端提前断开 → 请求 ctx 取消。
// 审计若直接用请求 ctx，这条记录会以一个"context canceled"的 warn 消失 ——
// 而那恰恰是最该被记下来的请求之一。故 Log 内部用 WithoutCancel 解耦，
// 只保留 2s 上界（有上界才允许它"可以慢、可以丢"）。
func TestLogDecouplesFromRequestContext(t *testing.T) {
	fs := &fakeStore{}
	r := NewWithStore(fs, zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 模拟客户端已断开

	if err := r.Log(ctx, validEntry()); err != nil {
		t.Fatalf("请求 ctx 已取消时审计仍应写入，实际 %v", err)
	}
	if fs.lastInsertCtxErr != nil {
		t.Fatalf("store 收到的 ctx 不应已取消，实际 %v", fs.lastInsertCtxErr)
	}
}

// TestLogHasBoundedDeadline 写入必须带耗时上界：允许审计慢/丢，不允许把业务拖死。
//
// 这是「审计不得成为故障点」的时间维度 —— 只让 store 报错不拖业务还不够，
// 一个卡住的 store（如库不可达时的 TCP 重试）同样会把业务请求挂住。
func TestLogHasBoundedDeadline(t *testing.T) {
	fs := &fakeStore{}
	r := NewWithStore(fs, zap.NewNop())
	if err := r.Log(context.Background(), validEntry()); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if !fs.lastInsertHadDeadline {
		t.Fatalf("写入 ctx 必须带 deadline（上界 %v），否则 store 卡住会拖死业务请求", auditTimeout)
	}
}

// TestRecordBlocksNoLongerThanDeadline 端到端：store 永久阻塞时，Record 仍会在上界内返回。
//
// fakeStore.insertDelay 用 3× 上界模拟"库挂住"，Record 必须在上界附近（而非 3× 之后）
// 就返回并记 warn —— 证明上界真的生效，而不是只挂了个 deadline 没人用。
func TestRecordBlocksNoLongerThanDeadline(t *testing.T) {
	fs := &fakeStore{insertDelay: 3 * auditTimeout}
	r := NewWithStore(fs, zap.NewNop())

	start := time.Now()
	mustNotPanic(t, "Record（store 卡住）", func() {
		r.Record(context.Background(), validEntry())
	})
	elapsed := time.Since(start)
	if elapsed > 2*auditTimeout {
		t.Fatalf("Record 耗时 %v，超上界 %v 太多（上界未生效，业务会被拖住）", elapsed, auditTimeout)
	}
}

func TestNormalizeUserAgentRuneSafety(t *testing.T) {
	// 已在 audit_test.go 覆盖截断语义，这里补一条：map 复用时不得被脱敏改写。
	in := map[string]any{"password": "x", "keep": "y"}
	out := RedactDetail(in)
	if _, still := in["password"]; !still {
		t.Fatal("脱敏必须返回副本，不得就地修改调用方的 map")
	}
	if !utf8.ValidString("ok") || out["keep"] != "y" {
		t.Fatal("非敏感字段应原样保留")
	}
}
