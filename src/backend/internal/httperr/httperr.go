// Package httperr 是 HTTP 错误响应形状与「内部错误不回显」的单一真源。
//
// 存在的理由不是"整洁"，而是**避免同一逻辑复制多份后漂移**：本仓的
// errResp / fail / errJSON 曾各自在 8 个包里各写一份 c.JSON(gin.H{"error": ...})，
// 而"把 err.Error() 回给客户端"这个缺陷正是沿着这些复制品各自蔓延的
// （本项目已因"同一逻辑复制多份后漂移"出过三次严重事故）。
//
// 两条边界（改动前务必读懂）：
//
//  1. Envelope 只负责**形状**，不判断消息内容。
//  2. Fail 只用于**来自本端之外**的错误（DB/驱动/文件系统/第三方库）。
//     它把完整错误写进服务端日志（排障必需），给客户端的 message 永远是你传进来的
//     固定文案 —— 绝不要把 err.Error() 当 msg 传进来，那会把 SQLSTATE、表名、
//     列名、类型名、文件路径等实现细节漏给调用方。
//
// 本端自己构造、面向用户的校验文案（例如 ErrInvalidGranularity.Error()）不属于这一
// 类，直接用 Envelope 原样返回即可，不要套 Fail。
package httperr

import (
	"log"

	"github.com/gin-gonic/gin"
)

// Envelope 写统一错误封套 {"error":{"code","message"}}。
//
// msg 由调用方负责：面向用户的文案可以原样传；**绝不能**把回显内部实现的原文传进来。
func Envelope(c *gin.Context, status int, code, msg string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": msg}})
}

// EnvelopeExtra 写统一封套并在**顶层**附加额外字段。
//
// 存在理由（Job000128）：个别错误响应需要在封套同级携带结构化附加数据——
// 如 423 锁定响应的 remaining_minute（前端据此跑本地倒计时）。附加字段放顶层
// 不破坏 {error:{...}} 子树，老客户端读 error 字段零感知。
// 需要新形状时扩展本文件，而不是在别的包复制 gin.H{"error": ...} 字面量
//（封套形状守卫 TestEnvelopeShapeHasSingleImplementation 强制这一点）。
//
// extra 中若含 "error" 键会被忽略——顶层 "error" 是封套本体，不允许覆盖。
// extra 为 nil 等价于 Envelope。
func EnvelopeExtra(c *gin.Context, status int, code, msg string, extra gin.H) {
	body := gin.H{"error": gin.H{"code": code, "message": msg}}
	for k, v := range extra {
		if k == "error" {
			continue
		}
		body[k] = v
	}
	c.JSON(status, body)
}

// Fail 处理来自本端之外的错误：完整错误只进服务端日志，客户端只拿固定文案 msg。
//
// 与 Envelope 的分工：msg 必须是**与 err 无关的固定字符串**（如"查询失败"），
// 由调用方选定；err 只用于日志。若某处确实需要把本端构造的校验文案回给用户，
// 用 Envelope 而不是 Fail。
func Fail(c *gin.Context, status int, code, msg string, err error) {
	if err != nil {
		log.Printf("[httperr] %s %s -> %d %s: %v",
			c.Request.Method, c.Request.URL.Path, status, code, err)
	}
	Envelope(c, status, code, msg)
}

// Abort 写统一封套**并中断**后续 handler（等价于 gin 的 AbortWithStatusJSON）。
//
// 为什么必须有它：中间件（鉴权 / 限流 / panic 恢复）用的不是 c.JSON 而是
// `c.AbortWithStatusJSON` —— 后者除了写响应还会 `c.Abort()`，让后续处理器不再执行。
// 若把这些地方直接换成只写 JSON 的 Envelope，会**静默丢掉中断语义**：
// 响应看起来正常，但 handler 链会继续往下跑（可能重复写响应或在无身份的情况下继续处理）。
// 因此 Abort 与 Envelope 是**两个不可互相替代**的入口，共同保证封套形状只有一份实现。
func Abort(c *gin.Context, status int, code, msg string) {
	Envelope(c, status, code, msg)
	c.Abort()
}
