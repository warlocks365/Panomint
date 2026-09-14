// 带宽配置 API（Phase 4 P1）—— 登录态。分享页（免登）走
// src/components/shares/publicApi.js::measureShareBandwidth，两者探针尺寸/上限保持一致。
import http from './http'
import { getAccessToken } from '../utils/tokenStore'

const API_BASE = import.meta.env.VITE_API_BASE || ''

// 探针尺寸：与 publicApi.js / 后端 bandwidth.go 同口径
const BW_DOWN_BYTES = 256 * 1024
const BW_UP_BYTES = 128 * 1024
const BW_MAX_KBPS = 5000000

function clampKbps(v) {
  if (!Number.isFinite(v) || v <= 0) return 0
  return Math.min(BW_MAX_KBPS, Math.round(v))
}

function randomBytes(n) {
  const b = new Uint8Array(n)
  for (let off = 0; off < n; off += 65536) {
    crypto.getRandomValues(b.subarray(off, Math.min(off + 65536, n)))
  }
  return b
}

function probeUrl(bytes) {
  return `${API_BASE}/bandwidth/probe?bytes=${bytes}`
}

// GET /bandwidth → {up_kbps, down_kbps, source}；source=none 表示无数据（调用方回落默认 ABR）
export function getBandwidth() {
  return http.get('/bandwidth').then((r) => r.data)
}

// PATCH /bandwidth {up_kbps?, down_kbps?, scope:'global'|'share_token', ref_id?}
// → 写 bandwidth_profiles(source=manual)。360 播放页据此选 HLS 档位。
export function patchBandwidth(payload) {
  return http.patch('/bandwidth', payload).then((r) => r.data)
}

// selfTestBandwidth 登录态带宽自测：与分享页同构的 3 步（下行探针浏览器计时 /
// 上行探针服务端计时 / 汇总落库），探针端点需 Bearer，故用裸 fetch 带 Authorization。
export async function selfTestBandwidth() {
  const headers = {}
  const token = getAccessToken()
  if (token) headers.Authorization = `Bearer ${token}`

  const t0 = performance.now()
  const dl = await fetch(probeUrl(BW_DOWN_BYTES), { headers, cache: 'no-store' })
  if (!dl.ok) throw new Error(`bandwidth probe HTTP ${dl.status}`)

  let received = 0
  let firstByteMs = 0
  if (dl.body && typeof dl.body.getReader === 'function') {
    const reader = dl.body.getReader()
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break
      if (!firstByteMs) firstByteMs = performance.now() - t0
      received += value.byteLength
    }
  } else {
    received = (await dl.arrayBuffer()).byteLength
  }
  const elapsedMs = Math.max(1, performance.now() - t0)
  const downKbps = clampKbps((received * 8) / (elapsedMs / 1000) / 1000)
  const latencyMs = Math.round(firstByteMs)
  if (downKbps <= 0) throw new Error('bandwidth probe 无有效载荷')

  let upKbps = 0
  try {
    const up = await fetch(probeUrl(BW_UP_BYTES), {
      method: 'POST',
      headers,
      body: randomBytes(BW_UP_BYTES),
      cache: 'no-store'
    })
    if (up.ok) upKbps = clampKbps(Number((await up.json())?.up_kbps) || 0)
  } catch {
    upKbps = 0
  }

  const local = { up_kbps: upKbps, down_kbps: downKbps, latency_ms: latencyMs }
  try {
    const res = await fetch(
      `${API_BASE}/bandwidth/self-test?down_kbps=${downKbps}&latency_ms=${latencyMs}`,
      { method: 'POST', headers, cache: 'no-store' }
    )
    if (res.ok) return await res.json()
  } catch {
    /* 回落本地测量值 */
  }
  return local
}
