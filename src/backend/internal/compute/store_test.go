package compute

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// 本文件是**免连库的 SQL 契约测试**：Store 的每条语句都提成了包级常量，
// 于是"语句里有没有出现明文令牌列""认领任务时有没有抢 hls""回传有没有归属守卫"
// 这类回归点可以在 CI 里以毫秒级成本守住。
//
// 这类断言刻意只检查**关键字存在性**而非完整文本：完整文本一改就碎，
// 反而会让人习惯性去改测试而不是想清楚改动是否安全。

// TestInsertSQLMustNotStorePlaintextToken 登记语句绝不能写明文令牌列。
//
// 这是本项目「只存哈希不存明文」的最硬一道防线：一旦有人为了"方便取回令牌"
// 把 agent_token 写回去，这个测试立即失败。
func TestInsertSQLMustNotStorePlaintextToken(t *testing.T) {
	if !strings.Contains(insertNodeSQL, "agent_token_hash") {
		t.Fatalf("登记语句必须写 agent_token_hash：\n%s", insertNodeSQL)
	}
	if strings.Contains(insertNodeSQL, "agent_token_expires_at") == false {
		t.Fatalf("登记语句必须能写 agent_token_expires_at：\n%s", insertNodeSQL)
	}
	// 精确判定：不得出现"独立"的 agent_token 列（agent_token_hash/agent_token_expires_at 不算）。
	if columnUsed(insertNodeSQL, "agent_token") {
		t.Fatalf("登记语句不得写明文 agent_token 列：\n%s", insertNodeSQL)
	}
}

// TestAllNodeSQLMustNotStorePlaintextToken 全量扫描：任何语句都不得读明文令牌列。
func TestAllNodeSQLMustNotStorePlaintextToken(t *testing.T) {
	stmts := map[string]string{
		"listNodesSQL":             listNodesSQL,
		"getNodeSQL":               getNodeSQL,
		"insertNodeSQL":            insertNodeSQL,
		"deleteNodeSQL":            deleteNodeSQL,
		"findNodeByTokenHashSQL":   findNodeByTokenHashSQL,
		"heartbeatSQL":             heartbeatSQL,
		"pollJobsSQL":              pollJobsSQL,
		"pollInputPathsSQL":        pollInputPathsSQL,
		"submitResultSQL":          submitResultSQL,
	}
	for name, sql := range stmts {
		// 允许出现在 COMMENT/参数里的是 hash 与 expires 两个派生列，明文列名一律禁止。
		if columnUsed(sql, "agent_token") {
			t.Fatalf("%s 出现了明文 agent_token 列（只允许 agent_token_hash / agent_token_expires_at）：\n%s", name, sql)
		}
	}
}

// columnUsed 判断 SQL 中是否把 name 当作独立列名使用（排除 name_suffix 形式的前缀碰撞）。
func columnUsed(sql, name string) bool {
	for i := 0; ; {
		idx := strings.Index(sql[i:], name)
		if idx < 0 {
			return false
		}
		at := i + idx
		end := at + len(name)
		// 后面紧跟下划线或字母数字 → 说明是 agent_token_hash 这类派生列，放行。
		if end < len(sql) {
			c := sql[end]
			if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
				i = end
				continue
			}
		}
		return true
	}
}

// TestColumnUsedHelper 自检辅助函数本身（否则整套明文断言可能静默失效）。
func TestColumnUsedHelper(t *testing.T) {
	if columnUsed("SELECT agent_token_hash FROM t", "agent_token") {
		t.Fatal("agent_token_hash 不应被判为明文列")
	}
	if columnUsed("SELECT agent_token_expires_at FROM t", "agent_token") {
		t.Fatal("agent_token_expires_at 不应被判为明文列")
	}
	if !columnUsed("INSERT INTO compute_nodes (agent_token) VALUES ($1)", "agent_token") {
		t.Fatal("独立列的 agent_token 必须被判出")
	}
	if !columnUsed("SET agent_token = NULL", "agent_token") {
		t.Fatal("SET agent_token = 必须被判出")
	}
}

// TestListSQLUsesRealColumnNames 列名必须是线上实际结构（last_heartbeat，不是 last_heartbeat_at）。
func TestListSQLUsesRealColumnNames(t *testing.T) {
	for name, sql := range map[string]string{"listNodesSQL": listNodesSQL, "getNodeSQL": getNodeSQL, "heartbeatSQL": heartbeatSQL} {
		if !strings.Contains(sql, "last_heartbeat") {
			t.Fatalf("%s 必须读 last_heartbeat：\n%s", name, sql)
		}
		if strings.Contains(sql, "last_heartbeat_at") {
			t.Fatalf("%s 使用了不存在的列 last_heartbeat_at：\n%s", name, sql)
		}
	}
	if !strings.Contains(listNodesSQL, "id::text") {
		t.Fatalf("UUID 必须 ::text 扫进 string 以避免 pgx uuid 类型依赖：\n%s", listNodesSQL)
	}
}

