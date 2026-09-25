package debug

// Store：debug_channel 单行表的数据访问。库是通道状态的唯一真源（设计 §8.3）；
// 连接槽/订阅表是 api 进程内易失态，不在本层。
//
// 并发纪律：一切凭据变更（Enable/Rotate/Disable/ExpireDue）与握手认证（Authenticate）
// 都在事务内先取 pg_advisory_xact_lock(AdvisoryLockID) 再读写 —— 这是
// "换钥瞬间旧钥仍握手成功"窗口的关闭手段（设计 §8.2）。

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Channel 通道行视图（无明钥；KeyDigest 仅供握手比对，绝不外发/落审计）。
type Channel struct {
	Enabled       bool
	ChannelID     string
	KeyDigest     string // sha256(access_key)；json:"-" 防序列化外泄
	CreatedBy     string
	CreatedAt     time.Time
	ExpiresAt     time.Time
	LastConnectAt *time.Time
	LastConnectIP string
}

// Store 数据访问层。Pool 不可为空。
type Store struct {
	Pool *pgxpool.Pool
}

const channelColumns = `enabled, channel_id, key_digest, created_by::text, created_at, expires_at, last_connect_at, last_connect_ip`

func scanChannel(row pgx.Row) (*Channel, error) {
	var ch Channel
	err := row.Scan(&ch.Enabled, &ch.ChannelID, &ch.KeyDigest, &ch.CreatedBy,
		&ch.CreatedAt, &ch.ExpiresAt, &ch.LastConnectAt, &ch.LastConnectIP)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

// Status 读通道行；无行返回 (nil, nil)——调用方按"未开启"处理。
func (s *Store) Status(ctx context.Context) (*Channel, error) {
	return scanChannel(s.Pool.QueryRow(ctx, `SELECT `+channelColumns+` FROM debug_channel WHERE id = true`))
}

// PeekByChannelID 按 channel_id 读行（无锁，供握手前置 404 判别）。
// 行不存在或与 channel_id 不符都返回 (nil, nil)——未开启/已过期/不存在一视同仁（不可探测）。
func (s *Store) PeekByChannelID(ctx context.Context, channelID string) (*Channel, error) {
	return scanChannel(s.Pool.QueryRow(ctx,
		`SELECT `+channelColumns+` FROM debug_channel WHERE id = true AND channel_id = $1`, channelID))
}

// Enable 开启通道并签发全新凭据（设计 §8.1 首行）。
// 已开启且未过期 → ErrAlreadyEnabled（409，防误操作轮换掉正在使用的密钥）；
// 行不存在 / 已关闭 / 已过期 → 全新凭据 UPSERT（过期不悬挂，等同关闭态）。
// 返回 (channelID, 明文密钥, 到期时间)。明文仅这一次出境。
func (s *Store) Enable(ctx context.Context, userID string, ttlHours int) (string, string, time.Time, error) {
	if !ValidTTL(ttlHours) {
		return "", "", time.Time{}, ErrBadTTL
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", "", time.Time{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, AdvisoryLockID); err != nil {
		return "", "", time.Time{}, err
	}

	var existingEnabled bool
	var existingExpires time.Time
	rowExists := true
	err = tx.QueryRow(ctx, `SELECT enabled, expires_at FROM debug_channel WHERE id = true`).
		Scan(&existingEnabled, &existingExpires)
	if errors.Is(err, pgx.ErrNoRows) {
		rowExists = false
	} else if err != nil {
		return "", "", time.Time{}, err
	}
	if rowExists && existingEnabled && time.Now().Before(existingExpires) {
		return "", "", time.Time{}, ErrAlreadyEnabled
	}

	// 撞 UNIQUE 重生成重试 ≤3（设计 §3.2 工程兜底；advisory lock 下理论无竞争，保险丝而已）。
	var channelID, plainKey string
	var expiresAt time.Time
	for i := 0; i < 3; i++ {
		channelID, plainKey, err = generateCredentials()
		if err != nil {
			return "", "", time.Time{}, err
		}
		if rowExists {
			err = tx.QueryRow(ctx, `
				UPDATE debug_channel
				SET enabled = true, channel_id = $1, key_digest = $2, created_by = $3,
				    created_at = now(), expires_at = now() + make_interval(hours => $4),
				    last_connect_at = NULL, last_connect_ip = NULL
				WHERE id = true RETURNING expires_at`,
				channelID, HashAccessKey(plainKey), userID, ttlHours).Scan(&expiresAt)
		} else {
			err = tx.QueryRow(ctx, `
				INSERT INTO debug_channel (id, enabled, channel_id, key_digest, created_by, expires_at)
				VALUES (true, true, $1, $2, $3, now() + make_interval(hours => $4))
				RETURNING expires_at`,
				channelID, HashAccessKey(plainKey), userID, ttlHours).Scan(&expiresAt)
		}
		if err == nil {
			break
		}
		if !isUniqueViolation(err) {
			return "", "", time.Time{}, err
		}
	}
	if err != nil {
		return "", "", time.Time{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", "", time.Time{}, err
	}
	return channelID, plainKey, expiresAt, nil
}

// Rotate 重置密钥（设计 §6.3）：新 channel_id+key 替换旧值，旧密钥在提交瞬间即失效
// （摘要查不对 + 通道 id 换掉），旧连接由调用方踢除（Close 4004）。
// 未开启（无行/enabled=false/已过期）→ ErrNotEnabled（409）。
// 成功时同时返回**旧**通道指纹（审计 debug.rotate 的 old_channel_fp 用）。
func (s *Store) Rotate(ctx context.Context, userID string, ttlHours int) (channelID, plainKey string, expiresAt time.Time, oldFP string, err error) {
	if !ValidTTL(ttlHours) {
		return "", "", time.Time{}, "", ErrBadTTL
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", "", time.Time{}, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, AdvisoryLockID); err != nil {
		return "", "", time.Time{}, "", err
	}
	ch, err := scanChannel(tx.QueryRow(ctx, `SELECT `+channelColumns+` FROM debug_channel WHERE id = true FOR UPDATE`))
	if err != nil {
		return "", "", time.Time{}, "", err
	}
	if ch == nil || !ch.Enabled || !time.Now().Before(ch.ExpiresAt) {
		return "", "", time.Time{}, "", ErrNotEnabled
	}
	oldFP = Fingerprint(ch.ChannelID)

	for i := 0; i < 3; i++ {
		channelID, plainKey, err = generateCredentials()
		if err != nil {
			return "", "", time.Time{}, "", err
		}
		err = tx.QueryRow(ctx, `
			UPDATE debug_channel
			SET enabled = true, channel_id = $1, key_digest = $2, created_by = $3,
			    created_at = now(), expires_at = now() + make_interval(hours => $4),
			    last_connect_at = NULL, last_connect_ip = NULL
			WHERE id = true RETURNING expires_at`,
			channelID, HashAccessKey(plainKey), userID, ttlHours).Scan(&expiresAt)
		if err == nil {
			break
		}
		if !isUniqueViolation(err) {
			return "", "", time.Time{}, "", err
		}
	}
	if err != nil {
		return "", "", time.Time{}, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", "", time.Time{}, "", err
	}
	return channelID, plainKey, expiresAt, oldFP, nil
}

// Disable 关闭通道（幂等，设计 §6.4）：未开启也成功，返回 wasEnabled=false。
// 关闭不清行（channel_id/key_digest 保留至下次 enable 覆盖）。
// 返回 (wasEnabled, 通道指纹)——踢连与审计用。
func (s *Store) Disable(ctx context.Context) (bool, string, error) {
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, AdvisoryLockID); err != nil {
		return false, "", err
	}
	ch, err := scanChannel(tx.QueryRow(ctx, `SELECT `+channelColumns+` FROM debug_channel WHERE id = true FOR UPDATE`))
	if err != nil {
		return false, "", err
	}
	if ch == nil || !ch.Enabled {
		return false, "", tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `UPDATE debug_channel SET enabled = false WHERE id = true`); err != nil {
		return false, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, "", err
	}
	return true, Fingerprint(ch.ChannelID), nil
}

// ExpireDue 到期巡检（reaper 周期调用，设计 §4.2 条件 1）：
// 行已开启且 expires_at <= now() → 置 enabled=false，返回 (true, 通道指纹)；
// 否则 (false, "")。同一函数也被握手续期复核复用语义。
func (s *Store) ExpireDue(ctx context.Context, now time.Time) (bool, string, error) {
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, AdvisoryLockID); err != nil {
		return false, "", err
	}
	var channelID string
	err = tx.QueryRow(ctx,
		`UPDATE debug_channel SET enabled = false
		 WHERE id = true AND enabled = true AND expires_at <= $1
		 RETURNING channel_id`, now).Scan(&channelID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, "", tx.Commit(ctx)
	}
	if err != nil {
		return false, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, "", err
	}
	return true, Fingerprint(channelID), nil
}

