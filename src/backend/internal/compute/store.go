package compute

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// 算力节点与转码任务的 DB 存取。
//
// 依赖约定（列名以**线上实际结构**为准，已核对 information_schema，勿凭 DDL 文档猜测）：
//
//	compute_nodes.id                       UUID PK
//	compute_nodes.name / kind / host       VARCHAR，host 可空
//	compute_nodes.agent_token              VARCHAR  —— **已废弃（明文）**，本包只读不写、迁移 00018 已清空
//	compute_nodes.agent_token_hash         VARCHAR(64) —— sha256 hex，迁移 00018 新增
//	compute_nodes.agent_token_expires_at   TIMESTAMPTZ —— 迁移 00018 新增，NULL = 永不过期
//	compute_nodes.codecs                   VARCHAR，默认 'h264'，形如 "h264,hevc"
//	compute_nodes.has_nvenc                BOOLEAN，无 NVENC 的 CPU 节点为 false（照常工作）
//	compute_nodes.vram_mb                  INTEGER，可空
//	compute_nodes.concurrency              INTEGER，默认 1
//	compute_nodes.status                   VARCHAR，online|busy|offline（存储态，可能陈旧）
//	compute_nodes.status_locked            BOOLEAN，迁移 00019 新增；true = 管理员强制上/下线，心跳不得改写 status
//	compute_nodes.last_heartbeat           TIMESTAMPTZ —— ⚠️ 列名是 last_heartbeat，不是 last_heartbeat_at
//	compute_nodes.updated_at               TIMESTAMPTZ —— 迁移 00018 新增
//	transcode_jobs.id / media_id / node_id / kind / status / profile / result_path / created_at
//
// ⚠️ 迁移 00018 补 updated_at 的原因（血泪）：compute_nodes 上挂着 set_updated_at_compute_nodes
// 这个 BEFORE UPDATE 触发器（00010 建立），函数体是 NEW.updated_at = now()，但该表建表时
// **没有** updated_at 列（数据库DDL_v1.1.md 第 545–559 行的建表语句同样缺列，属上游 DDL 缺陷）。
// 后果是**任何** UPDATE compute_nodes 都会报 "record \"new\" has no field \"updated_at\""，
// 而心跳、状态变更、令牌轮换全是 UPDATE。这也是该表在 Go 代码里长期零引用的真正原因。
//
// 本文件所有 SQL 都提成包级常量：这样单测能在**不连数据库**的前提下断言语句文本
// （尤其是"不得出现明文 agent_token 列""必须带归属守卫"这类回归点）。

// nodeCols compute_nodes 的读取列。因为要同时用于 SELECT 与 UPDATE ... RETURNING，
// 注意 RETURNING 里不能带表别名前缀（调用方拼 SELECT 时也不加别名）。
const nodeCols = `id::text, name, kind::text, COALESCE(host,''), codecs, has_nvenc,
	vram_mb, concurrency, status::text, last_heartbeat, created_at, status_locked`

// DefaultClaimableKinds 本交付项允许节点认领的任务类型。
//
// **刻意只含 noop**：真实的 kind='hls' 任务仍由 cmd/transcodectl 消费，若让节点也去抢
// pending 的 hls 任务，会与既有转码 worker 争抢同一批行，破坏现网转码流程。
// 本交付项目标是打通「拉取 → 执行 → 回传」协议闭环，不需要也不应该动真实转码队列。
// 接入 ffmpeg 执行器时，把 "hls" 加进来并同时下线 transcodectl 的 hls 消费即可。
var DefaultClaimableKinds = []string{"noop"}

// 包级错误：供 handler 层做状态码映射。
var (
	// ErrNotFound 节点不存在。
	ErrNotFound = errors.New("compute: 节点不存在")
	// ErrNodeInUse 节点仍被转码任务引用，不能删除（FK RESTRICT）。
	ErrNodeInUse = errors.New("compute: 节点仍被转码任务引用，无法删除")
	// ErrInvalidInput 输入非法（校验失败）。
	ErrInvalidInput = errors.New("compute: 输入非法")
	// ErrJobNotFound 回传的任务不存在，或不属于该节点。
	ErrJobNotFound = errors.New("compute: 任务不存在或不属于本节点")
)