// TestHeartbeatSQLKeepsUnreportedFields COALESCE 保证"未上报的字段不覆盖"。
//
// 断言前先把空白压掉：SQL 里的列名对齐空格会随字段名长度变化，
// 对它做字面匹配的测试只会在改排版时碎掉，反而掩盖了真正的语义。
func TestHeartbeatSQLKeepsUnreportedFields(t *testing.T) {
	compact := strings.Join(strings.Fields(heartbeatSQL), "")
	for _, col := range []string{"status", "codecs", "has_nvenc", "vram_mb", "concurrency"} {
		if !strings.Contains(compact, col+"=COALESCE(") {
			t.Fatalf("心跳语句缺少 %s 的 COALESCE 保护：\n%s", col, heartbeatSQL)
		}
	}
	if !strings.Contains(compact, "last_heartbeat=now()") {
		t.Fatalf("心跳语句必须更新 last_heartbeat（服务端时钟为准）：\n%s", heartbeatSQL)
	}
}

// TestPollJobsSQLIsAtomicAndScoped 认领必须原子、必须锁定跳过、且**不得抢 hls 任务**。
func TestPollJobsSQLIsAtomicAndScoped(t *testing.T) {
	for _, want := range []string{"FOR UPDATE SKIP LOCKED", "status = 'pending'", "kind = ANY(", "LIMIT $2"} {
		if !strings.Contains(pollJobsSQL, want) {
			t.Fatalf("认领语句缺少 %q：\n%s", want, pollJobsSQL)
		}
	}
	if !strings.Contains(pollJobsSQL, "node_id = $1::uuid") {
		t.Fatalf("认领语句必须把 node_id 落到本节点（这是 node_id 从恒 NULL 变为真实的唯一入口）：\n%s", pollJobsSQL)
	}
}

// TestDefaultClaimableKindsExcludesHLS 默认认领集合只含 noop。
//
// 若把 hls 放进来，节点会与在跑的 cmd/transcodectl worker 争抢同一批 pending 行，
// 直接破坏现网转码流程。这条断言是刻意的"防越界"。
func TestDefaultClaimableKindsExcludesHLS(t *testing.T) {
	if len(DefaultClaimableKinds) != 1 || DefaultClaimableKinds[0] != "noop" {
		t.Fatalf("默认认领集合应仅含 noop，实际 %v", DefaultClaimableKinds)
	}
	for _, k := range DefaultClaimableKinds {
		if k == "hls" || k == "thumbnail" || k == "memories" {
			t.Fatalf("不得把 %q 加入默认认领集合（会与既有 worker 抢任务）", k)
		}
	}
}

// TestSubmitResultSQLIsOwnershipGuarded 回传必须带归属守卫，防止节点篡改他人任务。
func TestSubmitResultSQLIsOwnershipGuarded(t *testing.T) {
	if !strings.Contains(submitResultSQL, "node_id IS NULL OR node_id = $2::uuid") {
		t.Fatalf("回传语句缺少 node_id 归属守卫：\n%s", submitResultSQL)
	}
	if !strings.Contains(submitResultSQL, "status = $3") {
		t.Fatalf("回传语句必须更新 status：\n%s", submitResultSQL)
	}
	if !strings.Contains(submitResultSQL, "result_path = NULLIF($4,'')") {
		t.Fatalf("回传语句应把空串 result_path 归一为 NULL：\n%s", submitResultSQL)
	}
}

// TestFindByTokenHashSQLUsesIndex 令牌查找必须按哈希列（走 00018 的索引）。
func TestFindByTokenHashSQLUsesIndex(t *testing.T) {
	if !strings.Contains(findNodeByTokenHashSQL, "agent_token_hash = $1") {
		t.Fatalf("令牌查找必须按 agent_token_hash 定位：\n%s", findNodeByTokenHashSQL)
	}
	if !strings.Contains(findNodeByTokenHashSQL, "agent_token_expires_at") {
		t.Fatalf("令牌查找必须取回过期时刻供 TokenValid 使用：\n%s", findNodeByTokenHashSQL)
	}
}

// TestApplyEffectiveStatus 查询侧的生效状态填充（List 用的就是它）。
func TestApplyEffectiveStatus(t *testing.T) {
	now := time.Now()
	fresh := now.Add(-time.Second)
	stale := now.Add(-DefaultOfflineAfter - time.Second)

	nodes := []Node{
		{Name: "a", Status: StatusOnline, LastHeartbeat: &fresh},
		{Name: "b", Status: StatusBusy, LastHeartbeat: &stale},
		{Name: "c", Status: StatusOnline, LastHeartbeat: nil},
		{Name: "d", Status: StatusOffline, LastHeartbeat: &fresh},
	}
	applyEffectiveStatus(nodes, DefaultOfflineAfter, now)

	want := []Status{StatusOnline, StatusOffline, StatusOffline, StatusOffline}
	for i := range nodes {
		if nodes[i].EffectiveStatus != want[i] {
			t.Fatalf("nodes[%d] (%s) EffectiveStatus = %q，期望 %q", i, nodes[i].Name, nodes[i].EffectiveStatus, want[i])
		}
	}
}

