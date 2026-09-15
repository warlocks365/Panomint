package compute

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"time"
)

// Agent 接入令牌：**只存哈希，不存明文**。
//
// 设计依据：TDD v1.1 §6.1「首次连接：POST /api/compute-nodes 获取 agent_token →
// 连接时 Authorization: Bearer <agent_token>」；§8 安全表「agent_token 轮换（管理员触发）」。
//
// 存储约定（配合迁移 00018）：
//   - 明文令牌**产生后只在登记/轮换的响应里返回一次**，此后任何接口都取不回；
//   - 库中只落 sha256 hex（compute_nodes.agent_token_hash），明文列 compute_nodes.agent_token
//     已废弃、Go 代码不读不写；
//   - 校验时先对来访明文求哈希、再按哈希查表（走 idx_nodes_agent_token_hash），
//     因此库中不存在可用于直接冒用的凭据。
//
// 为什么用 sha256 而不是 bcrypt：令牌是本进程生成的 32 字节高熵随机串（非用户口令），
// 不存在字典攻击面，无需慢哈希；而心跳/拉取是每 60s 一次的**热路径**，bcrypt 的
// 每次 ~50ms 会在多节点下变成可观的固定开销。这与项目既有做法一致
// （internal/auth/store.go 的 refresh token 同样用 sha256）。

// AgentTokenBytes 令牌随机字节数（32 字节 → 64 位 hex，与 auth.NewRefreshToken 同规格）。
const AgentTokenBytes = 32

// GenerateAgentToken 生成接入令牌，同时返回明文与其哈希。
//
// 返回值 plain 只应出现在"登记成功 / 轮换成功"这一次 HTTP 响应里；
// 入库的是 hash，两者不能互换使用。
func GenerateAgentToken() (plain string, hash string, err error) {
	b := make([]byte, AgentTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	plain = hex.EncodeToString(b)
	return plain, HashAgentToken(plain), nil
}

// HashAgentToken 计算令牌的 sha256 hex（64 字符，正好落进 VARCHAR(64)）。
func HashAgentToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// TokenValid 校验来访令牌是否匹配库中哈希且未过期。
//
//   - storedHash 为空 → 该节点未启用 agent（local_gpu/cloud_gpu），一律拒绝；
//   - presented 为空 → 拒绝（避免空令牌撞上空哈希）；
//   - expiresAt 为 nil → 永不过期（库列为 NULL，TDD §6.1 只要求可轮换、未要求必过期）；
//   - now >= expiresAt → 已过期（边界取"到期即失效"，不留 1ns 的模糊地带）；
//   - 比较用 subtle.ConstantTimeCompare：虽然查表本身已按哈希定位，这里仍做常量时间比较，
//     以免将来改成"先取节点再比对"时留下时序侧信道。
func TokenValid(storedHash, presented string, expiresAt *time.Time, now time.Time) bool {
	if storedHash == "" || presented == "" {
		return false
	}
	if expiresAt != nil && !now.Before(*expiresAt) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(HashAgentToken(presented)), []byte(storedHash)) == 1
}
