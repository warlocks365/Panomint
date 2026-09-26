import { defineStore } from 'pinia'
import http from '../api/http'
import {
  saveTokens,
  getAccessToken,
  getRefreshToken,
  clearTokens
} from '../utils/tokenStore'

// 登录失败的两种"需要二次验证"语义（与后端 internal/auth/handlers.go 的错误码一一对应）。
// 刻意分开：MFA_REQUIRED 意味着"你还没输码"，前端应弹出输入框；
// MFA_INVALID 意味着"码输了但不对"，前端应保留输入框并提示错误。
// 两者若共用一种提示，用户会不知道到底是哪里出了问题。
export const MFA_REQUIRED = 'MFA_REQUIRED'
export const MFA_INVALID = 'MFA_INVALID'

// errCode 从 axios 错误里取后端错误码（形如 {error:{code,message}}）。
export function errCode(e) {
  return e?.response?.data?.error?.code || ''
}

export function errMessage(e, fallback) {
  return e?.response?.data?.error?.message || fallback
}

export const useAuthStore = defineStore('auth', {
  state: () => ({
    user: null
  }),
  getters: {
    isAuthenticated: () => !!getAccessToken(),
    // 二次验证当前是否已生效（来自 /auth/me，避免前端自己维护一份可能过期的副本）
    mfaEnabled: (s) => !!s.user?.mfa_enabled,
    // "配到一半"：已生成密钥但尚未用口令确认。界面需要据此提示重新生成，
    // 否则用户刷新页面后就无从知道自己停在哪一步。
    mfaPending: (s) => !!s.user?.mfa_pending,
    // 强制改密（Job000128）：管理员重置密码后置位（登录响应与 /auth/me 都会带出）。
    // 路由守卫据此把用户困在设置页，直到改密成功（服务端清除标记）。
    mustChangePassword: (s) => !!s.user?.must_change_password
  },
  actions: {
    // totpCode 可选：未启用二次验证的账号照旧只传邮箱密码。
    async login(email, password, remember, totpCode) {
      const body = { email, password }
      if (totpCode) body.totp_code = String(totpCode).trim()
      const res = await http.post('/auth/login', body, { skipAuthRefresh: true })
      saveTokens(res.data, remember)
      await this.fetchMe()
    },
    async fetchMe() {
      const res = await http.get('/auth/me')
      this.user = res.data
      return this.user
    },
    async logout() {
      const refresh_token = getRefreshToken()
      try {
        if (refresh_token) {
          await http.post('/auth/logout', { refresh_token }, { skipAuthRefresh: true })
        }
      } catch (e) {
        // 登出请求失败不阻塞本地清理
      }
      clearTokens()
      this.user = null
    },

    // ---- 二次验证（TOTP）----
    // 三个动作成功后都刷新 /auth/me：界面显示的启用状态只有一个来源（服务端），
    // 前端不自己推断，避免"界面显示已启用但服务端其实没生效"这类不一致。

    // 生成待确认密钥。**密钥只在这一次响应里返回**（服务端只存它本身，无法再取回），
    // 所以调用方必须立刻把它显示给用户。
    async mfaSetup() {
      const res = await http.post('/auth/mfa/setup')
      return res.data
    },
    async mfaConfirm(code) {
      const res = await http.post('/auth/mfa/confirm', { code: String(code).trim() })
      await this.fetchMe()
      return res.data
    },
    async mfaDisable(code) {
      const res = await http.post('/auth/mfa/disable', { code: String(code).trim() })
      await this.fetchMe()
      return res.data
    },

    // ---- 自助改密（Job000034）----
    // 成功后服务端吊销该用户**全部**会话（含当前这个），所以这里刻意不 fetchMe——
    // 任何带旧 token 的请求都会 401。调用方负责登出并引导用新口令重新登录。
    async changePassword(current, next) {
      const res = await http.put('/user/password', {
        current_password: current,
        new_password: next
      })
      return res.data
    },

    // ---- 应用密码（Job000098）----
    // WebDAV 等第三方客户端专用密码：与主密码分离、可单独轮换/清除，
    // 泄漏面比"把主密码交给客户端"小得多。生成/清除都要当前密码（同改密的
    // "捡到开着的电脑"防线）；轮换/清除不吊销任何会话（应用密码不走登录、
    // 不产生会话），因此成功后不需要像 changePassword 那样登出。
    // GET 只回 {set}——明文只在 generate 的响应里出现一次，服务端只存散列。
    async fetchAppPasswordStatus() {
      const res = await http.get('/user/app-password')
      return res.data
    },
    async generateAppPassword(current) {
      const res = await http.put('/user/app-password', { current_password: current })
      return res.data
    },
    async clearAppPassword(current) {
      const res = await http.post('/user/app-password/clear', { current_password: current })
      return res.data
    },

    // ---- SSO/OIDC 回调登录（Job000054）----
    // IdP 带 code/state 跳回 /login 后调用；响应与密码登录同形（tokenPair）。
    // SSO 失败错误码与密码登录无关，这里原样上抛，由调用方按 code 分支提示。
    async loginWithSSO(code, state) {
      const res = await http.post('/auth/sso/oidc', { code, state }, { skipAuthRefresh: true })
      saveTokens(res.data, true)
      await this.fetchMe()
    },

    // SSO 是否启用（GET /auth/sso/config → {enabled}）。失败按未启用处理——
    // 按钮不出现比出现一个点不动的按钮好。
    async fetchSSOEnabled() {
      try {
        const res = await http.get('/auth/sso/config', { skipAuthRefresh: true })
        return !!res.data?.enabled
      } catch {
        return false
      }
    }
  }
})