// TestBuildNodeUpdate 覆盖 Store.Patch 里真正使用的拼装函数（不是复刻品）。
//
// 关注点：占位符必须从 $1 连续编号（pgx 靠个数匹配参数，错位会静默写错列）、
// args 与 sets 一一对应、未提供的字段绝不出现在 SET 里、轮换时必写 hash 且不写明文列。
func TestBuildNodeUpdate(t *testing.T) {
	t.Run("仅 status：一条 SET、一个参数、不轮换", func(t *testing.T) {
		sets, args, plain, err := buildNodeUpdate(PatchInput{Status: ptrStatus(StatusOffline)})
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if len(sets) != 1 || len(args) != 1 {
			t.Fatalf("应恰好 1 条 SET / 1 个参数，实际 sets=%v args=%v", sets, args)
		}
		if sets[0] != "status = $1" {
			t.Fatalf("SET 子句应为 status = $1，实际 %q", sets[0])
		}
		if plain != "" {
			t.Fatal("未请求轮换时不应返回明文令牌")
		}
	})

	t.Run("未提供的字段绝不进 SET", func(t *testing.T) {
		sets, _, _, _ := buildNodeUpdate(PatchInput{Codecs: ptrStr("h264,hevc")})
		joined := strings.Join(sets, ", ")
		for _, bad := range []string{"status =", "has_nvenc =", "vram_mb =", "concurrency =", "name =", "host ="} {
			if strings.Contains(joined, bad) {
				t.Fatalf("未提供的字段 %q 不该出现在 SET 里：%s", bad, joined)
			}
		}
	})

	t.Run("多字段：占位符连续且 args 对齐", func(t *testing.T) {
		sets, args, _, err := buildNodeUpdate(PatchInput{
			Name: ptrStr("n"), Host: ptrStr("h"), Status: ptrStatus(StatusBusy),
			Codecs: ptrStr("h264"), HasNVENC: ptrBool(true), VRAMMB: ptrInt(24576), Concurrency: ptrInt(2),
		})
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if len(sets) != 7 || len(args) != 7 {
			t.Fatalf("应有 7 条 SET / 7 个参数，实际 sets=%d args=%d", len(sets), len(args))
		}
		for i := 0; i < 7; i++ {
			want := "$" + strconv.Itoa(i+1)
			if !strings.Contains(sets[i], want) {
				t.Fatalf("第 %d 条 SET 应使用占位符 %s，实际 %q", i, want, sets[i])
			}
		}
		// host 的空串语义（清空）必须保留 NULLIF 包装
		if !strings.Contains(strings.Join(sets, ", "), "host = NULLIF($2, '')") {
			t.Fatalf("host 应写为 NULLIF($2,'')：%v", sets)
		}
	})

	t.Run("轮换：写 hash 与 expires，明文非空，且不写明文列", func(t *testing.T) {
		sets, args, plain, err := buildNodeUpdate(PatchInput{RotateToken: true})
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		joined := strings.Join(sets, ", ")
		if plain == "" {
			t.Fatal("轮换必须返回新明文令牌（只此一次）")
		}
		if !strings.Contains(joined, "agent_token_hash = $1") {
			t.Fatalf("轮换必须写 agent_token_hash：%s", joined)
		}
		if !strings.Contains(joined, "agent_token_expires_at = $2") {
			t.Fatalf("轮换必须重置过期时刻：%s", joined)
		}
		if columnUsed(joined, "agent_token") {
			t.Fatalf("轮换语句不得写明文 agent_token 列：%s", joined)
		}
		// $2 的实参必须是 nil（NULL = 永不过期）
		if len(args) != 2 || args[1] != nil {
			t.Fatalf("expires 实参应为 nil，实际 args=%v", args)
		}
		if args[0] != HashAgentToken(plain) {
			t.Fatalf("写入的应是明文的 sha256 哈希，实际 %v", args[0])
		}
	})

	t.Run("轮换 + 其它字段：占位符不冲突", func(t *testing.T) {
		sets, args, plain, err := buildNodeUpdate(PatchInput{Status: ptrStatus(StatusOnline), RotateToken: true})
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if len(sets) != 3 || len(args) != 3 {
			t.Fatalf("应有 3 条 SET / 3 个参数，实际 sets=%v args=%v", sets, args)
		}
		if plain == "" {
			t.Fatal("应返回明文令牌")
		}
		if sets[2] != "agent_token_expires_at = $3" {
			t.Fatalf("最后一条应为 agent_token_expires_at = $3，实际 %q", sets[2])
		}
	})
}

// TestNodeColsHasNoPlaintextToken 读取列集合本身也必须干净。
func TestNodeColsHasNoPlaintextToken(t *testing.T) {
	if columnUsed(nodeCols, "agent_token") {
		t.Fatalf("nodeCols 不得包含明文令牌列：%s", nodeCols)
	}
}