// ---- 固定 SQL ----

var (
	listNodesSQL = `SELECT ` + nodeCols + ` FROM compute_nodes ORDER BY name, id`

	getNodeSQL = `SELECT ` + nodeCols + ` FROM compute_nodes WHERE id = $1::uuid`

	// insertNodeSQL 只写 agent_token_hash，**绝不写明文 agent_token 列**。
	// $6/$7 可为 NULL（非 lan_agent 节点不发令牌 / 令牌不过期）。
	insertNodeSQL = `INSERT INTO compute_nodes
		(name, kind, host, codecs, has_nvenc, vram_mb, concurrency, agent_token_hash, agent_token_expires_at)
		VALUES ($1, $2, NULLIF($3,''), $4, $5, $6, $7, $8, $9)
		RETURNING ` + nodeCols

	deleteNodeSQL = `DELETE FROM compute_nodes WHERE id = $1::uuid RETURNING id`

	// findNodeByTokenHashSQL 按哈希定位节点（走 00018 的 idx_nodes_agent_token_hash）。
	// 校验流程是「先对来访明文求 sha256 → 再按哈希查」，所以查询语句里永远不出现明文列。
	findNodeByTokenHashSQL = `SELECT ` + nodeCols + `, agent_token_hash, agent_token_expires_at
		FROM compute_nodes WHERE agent_token_hash = $1`

	// heartbeatSQL 用 COALESCE 实现「未上报的字段不覆盖」：
	// 能力字段只在节点显式声明（首个心跳/重连后重声明）时才更新，
	// 平时的心跳只续 last_heartbeat 与 status。
	//
	// status 单独用 CASE WHEN 处理（缺陷修复点，见迁移 00019 的 status_locked）：
	//   - 未加锁（status_locked = false）→ 取节点自报的 online/busy；未上报时缺省 online。
	//     缺省 online 是「新登记节点（落库取 DDL 默认 'offline'）心跳后必须变 online」的落地点。
	//   - 已加锁（status_locked = true，管理员显式置过 offline/busy）→ 保持原值，心跳不得改写。
	heartbeatSQL = `UPDATE compute_nodes SET
			last_heartbeat = now(),
			status         = CASE WHEN status_locked THEN status ELSE COALESCE($2, 'online') END,
			codecs         = COALESCE($3, codecs),
			has_nvenc      = COALESCE($4, has_nvenc),
			vram_mb        = COALESCE($5, vram_mb),
			concurrency    = COALESCE($6, concurrency)
		WHERE id = $1::uuid
		RETURNING ` + nodeCols

	// pollJobsSQL 原子认领：UPDATE ... WHERE id IN (SELECT ... FOR UPDATE SKIP LOCKED)
	// 保证多个节点并发拉取时不会领到同一条任务，也不会互相阻塞（SKIP LOCKED）。
	// 认领同时把 status 置 running、node_id 落到本节点——这就是 transcode_jobs.node_id
	// 从"永远 NULL"变成真实归属的地方。
	pollJobsSQL = `UPDATE transcode_jobs j SET status = 'running', node_id = $1::uuid
		WHERE j.id IN (
			SELECT id FROM transcode_jobs
			WHERE status = 'pending' AND kind = ANY($3::text[])
			ORDER BY created_at, id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		RETURNING j.id::text, j.kind, j.media_id::text, COALESCE(j.profile,'')`

	// pollInputPathsSQL 补输入路径。media.path 是库内相对路径，
	// 真实绝对路径解析（MediaRoot / UploadDir 回退，同 internal/transcode）留待接入 ffmpeg 时落地。
	pollInputPathsSQL = `SELECT id::text, COALESCE(path,'') FROM media WHERE id = ANY($1::uuid[])`

	// submitResultSQL 回传结果。WHERE 里的归属守卫 (node_id IS NULL OR node_id = $2)
	// 防止一个节点把另一个节点的任务标成完成/失败（节点令牌之间必须互相隔离）。
	submitResultSQL = `UPDATE transcode_jobs SET status = $3, result_path = NULLIF($4,''), node_id = $2::uuid
		WHERE id = $1::uuid AND (node_id IS NULL OR node_id = $2::uuid)
		RETURNING id`
)

