<template>
  <div class="viewer-mask" @click.self="close">
    <button class="viewer-close" type="button" aria-label="关闭" @click="close">
      <svg viewBox="0 0 24 24" width="22" height="22" fill="none">
        <path d="M6 6l12 12M18 6L6 18" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
      </svg>
    </button>
    <div class="pano-box">
      <Player360
        v-if="panoReady"
        :key="item.id"
        :media-id="String(item.id)"
        :mode="item.type === 'photo' ? 'photo' : 'video'"
        :src="panoSrc"
        :title="item.filename || ''"
        auth="none"
        :append-query="panoAppendQuery"
        :bandwidth-kbps="panoBandwidthKbps"
      />
      <div v-else class="pano-loading">
        <div class="spinner"></div>
      </div>
    </div>
  </div>
</template>

<script setup>
// SharePublicView 拆解（Job000084）：360 全景查看器（照片/视频统一球面渲染）。
// 照片取 lg 缩略图贴球面立即就绪；视频先做带宽自测（有上界），据测速值让 360Player
// 选初始档位。测速在途时关闭：作废 seq 防回填重新挂载。
import { onBeforeUnmount, ref } from 'vue'
import Player360 from '../player/360Player.vue'
import { measureShareBandwidth, publicHlsUrl, publicStreamUrl, publicThumbUrl } from './publicApi'

const props = defineProps({
  item: { type: Object, required: true },
  token: { type: String, required: true },
  password: { type: String, default: '' }
})
const emit = defineEmits(['close'])

/* Phase 4 P1：带宽自测结束前先不挂载 360 播放器（避免用错档位起播） */
const panoReady = ref(false)
/* 实测下行带宽（kbps）；0 = 未知/测速失败 → 360Player 走 hls.js 默认 ABR */
const panoBandwidthKbps = ref(0)
/* 带宽自测等待上限：超时先播放，拿不到提示就交给 hls.js 默认 ABR（绝不无限阻塞） */
const BW_HINT_TIMEOUT_MS = 1500
/* 关闭时作废在途的带宽测量，避免其回填后重新挂载播放器 */
let panoSeq = 0
let alive = true

// Job000139：视频源探测——未转码的视频没有 HLS（404），回退原片在线播放（与登录端 PlayerView 的
// hlsUrl||panoOriginalUrl 同一策略；此前分享端只连 HLS，未转码视频必黑屏失败）。
const panoSrc = ref('')
const panoFallback = ref(false)
async function resolvePanoSrc() {
  if (props.item.type === 'photo') {
    panoSrc.value = publicThumbUrl(props.token, props.item.id, 'lg', props.password)
    return
  }
  const hls = publicHlsUrl(props.token, props.item.id, props.password)
  try {
    const r = await fetch(hls, { method: 'HEAD' })
    if (r.ok) { panoSrc.value = hls; return }
  } catch { /* 网络异常按无 HLS 处理 */ }
  panoFallback.value = true
  panoSrc.value = publicStreamUrl(props.token, props.item.id, props.password)
}
// 密码分享的 HLS ts 切片请求不继承 master URL 查询串，需逐请求补挂
const panoAppendQuery = computed(() => (props.password ? `password=${encodeURIComponent(props.password)}` : ''))

async function prepare() {
  await resolvePanoSrc()
  panoBandwidthKbps.value = 0
  if (props.item.type === 'photo') {
    panoReady.value = true
    return
  }
  panoReady.value = false
  const seq = ++panoSeq
  const measured = measureShareBandwidth(props.token, props.password)
    .then((r) => (r && Number(r.down_kbps) > 0 ? Number(r.down_kbps) : 0))
    .catch(() => 0) // 测速失败 → 0 → 360Player 走 hls.js 默认 ABR
  const hint = await Promise.race([
    measured,
    new Promise((resolve) => setTimeout(() => resolve(0), BW_HINT_TIMEOUT_MS))
  ])
  if (seq !== panoSeq || !alive) return // 已关闭/已卸载：丢弃
  panoBandwidthKbps.value = hint
  panoReady.value = true
}

function close() {
  panoSeq++ // 作废在途的带宽测量
  panoReady.value = false
  panoBandwidthKbps.value = 0
  emit('close') // v-if 卸载即触发 Player360 自身资源销毁
}

prepare()

onBeforeUnmount(() => {
  alive = false
  panoSeq++
})
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

/* 360 球面渲染容器：占满可视区（Player360 内部自带控制栏） */
.pano-box {
  position: relative;
  width: 94vw;
  height: 84vh;
  max-width: 1200px;
  background-color: var(--player-bg);
  border-radius: var(--radius-md);
  overflow: hidden;
}

/* 带宽自测期间的占位（有上限，不会长期停留） */
.pano-loading {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
}

.spinner {
  width: 28px;
  height: 28px;
  border: 3px solid var(--color-border);
  border-top-color: var(--color-primary);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
