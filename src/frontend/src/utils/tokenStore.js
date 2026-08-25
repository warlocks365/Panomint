const ACCESS_KEY = 'pano_access_token'
const REFRESH_KEY = 'pano_refresh_token'

function storages() {
  return [localStorage, sessionStorage]
}

export function saveTokens({ access_token, refresh_token }, remember) {
  clearTokens()
  const store = remember ? localStorage : sessionStorage
  store.setItem(ACCESS_KEY, access_token)
  store.setItem(REFRESH_KEY, refresh_token)
}

export function getAccessToken() {
  for (const s of storages()) {
    const v = s.getItem(ACCESS_KEY)
    if (v) return v
  }
  return null
}

export function getRefreshToken() {
  for (const s of storages()) {
    const v = s.getItem(REFRESH_KEY)
    if (v) return v
  }
  return null
}

export function updateTokens({ access_token, refresh_token }) {
  // 轮换后写回原来所在的存储
  const store = localStorage.getItem(REFRESH_KEY) ? localStorage : sessionStorage
  store.setItem(ACCESS_KEY, access_token)
  store.setItem(REFRESH_KEY, refresh_token)
}

export function clearTokens() {
  for (const s of storages()) {
    s.removeItem(ACCESS_KEY)
    s.removeItem(REFRESH_KEY)
  }
}
