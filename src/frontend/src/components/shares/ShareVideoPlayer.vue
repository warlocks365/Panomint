<template>
  <div class="viewer-mask" @click.self="close">
    <button class="viewer-close" type="button" aria-label="关闭" @click="close">
      <svg viewBox="0 0 24 24" width="22" height="22" fill="none">
        <path d="M6 6l12 12M18 6L6 18" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
      </svg>
    </button>
    <div class="player-box">
      <!-- Job000138：加 muted——移动端浏览器（Chrome/Safari/微信）静默阻止「带声 autoplay」，分享页视频因此黑屏不播；
     muted 后自动起播成立，声音由用户经原生控制条开启 -->
    <video ref="videoEl" class="player-video" controls autoplay muted playsinline webkit-playsinline></video>
      <p v-if="playerError" class="player-error">{{ playerError }}</p>
    </div>
  </div>
</template>

<script setup>
// SharePublicView 拆解（Job000084）：普通视频 HLS 播放器。
// 挂载即起播（hls.js；Safari/微信原生 HLS 分支密码分享明确提示不支持），
// 关闭/卸载即销毁 hls 实例并复位 video 元素。
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import Hls from 'hls.js'
import { publicHlsUrl, publicStreamUrl } from './publicApi'

const props = defineProps({
  item: { type: Object, required: true },
  token: { type: String, required: true },
  password: { type: String, default: '' }
})
const emit = defineEmits(['close'])

const playerError = ref('')
const videoEl = ref(null)
let hls = null
let triedFallback = false

function destroyPlayer() {
  if (hls) {
    hls.destroy()
    hls = null
  }
  const v = videoEl.value
  if (v) {
    v.pause()
    v.removeAttribute('src')
    v.load()
  }
}

function close() {
  destroyPlayer()
  emit('close')
}

onMounted(async () => {
  await nextTick()
  const v = videoEl.value
  if (!v) return
  const url = publicHlsUrl(props.token, props.item.id, props.password)
  if (Hls.isSupported()) {
    hls = new Hls({
      enableWorker: true,
      // 密码分享：m3u8 相对路径的 ts 切片请求不继承 master URL 查询串，逐请求补挂 password
      xhrSetup: (xhr, url) => {
        if (props.password) {
          const sep = url.includes('?') ? '&' : '?'
          xhr.open('GET', url + sep + 'password=' + encodeURIComponent(props.password), true)
        }
      }
    })
    hls.on(Hls.Events.ERROR, (_evt, data) => {
      if (!data?.fatal) return
      // Job000139：HLS 不存在（未转码）→ 回退原片在线播放（inline 流，Range 拖动可用）
      if (data.type === Hls.ErrorTypes.NETWORK_ERROR && !triedFallback) {
        triedFallback = true
        hls.destroy()
        hls = null
        v.src = publicStreamUrl(props.token, props.item.id, props.password)
        v.play().catch(() => {})
        return
      }
      playerError.value = '视频加载失败，请稍后重试'
    })
    hls.loadSource(url)
    hls.attachMedia(v)
  } else if (v.canPlayType('application/vnd.apple.mpegurl')) {
    // iOS Safari / 微信内置浏览器原生 HLS：无法给 m3u8 相对路径的 ts 切片逐个补挂密码
    // （hls.js 的 xhrSetup 在原生分支不存在），密码分享必 403 —— 明确提示，不静默失败。
    // 长期方案：后端为切片签发一次性查询令牌（签名 URL）。
    if (props.password) {
      playerError.value = '当前浏览器不支持受保护视频播放，请用桌面或 Android 设备观看'
      return
    }
    v.src = url
  } else {
    playerError.value = '当前浏览器不支持视频播放'
  }
})

onBeforeUnmount(destroyPlayer)
</script>

<style scoped>
.viewer-mask {
  position: fixed;
  inset: 0;
  z-index: 200;
  display: flex;
  align-items: center;
  justify-content: center;
  background-color: rgba(0, 0, 0, 0.9);
}

.viewer-close {
  position: absolute;
  top: 12px;
  right: 12px;
  z-index: 210;
  width: 40px;
  height: 40px;
  border: none;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  background-color: rgba(255, 255, 255, 0.15);
  cursor: pointer;
}

.player-box {
  width: 100%;
  max-width: 720px;
  display: flex;
  flex-direction: column;
  align-items: center;
}

.player-video {
  width: 100%;
  max-height: 80vh;
  background-color: #000;
}

.player-error {
  margin-top: 10px;
  font-size: var(--font-size-sm);
  color: var(--player-error-text);
}
</style>
