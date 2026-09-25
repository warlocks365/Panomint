// 版本信息 API（Job000117）：GET /version 为公开端点（Job000105），无需登录。
// 响应形状：{version, commit, build_date}——version 恒有值（发布产物注入；开发构建恒 "dev"）。
import http from './http'

export function getVersion() {
  return http.get('/version').then((r) => r.data)
}