// Authenticate 握手认证（设计 §5.2 步骤 6/8 的库侧部分）：
// advisory lock → 按 channel_id 读行 → 校验 enabled/未过期/摘要 → touch 接入信息。
// 认证结果四态：
//
//   - (ch, nil)              —— 通过，ch 即通道行（调用方继续 TLS 检查与 upgrade）；
//   - (nil, ErrNotEnabled)   —— 行不存在或已关闭 → 404（与"不存在"同形，不可探测）；
//   - (nil, ErrExpired)      —— 已开启但已过期（顺手置 enabled=false）→ 404 + 审计 reason=expired；
//   - (nil, ErrAuthFailed)   —— 摘要比对失败 → 401（调用方做失败计数）。
//
// 全过程在单事务内：换钥（Rotate）与认证互斥，旧密钥不可能"认证途中被换还放行"。
func (s *Store) Authenticate(ctx context.Context, channelID, presentedKey, peerIP string, now time.Time) (*Channel, error) {
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, AdvisoryLockID); err != nil {
		return nil, err
	}
	ch, err := scanChannel(tx.QueryRow(ctx,
		`SELECT `+channelColumns+` FROM debug_channel WHERE id = true AND channel_id = $1 FOR UPDATE`, channelID))
	if err != nil {
		return nil, err
	}
	if ch == nil || !ch.Enabled {
		return nil, ErrNotEnabled
	}
	if !now.Before(ch.ExpiresAt) {
		// 顺手置失效（幂等），与 reaper 同语义；审计由调用方补 debug.disable{reason:expired}。
		if _, err := tx.Exec(ctx, `UPDATE debug_channel SET enabled = false WHERE id = true`); err != nil {
			return nil, err
		}
		_ = tx.Commit(ctx)
		return nil, ErrExpired
	}
	if !KeyValid(ch.KeyDigest, presentedKey, ch.ExpiresAt, now) {
		_ = tx.Rollback(ctx) // 失败不写 last_connect_*，也不占锁提交
		return nil, ErrAuthFailed
	}
	if _, err := tx.Exec(ctx,
		`UPDATE debug_channel SET last_connect_at = now(), last_connect_ip = $1 WHERE id = true`, peerIP); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return ch, nil
}

// generateCredentials 生成一对新凭据。
func generateCredentials() (channelID, plainKey string, err error) {
	channelID, err = GenerateChannelID()
	if err != nil {
		return "", "", err
	}
	plainKey, err = GenerateAccessKey()
	if err != nil {
		return "", "", err
	}
	return channelID, plainKey, nil
}

// isUniqueViolation 判定 PG 唯一约束冲突（23505）。
// pgconn.PgError 经 pgx 透传；不用 pgerrcode 常量包以保持依赖最小，23505 是 SQL 标准码。
func isUniqueViolation(err error) bool {
	type sqlStater interface{ SQLState() string }
	var st sqlStater
	if errors.As(err, &st) {
		return st.SQLState() == "23505"
	}
	return false
}
