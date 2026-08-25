import { defineStore } from 'pinia'
import http from '../api/http'
import {
  saveTokens,
  getAccessToken,
  getRefreshToken,
  clearTokens
} from '../utils/tokenStore'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    user: null
  }),
  getters: {
    isAuthenticated: () => !!getAccessToken()
  },
  actions: {
    async login(email, password, remember) {
      const res = await http.post(
        '/auth/login',
        { email, password },
        { skipAuthRefresh: true }
      )
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
    }
  }
})
