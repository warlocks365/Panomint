// Agent 语义接口 LLM 上游配置 API（Job000140 Phase 2）——admin:system 门禁（服务端强校验）。
// api_key 三态契约：非空串=更新；空串=清除；null/缺省=保留旧值（key 不可回显的现实妥协）。
import http from './http'

// GET /admin/agent/llm-config → {enabled, base_url, model, has_key, key_tail}
export function getAgentLlmConfig() {
  return http.get('/admin/agent/llm-config').then((r) => r.data)
}

// PUT /admin/agent/llm-config ← {base_url, model, api_key, enabled} → 204
export function saveAgentLlmConfig(cfg) {
  return http.put('/admin/agent/llm-config', cfg).then((r) => r.data)
}

// POST /admin/agent/llm-config/test ← {base_url?, api_key?} → {ok, error?}
export function testAgentLlmConfig(cfg) {
  return http.post('/admin/agent/llm-config/test', cfg || {}).then((r) => r.data)
}
