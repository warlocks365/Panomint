package compute

import (
	"errors"
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
		"listNodesSQL":           listNodesSQL,
		"getNodeSQL":             getNodeSQL,
		"insertNodeSQL":          insertNodeSQL,
		"deleteNodeSQL":          deleteNodeSQL,
		"findNodeByTokenHashSQL": findNodeByTokenHashSQL,
		"heartbeatSQL":           heartbeatSQL,
		"pollJobsSQL":            pollJobsSQL,
		"pollInputPathsSQL":      pollInputPathsSQL,
		"submitResultSQL":        submitResultSQL,
		"probeJobStatusSQL":      probeJobStatusSQL,
		"reclaimJobsSQL":         reclaimJobsSQL,
		"publishHLSMasterSQL":    publishHLSMasterSQL,
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

// TestHeartbeatSQLKeepsUnreportedFields 能力字段仍由 COALESCE 保证"未上报的不覆盖"。
//
// ⚠️ 本测试在 T6.2 缺陷修复中改过：status 从列表里移除了。
// 原因：status 原来也是 `COALESCE($2, status)`，但那正是缺陷——新登记节点落库 status 为
// DDL 默认 'offline'，COALESCE 保留它 → 心跳永远无法把节点变成 online。
// 改为 `CASE WHEN status_locked ... END` 后不再使用该形式，其行为由
// TestHeartbeatSQLSetsStatusWhenUnlocked 专门覆盖。
func TestHeartbeatSQLKeepsUnreportedFields(t *testing.T) {
	compact := strings.Join(strings.Fields(heartbeatSQL), "")
	for _, col := range []string{"codecs", "has_nvenc", "vram_mb", "concurrency"} {
		if !strings.Contains(compact, col+"=COALESCE(") {
			t.Fatalf("心跳语句缺少 %s 的 COALESCE 保护：\n%s", col, heartbeatSQL)
		}
	}
	if !strings.Contains(compact, "last_heartbeat=now()") {
		t.Fatalf("心跳语句必须更新 last_heartbeat（服务端时钟为准）：\n%s", heartbeatSQL)
	}
}

// TestHeartbeatSQLSetsStatusWhenUnlocked 心跳必须能把未加锁的节点置为 online。
//
// 这是 T6.2 缺陷的**直接回归断言**（缺陷现象：登记后心跳成功，节点仍 forever offline）：
//   - 未加锁时用节点自报状态，缺省 'online' —— 否则新登记节点（落库默认 offline）永不上线；
//   - 已加锁时保持原值 —— 否则"管理员强制下线"会被心跳推翻，等于没有强制下线能力。
//
// 保留 COALESCE($2, ...) 是为了**不把节点自报的 busy 抹成 online**：
// cmd/nodeagent 在 active_tasks>0 时上报 busy，无条件写 'online' 会让 busy 永远不可见。
func TestHeartbeatSQLSetsStatusWhenUnlocked(t *testing.T) {
	compact := strings.Join(strings.Fields(heartbeatSQL), "")

	if !strings.Contains(compact, "status=CASEWHENstatus_lockedTHENstatusELSE") {
		t.Fatalf("心跳语句必须按 status_locked 分支改写 status（未加锁才写自报状态）：\n%s", heartbeatSQL)
	}
	if !strings.Contains(compact, "'online'") {
		t.Fatalf("心跳语句未加锁分支缺省值必须是 'online'（这是新节点能上线的原因）：\n%s", heartbeatSQL)
	}
	if !strings.Contains(compact, "COALESCE($2,'online')") {
		t.Fatalf("未加锁分支应保留节点自报的 busy（COALESCE($2,'online')），不得无条件写 'online'：\n%s", heartbeatSQL)
	}
	if !strings.Contains(compact, "status_locked") {
		t.Fatalf("心跳语句必须读 status_locked：\n%s", heartbeatSQL)
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

// TestSubmitResultSQLIsOwnershipGuarded 回传必须带归属守卫与终态守卫，防止节点篡改他人任务
// 或改写已结束的任务。
//
// ⚠️ 本测试在「离线超时任务重派」交付项中改过（原来是单条 `node_id IS NULL OR node_id = $2`）：
// 回收机制上线后，"node_id IS NULL" 不再只出现在"任务刚被创建"这一刻——僵死节点的任务
// 会被放回 pending 且清空归属。于是旧写法把两种完全不同的情形混成一条：既接受
// 「原节点完成得很晚」（应接受，避免白跑一遍），也接受「任务已被别的节点领走」
// （不该接受，会互相覆盖产出）。新写法把后者排除掉，并补上终态守卫。
func TestSubmitResultSQLIsOwnershipGuarded(t *testing.T) {
	compact := strings.Join(strings.Fields(submitResultSQL), "")
	if !strings.Contains(compact, "(node_id=$2::uuidOR(node_idISNULLANDstatus='pending'))") {
		t.Fatalf("回传语句缺少两分支归属守卫（本节点持有 ‖ 已回收且无人持有）：\n%s", submitResultSQL)
	}
	if !strings.Contains(compact, "statusNOTIN('done','failed')") {
		t.Fatalf("回传语句缺少终态守卫（终态不可改写）：\n%s", submitResultSQL)
	}
	if !strings.Contains(submitResultSQL, "status = $3") {
		t.Fatalf("回传语句必须更新 status：\n%s", submitResultSQL)
	}
	if !strings.Contains(submitResultSQL, "result_path = NULLIF($4,'')") {
		t.Fatalf("回传语句应把空串 result_path 归一为 NULL：\n%s", submitResultSQL)
	}
}

// TestProbeJobStatusSQLIsASingleLookup 反查语句必须是按主键的单次查询（失败路径专用）。
func TestProbeJobStatusSQLIsASingleLookup(t *testing.T) {
	if !strings.Contains(probeJobStatusSQL, "WHERE id = $1::uuid") {
		t.Fatalf("反查语句必须按主键定位：\n%s", probeJobStatusSQL)
	}
	if !strings.Contains(probeJobStatusSQL, "status") {
		t.Fatalf("反查语句必须取回 status：\n%s", probeJobStatusSQL)
	}
}

// TestReclaimJobsSQLOnlyTouchesStaleRunning 回收语句的边界必须精确。
//
// 这是最危险的一条语句：它会把任务从别的节点手里拿走。写宽了（例如漏掉 status='running'
// 或漏掉归属非空）就会误伤正常在跑的任务，且这种 bug 只在多节点并发时才显形。
func TestReclaimJobsSQLOnlyTouchesStaleRunning(t *testing.T) {
	compact := strings.Join(strings.Fields(reclaimJobsSQL), "")

	// 只回收 running：pending 无人持有，done/failed 是终态（审计意义上的历史）。
	if !strings.Contains(compact, "j.status='running'") {
		t.Fatalf("回收语句必须限定 status='running'：\n%s", reclaimJobsSQL)
	}
	if !strings.Contains(compact, "j.node_idISNOTNULL") {
		t.Fatalf("回收语句必须要求归属非空（node_id IS NULL 的行本就无人持有）：\n%s", reclaimJobsSQL)
	}
	// 归属清空 + 状态置回 pending，才能被 pollJobsSQL 重新认领。
	if !strings.Contains(compact, "SETstatus='pending',node_id=NULL") {
		t.Fatalf("回收语句必须置 pending 并清空 node_id：\n%s", reclaimJobsSQL)
	}
	// 判据来源必须是 compute_nodes 的心跳（或管理员强制下线），而不是"任务跑了多久"。
	if !strings.Contains(compact, "FROMcompute_nodes") {
		t.Fatalf("回收判据必须取自 compute_nodes：\n%s", reclaimJobsSQL)
	}
	if !strings.Contains(compact, "status_lockedANDstatus='offline'") {
		t.Fatalf("回收语句必须识别「管理员强制下线」（status_locked + offline）：\n%s", reclaimJobsSQL)
	}
	if !strings.Contains(compact, "COALESCE(last_heartbeat,created_at)") {
		t.Fatalf("回收语句须用 COALESCE(last_heartbeat, created_at) 兜底，给从未心跳的新节点宽限期：\n%s", reclaimJobsSQL)
	}
	if !strings.Contains(compact, "$1::int*interval'1second'") {
		t.Fatalf("回收阈值必须是可注入的参数（不得写死秒数）：\n%s", reclaimJobsSQL)
	}
	// 绝不把终态行也捞进来。
	for _, bad := range []string{"'pending'OR", "statusIN('done'", "status='done'"} {
		if strings.Contains(compact, bad) {
			t.Fatalf("回收语句不该出现 %q（会误伤终态行）：\n%s", bad, reclaimJobsSQL)
		}
	}
}

// TestReclaimAfter 回收阈值由离线阈值推出（2×），且对非正值有安全的兜底。
//
// 「回收阈值 = 2 × 离线阈值」这一比例是刻意的：回收比"判离线"更保守，
// 因为误回收的代价（两个节点跑同一条媒体、产出互相覆盖）远高于晚回收。
func TestReclaimAfter(t *testing.T) {
	if DefaultReclaimAfter != 2*DefaultOfflineAfter {
		t.Fatalf("DefaultReclaimAfter 应为 2×DefaultOfflineAfter，实际 %s vs %s",
			DefaultReclaimAfter, DefaultOfflineAfter)
	}
	if got := ReclaimAfter(0); got != DefaultReclaimAfter {
		t.Fatalf("offlineAfter<=0 应回落默认值 %s，实际 %s", DefaultReclaimAfter, got)
	}
	if got := ReclaimAfter(-time.Second); got != DefaultReclaimAfter {
		t.Fatalf("负值应回落默认值 %s，实际 %s", DefaultReclaimAfter, got)
	}
	if got, want := ReclaimAfter(30*time.Second), 60*time.Second; got != want {
		t.Fatalf("应随离线阈值缩放：ReclaimAfter(30s) = %s，期望 %s", got, want)
	}
	// 阈值必须是正的，否则 reclaimJobsSQL 的 `now() - 0s` 会回收**所有** running 任务。
	if DefaultReclaimAfter <= 0 {
		t.Fatal("回收阈值必须为正")
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
//
// ⚠️ 本测试在 T6.2 缺陷修复中改过：原来 nodes[3] 是「未加锁的 offline + 新鲜心跳 → offline」，
// 那正是缺陷本身（新登记节点落库即 offline，永不上线）。改法：把它标为**加锁**的 offline
// （仍期望 offline），并新增一条未加锁的同形数据期望 online —— 两者并存才能证明
// 「区分初始 offline 与强制 offline」这件事真的做到了。
func TestApplyEffectiveStatus(t *testing.T) {
	now := time.Now()
	fresh := now.Add(-time.Second)
	stale := now.Add(-DefaultOfflineAfter - time.Second)

	nodes := []Node{
		{Name: "a", Status: StatusOnline, LastHeartbeat: &fresh},
		{Name: "b", Status: StatusBusy, LastHeartbeat: &stale},
		{Name: "c", Status: StatusOnline, LastHeartbeat: nil},
		{Name: "d", Status: StatusOffline, StatusLocked: true, LastHeartbeat: &fresh}, // 管理员强制下线
		{Name: "e", Status: StatusOffline, LastHeartbeat: &fresh},                     // 初始 offline，心跳新鲜
	}
	applyEffectiveStatus(nodes, DefaultOfflineAfter, now)

	want := []Status{StatusOnline, StatusOffline, StatusOffline, StatusOffline, StatusOnline}
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
	t.Run("仅 status：两条 SET（status + status_locked）、两个参数、不轮换", func(t *testing.T) {
		// ⚠️ 本条在 T6.2 修复中由「1 条 SET / 1 个参数」改为 2 条：
		// 显式带 status 现在同时写状态锁（offline → status_locked=true），见 buildNodeUpdate。
		sets, args, plain, err := buildNodeUpdate(PatchInput{Status: ptrStatus(StatusOffline)})
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if len(sets) != 2 || len(args) != 2 {
			t.Fatalf("应恰好 2 条 SET / 2 个参数，实际 sets=%v args=%v", sets, args)
		}
		if sets[0] != "status = $1" {
			t.Fatalf("SET 子句应为 status = $1，实际 %q", sets[0])
		}
		if sets[1] != "status_locked = $2" {
			t.Fatalf("SET 子句应为 status_locked = $2，实际 %q", sets[1])
		}
		if plain != "" {
			t.Fatal("未请求轮换时不应返回明文令牌")
		}
	})

	t.Run("未提供的字段绝不进 SET", func(t *testing.T) {
		sets, _, _, _ := buildNodeUpdate(PatchInput{Codecs: ptrStr("h264,hevc")})
		joined := strings.Join(sets, ", ")
		for _, bad := range []string{"status =", "status_locked", "has_nvenc =", "vram_mb =", "concurrency =", "name =", "host ="} {
			if strings.Contains(joined, bad) {
				t.Fatalf("未提供的字段 %q 不该出现在 SET 里：%s", bad, joined)
			}
		}
	})

	t.Run("多字段：占位符连续且 args 对齐", func(t *testing.T) {
		// ⚠️ 本条在 T6.2 修复中由 7 条 SET 改为 8 条：显式 status 额外带一条 status_locked。
		// name=$1 host=$2 status=$3 status_locked=$4 codecs=$5 has_nvenc=$6 vram_mb=$7 concurrency=$8
		sets, args, _, err := buildNodeUpdate(PatchInput{
			Name: ptrStr("n"), Host: ptrStr("h"), Status: ptrStatus(StatusBusy),
			Codecs: ptrStr("h264"), HasNVENC: ptrBool(true), VRAMMB: ptrInt(24576), Concurrency: ptrInt(2),
		})
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if len(sets) != 8 || len(args) != 8 {
			t.Fatalf("应有 8 条 SET / 8 个参数，实际 sets=%d args=%d", len(sets), len(args))
		}
		for i := 0; i < 8; i++ {
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
		// ⚠️ 本条在 T6.2 修复中由 3 条 SET 改为 4 条：显式 status 额外带一条 status_locked。
		// status=$1 status_locked=$2 agent_token_hash=$3 agent_token_expires_at=$4
		sets, args, plain, err := buildNodeUpdate(PatchInput{Status: ptrStatus(StatusOnline), RotateToken: true})
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if len(sets) != 4 || len(args) != 4 {
			t.Fatalf("应有 4 条 SET / 4 个参数，实际 sets=%v args=%v", sets, args)
		}
		if plain == "" {
			t.Fatal("应返回明文令牌")
		}
		if sets[3] != "agent_token_expires_at = $4" {
			t.Fatalf("最后一条应为 agent_token_expires_at = $4，实际 %q", sets[3])
		}
	})
}

// TestBuildNodeUpdateStatusLock 显式改状态时的「状态锁」写入行为。
//
// 这是本次缺陷修复里最容易写错的一处（占位符与 args 下标错位会静默写错列），
// 所以直接测生产代码用的 buildNodeUpdate 纯函数，而不是复刻一份。
//
// 语义（见迁移 00019 与 offline.go）：
//   - offline / busy = 管理员的强制意图 → status_locked = true（心跳不得改写）；
//   - online         = 解除锁定，交还给心跳自治 → status_locked = false；
//   - 未带 status     = 与状态无关的更新（改名/改 codecs）→ 绝不动 status_locked。
func TestBuildNodeUpdateStatusLock(t *testing.T) {
	cases := []struct {
		name       string
		in         PatchInput
		wantLocked bool
	}{
		{"显式 offline → 加锁", PatchInput{Status: ptrStatus(StatusOffline)}, true},
		{"显式 busy → 加锁（busy 也是管理员的显式意图）", PatchInput{Status: ptrStatus(StatusBusy)}, true},
		{"显式 online → 解锁（交还心跳自治）", PatchInput{Status: ptrStatus(StatusOnline)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sets, args, _, err := buildNodeUpdate(tc.in)
			if err != nil {
				t.Fatalf("不应报错: %v", err)
			}
			if len(sets) != 2 || len(args) != 2 {
				t.Fatalf("应恰好 2 条 SET / 2 个参数（status + status_locked），实际 sets=%v args=%v", sets, args)
			}
			if sets[1] != "status_locked = $2" {
				t.Fatalf("第二条 SET 应为 status_locked = $2，实际 %q", sets[1])
			}
			if args[1] != tc.wantLocked {
				t.Fatalf("status_locked 实参应为 %v，实际 %v", tc.wantLocked, args[1])
			}
		})
	}

	t.Run("未带 status：绝不触碰 status_locked", func(t *testing.T) {
		sets, args, _, err := buildNodeUpdate(PatchInput{Codecs: ptrStr("h264,hevc")})
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if len(sets) != 1 || len(args) != 1 {
			t.Fatalf("应恰好 1 条 SET / 1 个参数，实际 sets=%v args=%v", sets, args)
		}
		if strings.Contains(strings.Join(sets, ", "), "status_locked") {
			t.Fatalf("只改 codecs 时不得写 status_locked（否则会静默改变节点的锁状态）：%v", sets)
		}
	})
}

// TestNodeColsHasNoPlaintextToken 读取列集合本身也必须干净。
func TestNodeColsHasNoPlaintextToken(t *testing.T) {
	if columnUsed(nodeCols, "agent_token") {
		t.Fatalf("nodeCols 不得包含明文令牌列：%s", nodeCols)
	}
	// T6.2：状态锁必须被读出来（否则 effectiveStatus 无从区分初始 offline 与强制 offline）。
	if !strings.Contains(nodeCols, "status_locked") {
		t.Fatalf("nodeCols 必须包含 status_locked（否则 Node.StatusLocked 恒为 false，强制下线会被心跳推翻）：%s", nodeCols)
	}
}

// TestStoreClaimableKindsDefaultsToNoop 未配置时，认领集合必须仍是"仅 noop"。
//
// 这是把 hls 交给节点执行的**唯一开关**，且必须是显式的（COMPUTE_CLAIMABLE_KINDS）。
// 零值 Store（管理端/测试随手构造的那种）绝不能意外放开 hls ——
// 那会让节点与 cmd/transcodectl 同时执行同一条 job_id。
func TestStoreClaimableKindsDefaultsToNoop(t *testing.T) {
	var s Store
	if got := s.claimableKinds(); len(got) != 1 || got[0] != KindNoop {
		t.Fatalf("零值 Store 的认领集合应为 [noop]，实际 %v", got)
	}
	s.ClaimableKinds = []string{KindNoop, KindHLS}
	if got := s.claimableKinds(); len(got) != 2 || got[1] != KindHLS {
		t.Fatalf("显式配置应被采纳，实际 %v", got)
	}
}

// TestParseClaimableKinds 解析 -kinds / COMPUTE_CLAIMABLE_KINDS 的取值。
//
// 重点是"拼错的类型名必须报错"：若静默接受，节点会得到一个空的认领集合，
// 表现是"永远领不到任务"，而日志里看不出任何异常 —— 这类沉默失败最难查。
func TestParseClaimableKinds(t *testing.T) {
	t.Run("空串 → nil（表示用默认值）", func(t *testing.T) {
		for _, in := range []string{"", "   ", ",", " , "} {
			got, err := ParseClaimableKinds(in)
			if err != nil || got != nil {
				t.Fatalf("输入 %q 应返回 (nil, nil)，实际 (%v, %v)", in, got, err)
			}
		}
	})

	t.Run("正常解析：大小写归一、去空白、去重、保持顺序", func(t *testing.T) {
		got, err := ParseClaimableKinds(" HLS , noop ,hls ")
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		want := []string{"hls", "noop"}
		if len(got) != len(want) {
			t.Fatalf("应去重后得到 %v，实际 %v", want, got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("第 %d 项应为 %q，实际 %q（全部：%v）", i, want[i], got[i], got)
			}
		}
	})

	t.Run("未知类型 → ErrInvalidInput（不得静默忽略）", func(t *testing.T) {
		for _, in := range []string{"nope", "noop,thumnail", "hls;"} {
			if _, err := ParseClaimableKinds(in); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("输入 %q 应报 ErrInvalidInput，实际 %v", in, err)
			}
		}
	})

	t.Run("KnownKinds 必须覆盖执行器支持的全部类型", func(t *testing.T) {
		for _, k := range []string{KindNoop, KindHLS} {
			found := false
			for _, known := range KnownKinds {
				if known == k {
					found = true
				}
			}
			if !found {
				t.Fatalf("KnownKinds 缺少执行器支持的 %q：%v", k, KnownKinds)
			}
		}
	})
}

// TestPublishHLSMasterSQLIsKindGuarded 回写 media.hls_master 的语句必须自带 kind 守卫。
//
// 若不守 kind，一个非 hls 任务（将来的 thumbnail/memories）回传 result_path 时
// 会污染 hls_master，让前端把一张图当视频播 —— 而且只在"那种任务真的跑起来"时才暴露。
func TestPublishHLSMasterSQLIsKindGuarded(t *testing.T) {
	compact := strings.Join(strings.Fields(publishHLSMasterSQL), "")
	if !strings.Contains(compact, "j.kind='hls'") {
		t.Fatalf("回写语句必须限定 kind='hls'：\n%s", publishHLSMasterSQL)
	}
	if !strings.Contains(compact, "SEThls_master=$2") {
		t.Fatalf("回写语句必须写 media.hls_master：\n%s", publishHLSMasterSQL)
	}
	if !strings.Contains(compact, "media.id=j.media_id") {
		t.Fatalf("回写语句必须按 job 关联到对应媒体：\n%s", publishHLSMasterSQL)
	}
	if !strings.Contains(compact, "media.deleted_atISNULL") {
		t.Fatalf("回写语句必须跳过已软删媒体：\n%s", publishHLSMasterSQL)
	}
	if !strings.Contains(publishHLSMasterSQL, "WHERE j.id = $1::uuid") {
		t.Fatalf("回写语句必须按 job id 定位：\n%s", publishHLSMasterSQL)
	}
}