// Store 算力节点存取。
type Store struct {
	Pool *pgxpool.Pool
}

// scanNode 按 nodeCols 的顺序扫描一行节点。
func scanNode(row pgx.Row) (*Node, error) {
	var n Node
	if err := row.Scan(&n.ID, &n.Name, &n.Kind, &n.Host, &n.Codecs, &n.HasNVENC,
		&n.VRAMMB, &n.Concurrency, &n.Status, &n.LastHeartbeat, &n.CreatedAt, &n.StatusLocked); err != nil {
		return nil, err
	}
	return &n, nil
}

// applyEffectiveStatus 就地按心跳超时修正生效状态（见 offline.go 的规则说明）。
func applyEffectiveStatus(nodes []Node, offlineAfter time.Duration, now time.Time) {
	for i := range nodes {
		nodes[i].EffectiveStatus = EffectiveStatus(nodes[i].Status, nodes[i].StatusLocked, nodes[i].LastHeartbeat, now, offlineAfter)
	}
}

// List 节点列表（含生效状态）。offlineAfter <= 0 时取 DefaultOfflineAfter。
func (s *Store) List(ctx context.Context, offlineAfter time.Duration) ([]Node, error) {
	if offlineAfter <= 0 {
		offlineAfter = DefaultOfflineAfter
	}
	rows, err := s.Pool.Query(ctx, listNodesSQL)
	if err != nil {
		return nil, fmt.Errorf("查询节点列表失败: %w", err)
	}
	defer rows.Close()

	out := []Node{}
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, fmt.Errorf("读取节点行失败: %w", err)
		}
		out = append(out, *n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历节点行失败: %w", err)
	}
	applyEffectiveStatus(out, offlineAfter, time.Now())
	return out, nil
}

// Get 按 id 取单个节点。
func (s *Store) Get(ctx context.Context, id string, offlineAfter time.Duration) (*Node, error) {
	if offlineAfter <= 0 {
		offlineAfter = DefaultOfflineAfter
	}
	n, err := scanNode(s.Pool.QueryRow(ctx, getNodeSQL, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("查询节点失败: %w", err)
	}
	n.EffectiveStatus = EffectiveStatus(n.Status, n.StatusLocked, n.LastHeartbeat, time.Now(), offlineAfter)
	return n, nil
}

// Register 登记节点。lan_agent 会生成接入令牌并返回明文（**只此一次**），其余类型返回空串。
func (s *Store) Register(ctx context.Context, in RegisterInput) (*Node, string, error) {
	if err := in.Normalize(); err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}

	var plain string
	var hash, expires any // 非 lan_agent 时都为 NULL
	if in.NeedsAgentToken() {
		p, h, err := GenerateAgentToken()
		if err != nil {
			return nil, "", fmt.Errorf("生成节点令牌失败: %w", err)
		}
		plain, hash = p, h
		// 令牌默认不过期（TDD §6.1 只要求"可轮换"，未要求必过期）；
		// 需要过期策略时由管理端直接置 agent_token_expires_at，或后续加轮换周期。
		expires = nil
	}

	n, err := scanNode(s.Pool.QueryRow(ctx, insertNodeSQL,
		in.Name, string(in.Kind), in.Host, in.Codecs, in.HasNVENC, in.VRAMMB, in.Concurrency,
		hash, expires))
	if err != nil {
		return nil, "", fmt.Errorf("登记节点失败: %w", err)
	}
	n.EffectiveStatus = EffectiveStatus(n.Status, n.StatusLocked, n.LastHeartbeat, time.Now(), DefaultOfflineAfter)
	return n, plain, nil
}

