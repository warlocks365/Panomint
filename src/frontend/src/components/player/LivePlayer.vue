<!-- LivePlayer：HEVC 兼容播放（实时转码兜底，Job000131 P1）。
     触发语义：用户在「浏览器不支持该编码」提示下显式点击启用——服务端按需 ffmpeg
     转码 720p 单档动态 HLS。seek = 携带 pos 参数重建会话（旧会话自动回收）。
     与「关闭自动 HLS 转码」语义兼容：不自动转码，仅人工按需兜底。 -->
<template>
  <div class="live-player" data-testid="live-player">
    <video ref="videoEl" class="live-video" playsinline muted></video>
    <div class="live-bar">
      <button class="btn btn--sm" data-testid="live-toggle" @click="toggle">{{ playing ? '暂停' : '播放' }}</button>
      <span class="live-time" data-testid="live-time">{{ fmt(displayPos) }} / {{ fmt(duration) }}</span>
      <input
        class="live-seek"
        data-testid="live-seek"
        type="range"
        min="0"
        max="1000"
        :value="seekValue"
        @change="onSeek"
      />
      <button class="btn btn--sm btn--ghost" data-testid="live-restart" @click="restart">从头播放</button>
    </div>
    <p class="live-hint" data-testid="live-hint">
      兼容播放 · 720p 单档实时转码 · 服务器 CPU 开销较高 · 首次缓冲约 10 秒（取决于服务器算力）· 拖动进度会重新定位
    </p>
    <p v-if="errMsg" class="live-err" data-testid="live-err">{{ errMsg }}</p>
  </div>
</template>

<script setup>
import { onBeforeUnmount, onMounted, ref } from 'vue'
import Hls from 'hls.js'
import http from '../../api/http'
import { getAccessToken } from '../../utils/tokenStore'

const props = defineProps({ mediaId: { type: String, required: true } })

const videoEl = ref(null)
const playing = ref(false)
const duration = ref(0)
const displayPos = ref(0) // 会话起点 + hls 相对播放位置
const seekValue = ref(0)
const errMsg = ref('')

let hls = null
let startPos = 0
let hlsOffset = 0 // 当前 hls 实例内已播放秒数（liveDuration 回调累计）

const API_BASE = http.defaults.baseURL

function buildUrl(pos) {
  const token = getAccessToken()
  const base = `${API_BASE}/media/${props.mediaId}/live/master.m3u8?pos=${Math.max(0, Math.round(pos))}`
  return token ? `${base}&at=${encodeURIComponent(token)}` : base
}

function destroyHls() {
  if (hls) {
    hls.destroy()
    hls = null
  }
}

function attach(pos) {
  errMsg.value = ''
  destroyHls()
  const v = videoEl.value
  if (!v) return
  if (Hls.isSupported()) {
    hls = new Hls({
      maxBufferLength: 20,
      xhrSetup: (xhr) => {
        const token = getAccessToken()
        if (token) xhr.setRequestHeader('Authorization', 'Bearer ' + token)
      },
    })
    hls.loadSource(buildUrl(pos))
    hls.attachMedia(v)
    hls.on(Hls.Events.MANIFEST_PARSED, () => {
      startPos = pos
      hlsOffset = 0
      v.play().catch(() => {})
      playing.value = !v.paused
    })
    hls.on(Hls.Events.ERROR, (_, data) => {
      if (!data.fatal) return
      if (data.response?.code === 503 || data.response?.code === 504) {
        errMsg.value = '已有实时转码任务进行中，请稍后重试'
      } else if (data.response?.code === 404 && data.url?.includes('master.m3u8')) {
        errMsg.value = '转码启动失败，请重试'
      } else if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
        // 会话被回收（空闲超时/源播完）→ 提示而非无限重试
        errMsg.value = '转码会话已结束，请重新点击播放'
      } else {
        errMsg.value = '播放失败，请重试'
      }
    })
    // 动态流：用 LEVEL_LOADED 的累计时长估算总长（首片即得）
    hls.on(Hls.Events.LEVEL_LOADED, (_, d) => {
      if (d?.details?.totalduration) duration.value = startPos + d.details.totalduration
    })
  } else {
    errMsg.value = '当前浏览器不支持兼容播放（需支持 MSE）'
  }
}

function toggle() {
  const v = videoEl.value
  if (!v) return
  if (v.paused) {
    v.play().catch(() => {})
    playing.value = true
  } else {
    v.pause()
    playing.value = false
  }
}

function onSeek(e) {
  const ratio = Number(e.target.value) / 1000
  if (!duration.value) return
  const target = ratio * duration.value
  displayPos.value = target
  attach(target) // 重建会话从 target 起播
}

function restart() {
  displayPos.value = 0
  attach(0)
}

function fmt(t) {
  if (!isFinite(t) || t < 0) return '00:00'
  const m = Math.floor(t / 60)
  const s = Math.floor(t % 60)
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
}

let rafId = 0
function tick() {
  const v = videoEl.value
  if (v) {
    hlsOffset = v.currentTime || 0
    displayPos.value = startPos + hlsOffset
    if (duration.value) seekValue.value = Math.round((displayPos.value / duration.value) * 1000)
  }
  rafId = requestAnimationFrame(tick)
}

onMounted(() => {
  attach(0)
  rafId = requestAnimationFrame(tick)
})

onBeforeUnmount(() => {
  cancelAnimationFrame(rafId)
  destroyHls()
})
</script>

<style scoped>
.live-player {
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: 100%;
}
.live-video {
  width: 100%;
  max-height: 60vh;
  background: #000;
  border-radius: 8px;
}
.live-bar {
  display: flex;
  align-items: center;
  gap: 10px;
}
.live-time {
  font-variant-numeric: tabular-nums;
  color: var(--color-text-secondary, #666);
  font-size: 13px;
}
.live-seek {
  flex: 1;
}
.live-hint {
  margin: 0;
  font-size: 12px;
  color: var(--color-text-tertiary, #999);
}
.live-err {
  margin: 0;
  font-size: 13px;
  color: var(--color-danger, #c0392b);
}
</style>
