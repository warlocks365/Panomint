// Job000125：登录回跳的安全站内路径校验（守卫与 LoginView 共用，单点实现）。
//
// 规则：
//   - 必须以单个 "/" 开头（站内绝对路径）；
//   - 拒绝 "//" 开头（协议相对 URL，会被浏览器解析为外部协议）；
//   - 拒绝包含 "://"（外部绝对 URL）与控制字符；
//   - 拒绝回到 /login、/setup 本身（避免登录后死循环回登录页 / 回向导）；
//   - 其余一律视为非法（调用方回退默认页 /timeline）。
export function safeInternalPath(v) {
  if (typeof v !== 'string' || !v.startsWith('/')) return ''
  if (v.startsWith('//')) return ''
  if (/[\0-\x1f\x7f]/.test(v)) return ''
  if (/^[a-z][a-z0-9+.-]*:/i.test(v)) return '' // 任意 scheme: 前缀（含 https:、javascript: 等）
  if (v === '/login' || v.startsWith('/login?') || v === '/setup' || v.startsWith('/setup?')) return ''
  return v
}