// buildNodeUpdate 由 PatchInput 拼出 SET 子句与参数。
//
// 抽成独立纯函数是为了让"部分更新的拼装"能被单测直接覆盖——这段逻辑最容易出错的地方
// 是参数占位符与 args 的下标错位（pgx 会报参数个数不匹配，或更糟：静默写错列），
// 而复刻一份到测试里做断言必然会与实现漂移，等于没测。
//
// 返回的 plain 非空表示本次轮换了令牌（明文只应出现在响应里一次）。
func buildNodeUpdate(in PatchInput) (sets []string, args []any, plain string, err error) {
	add := func(expr string, v any) {
		args = append(args, v)
		sets = append(sets, fmt.Sprintf(expr, len(args)))
	}

	if in.Name != nil {
		add("name = $%d", *in.Name)
	}
	if in.Host != nil {
		// 空串语义 = 清空 host（写 NULL），与 Register 的 NULLIF 保持一致。
		add("host = NULLIF($%d, '')", *in.Host)
	}
	if in.Status != nil {
		add("status = $%d", string(*in.Status))
		// 显式带 status = 管理员的显式意图 → 打/解状态锁（迁移 00019）：
		//   offline / busy = 强制状态，心跳不得改写 → status_locked = true
		//   online         = 解除锁定，交还给心跳自治 → status_locked = false
		// 未带 status 的 PATCH（只改 name/codecs 等）绝不触碰 status_locked。
		//
		// ⚠️ 必须先 add status 再 add status_locked：add 用 len(args) 生成占位符，
		// 顺序反了会让 status_locked 抢走 status 的 $N。
		add("status_locked = $%d", *in.Status != StatusOnline)
	}
	if in.Codecs != nil {
		add("codecs = $%d", *in.Codecs)
	}
	if in.HasNVENC != nil {
		add("has_nvenc = $%d", *in.HasNVENC)
	}
	if in.VRAMMB != nil {
		add("vram_mb = $%d", *in.VRAMMB)
	}
	if in.Concurrency != nil {
		add("concurrency = $%d", *in.Concurrency)
	}
	if in.RotateToken {
		p, h, err := GenerateAgentToken()
		if err != nil {
			return nil, nil, "", fmt.Errorf("生成节点令牌失败: %w", err)
		}
		plain = p
		add("agent_token_hash = $%d", h)
		add("agent_token_expires_at = $%d", nil)
	}
	return sets, args, plain, nil
}

// Patch 部分更新节点。RotateToken 为真时轮换令牌并返回新明文（只此一次）。
func (s *Store) Patch(ctx context.Context, id string, in PatchInput) (*Node, string, error) {
	if err := in.Validate(); err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if in.Empty() {
		return nil, "", fmt.Errorf("%w: 没有任何待更新字段", ErrInvalidInput)
	}

	sets, args, plain, err := buildNodeUpdate(in)
	if err != nil {
		return nil, "", err
	}

	args = append(args, id)
	q := fmt.Sprintf(`UPDATE compute_nodes SET %s WHERE id = $%d::uuid RETURNING %s`,
		strings.Join(sets, ", "), len(args), nodeCols)

	n, err := scanNode(s.Pool.QueryRow(ctx, q, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", fmt.Errorf("更新节点失败: %w", err)
	}
	n.EffectiveStatus = EffectiveStatus(n.Status, n.StatusLocked, n.LastHeartbeat, time.Now(), DefaultOfflineAfter)
	return n, plain, nil
}

// Delete 删除节点。若仍有转码任务引用它（FK 为默认 RESTRICT），返回 ErrNodeInUse。
//
// 刻意不做级联：转码任务的历史归属是审计信息，为了删一个节点就把任务行的 node_id 抹掉
// （或连任务一起删）会破坏"这条任务由谁处理"的记录。让调用方先决定如何处理在跑的任务。
func (s *Store) Delete(ctx context.Context, id string) error {
	var got string
	err := s.Pool.QueryRow(ctx, deleteNodeSQL, id).Scan(&got)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" { // foreign_key_violation
		return ErrNodeInUse
	}
	if err != nil {
		return fmt.Errorf("删除节点失败: %w", err)
	}
	return nil
}

