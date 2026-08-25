import axios from 'axios'
import {
  getAccessToken,
  getRefreshToken,
  updateTokens,
  clearTokens
} from '../utils/tokenStore'

const http = axios.create({
  baseURL: 'http://localhost:8080',
  timeout: 15000
})

http.interceptors.request.use((config) => {
  const token = getAccessToken()
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// 并发 401 时共用同一个刷新 Promise，保证只刷新一次
let refreshPromise = null

function refreshTokens() {
  if (!refreshPromise) {
    const refresh_token = getRefreshToken()
    if (!refresh_token) {
      return Promise.reject(new Error('NO_REFRESH_TOKEN'))
    }
    refreshPromise = axios
      .post('http://localhost:8080/auth/refresh', { refresh_token })
      .then((res) => {
        updateTokens(res.data)
        return res.data.access_token
      })
      .finally(() => {
        refreshPromise = null
      })
  }
  return refreshPromise
}

function forceLogout() {
  clearTokens()
  if (window.location.pathname !== '/login') {
    window.location.href = '/login'
  }
}

http.interceptors.response.use(
  (response) => response,
  async (error) => {
    const { response, config } = error
    if (!response) return Promise.reject(error)

    const code = response.data?.error?.code
    const shouldRefresh =
      response.status === 401 &&
      code === 'INVALID_TOKEN' &&
      !config._retried &&
      !config.skipAuthRefresh

    if (!shouldRefresh) {
      return Promise.reject(error)
    }

    config._retried = true
    try {
      const newToken = await refreshTokens()
      config.headers.Authorization = `Bearer ${newToken}`
      return http(config)
    } catch (e) {
      forceLogout()
      return Promise.reject(e)
    }
  }
)

export default http
