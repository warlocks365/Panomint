// Agent 语义接口 API（Job000140 Phase 1，设计 §4/§5）——POST /admin/agent/cmd。
// 与 WSS 调试通道（api/debug.js）同一权限锁（admin:system，服务端强校验）；
// 同一命令核心（后端 commands_core.go），错误走项目标准 httperr 形态（status+code）。
import http from './http'

// POST /admin/agent/cmd ← {type, payload?}
// → 200 {ok:true, cmd, result}；失败按错误码映射状态：
//   UNKNOWN_COMMAND/INVALID_PARAMS 400 ｜ JOB_NOT_FOUND 404 ｜ INVALID_STATE 409 ｜ INTERNAL 500
//   （错误体为 httperr.Envelope{code,message}，errMessage/errCode 通用解析）
export function postAgentCmd(type, payload) {
  const body = payload === undefined ? { type } : { type, payload }
  return http.post('/admin/agent/cmd', body).then((r) => r.data)
}