// FindByTokenHash 按令牌哈希定位节点，并返回该行的令牌哈希与过期时刻供 TokenValid 复核。
//
// 之所以按哈希查而不是"取全部节点逐个比对"：既避免 O(n) 次比较，也让明文令牌
// 在 SQL 参数里都不出现（日志/慢查询里就不会留下可用凭据）。
//
// 返回值里的 hash 与入参 hash 必然相等（查询条件就是它）。之所以回传而不是让调用方
// 自己拼，是为了不把哈希塞进 Node 结构体——Node 是要直接序列化给管理端的，
// 多一个字段就多一次"HASH 被下发到前端"的机会。
func (s *Store) FindByTokenHash(ctx context.Context, hash string, offlineAfter time.Duration) (*Node, string, *time.Time, error) {
	if hash == "" {
		return nil, "", nil, ErrNotFound
	}
	if offlineAfter <= 0 {
		offlineAfter = DefaultOfflineAfter
	}
	var (
		n       Node
		gotHash string
		expires *time.Time
	)
	err := s.Pool.QueryRow(ctx, findNodeByTokenHashSQL, hash).Scan(
		&n.ID, &n.Name, &n.Kind, &n.Host, &n.Codecs, &n.HasNVENC,
		&n.VRAMMB, &n.Concurrency, &n.Status, &n.LastHeartbeat, &n.CreatedAt, &n.StatusLocked,
		&gotHash, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", nil, ErrNotFound
	}
	if err != nil {
		return nil, "", nil, fmt.Errorf("按令牌查询节点失败: %w", err)
	}
	n.EffectiveStatus = EffectiveStatus(n.Status, n.StatusLocked, n.LastHeartbeat, time.Now(), offlineAfter)
	return &n, gotHash, expires, nil
}

// Heartbeat 记一次心跳并（可选）刷新能力声明，返回更新后的节点。
//
// 状态处理：节点**只能**自报 online / busy。offline 是"人的决定"（管理端 PATCH 下线）
// 或"心跳超时的推论"，不允许节点自己声明——否则一个卡死的 agent 会把自己标成 offline
// 而又继续发心跳，状态自相矛盾。非法状态值直接忽略（保持心跳可用，不因脏字段阻断续命）。
//
// 未加锁（status_locked=false）时，心跳即使没上报 status 也会把状态落为 online
// ——这是「新登记节点（落库取 DDL 默认 'offline'）心跳后必须变 online」的落地点，
// 由 heartbeatSQL 的 CASE WHEN 完成。已加锁时（管理员 PATCH 显式置过 offline/busy）
// 心跳只续 last_heartbeat，不改写 status。
func (s *Store) Heartbeat(ctx context.Context, nodeID string, hb HeartbeatRequest, offlineAfter time.Duration) (*Node, error) {
	if offlineAfter <= 0 {
		offlineAfter = DefaultOfflineAfter
	}

	var status any
	if hb.Status != "" {
		st := Status(hb.Status)
		if ValidStatus(st) && st != StatusOffline {
			status = string(st)
		}
	}

	var codecs *string
	if hb.Codecs != nil {
		c := strings.TrimSpace(*hb.Codecs)
		if c != "" {
			codecs = &c
		}
	}
	var concurrency *int
	if hb.Concurrency != nil && *hb.Concurrency > 0 && *hb.Concurrency <= MaxConcurrency {
		concurrency = hb.Concurrency
	}
	var vram *int
	if hb.VRAMMB != nil && *hb.VRAMMB >= 0 {
		vram = hb.VRAMMB
	}

	n, err := scanNode(s.Pool.QueryRow(ctx, heartbeatSQL, nodeID, status, codecs, hb.HasNVENC, vram, concurrency))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("更新心跳失败: %w", err)
	}
	n.EffectiveStatus = EffectiveStatus(n.Status, n.StatusLocked, n.LastHeartbeat, time.Now(), offlineAfter)
	return n, nil
}

// PollJobs 原子认领本节点的待处理任务（默认只认领 kind='noop'，见 DefaultClaimableKinds）。
//
// max <= 0 取 MaxPollJobs，max > MaxPollJobs 截断——上限存在的意义是防止一个节点
// 一次把整条队列吞进内存（尤其是任务里带路径/规格字符串时）。
func (s *Store) PollJobs(ctx context.Context, nodeID string, max int) ([]JobSpec, error) {
	if max <= 0 {
		max = MaxPollJobs
	}
	if max > MaxPollJobs {
		max = MaxPollJobs
	}

	rows, err := s.Pool.Query(ctx, pollJobsSQL, nodeID, max, DefaultClaimableKinds)
	if err != nil {
		return nil, fmt.Errorf("拉取任务失败: %w", err)
	}
	defer rows.Close()

	out := []JobSpec{}
	mediaIDs := []string{}
	for rows.Next() {
		var j JobSpec
		if err := rows.Scan(&j.JobID, &j.Kind, &j.MediaID, &j.Profile); err != nil {
			return nil, fmt.Errorf("读取任务行失败: %w", err)
		}
		// OutputSpec 留空：output_spec 是 TDD §6.1 的 poll 字段，但本交付项没有真实
		// 转码产出，填什么都是臆造。接入 ffmpeg 执行器时在此填充档位/分片目录。
		out = append(out, j)
		mediaIDs = append(mediaIDs, j.MediaID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历任务行失败: %w", err)
	}
	if len(out) == 0 {
		return out, nil
	}

	// 补输入路径（一次查询取回全部 media.path，避免 N+1）。
	paths, err := s.inputPaths(ctx, mediaIDs)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].InputPath = paths[out[i].MediaID]
	}
	return out, nil
}

// inputPaths 批量取 media.path。
func (s *Store) inputPaths(ctx context.Context, mediaIDs []string) (map[string]string, error) {
	rows, err := s.Pool.Query(ctx, pollInputPathsSQL, mediaIDs)
	if err != nil {
		return nil, fmt.Errorf("查询媒体路径失败: %w", err)
	}
	defer rows.Close()

	out := make(map[string]string, len(mediaIDs))
	for rows.Next() {
		var id, p string
		if err := rows.Scan(&id, &p); err != nil {
			return nil, fmt.Errorf("读取媒体路径失败: %w", err)
		}
		out[id] = p
	}
	return out, rows.Err()
}

// SubmitResult 回传任务结果并写入 node_id 归属。
//
// 任务失败的原因（r.Error）**不落库**：transcode_jobs 没有可放它的列，而为了一个错误串
// 新增列会牵扯 DDL 变更与历史兼容。当前只回给调用方与日志；待接入 ffmpeg 需要
// 持久化失败细节时再一并设计（例如新增 transcode_job_events 事件表）。
func (s *Store) SubmitResult(ctx context.Context, nodeID string, r ResultRequest) error {
	if r.JobID == "" {
		return fmt.Errorf("%w: job_id 不能为空", ErrInvalidInput)
	}
	if r.Status != JobStatusDone && r.Status != JobStatusFailed {
		return fmt.Errorf("%w: status 需为 done|failed，实际 %q", ErrInvalidInput, r.Status)
	}

	var got string
	err := s.Pool.QueryRow(ctx, submitResultSQL, r.JobID, nodeID, r.Status, r.ResultPath).Scan(&got)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrJobNotFound
	}
	if err != nil {
		return fmt.Errorf("回传任务结果失败: %w", err)
	}
	return nil
}
