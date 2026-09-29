<template>
  <div class="pano-player">
    <div ref="containerRef" class="pano-container"></div>
    <video ref="videoRef" crossorigin="anonymous" playsinline webkit-playsinline loop muted class="pano-video"></video>

    <div class="bar topbar" :class="{ hidden: barsHidden }">
      <span class="title">{{ title || (isPhoto ? '360 全景照片' : '360 全景播放') }}</span>
      <span class="stats">{{ stream.statsText.value }}</span>
      <select v-if="!isPhoto && stream.qualityOptions.value.length > 0" v-model="qualityValue" @change="stream.onQualityChange">
        <option value="-1">自动</option>
        <option v-for="q in stream.qualityOptions.value" :key="q.value" :value="q.value" :disabled="q.disabled">
          {{ q.label }}
        </option>
      </select>
    </div>

    <div class="bar controls" :class="{ hidden: barsHidden }">
      <div v-if="!isPhoto" class="progress-wrap">
        <span class="time">{{ stream.timeText.value }}</span>
        <input type="range" class="progress" min="0" max="1000" v-model="progressValue" @input="stream.onSeek">
      </div>
      <button v-if="!isPhoto" @click="stream.togglePlay">{{ stream.playing.value ? '暂停' : '播放' }}</button>
      <button v-if="!isPhoto" :class="{ active: !stream.muted.value }" @click="stream.toggleMute">音量</button>
      <select v-if="!isPhoto" v-model="rate" @change="stream.onRateChange">
        <option value="0.5">0.5x</option>
        <option value="1">1.0x</option>
        <option value="1.5">1.5x</option>
        <option value="2">2.0x</option>
      </select>
      <button :class="{ active: gyro.gyroOn.value }" @click="gyro.toggleGyro">陀螺仪 {{ gyro.gyroOn.value ? '开' : '关' }}</button>
      <button @click="gyro.calibrate">校准</button>
      <button :disabled="!stream.vrSupported.value" @click="stream.enterVr">VR 模式</button>
      <span class="fov-label">FOV</span>
      <input type="range" class="fov" min="45" max="100" v-model="fovValue" @input="onFovInput">
    </div>

    <div class="overlay" :class="{ show: stream.overlay.show }">
      <div class="msg">{{ stream.overlay.msg }}</div>
      <div class="sub">{{ stream.overlay.sub }}</div>
      <button class="retry" @click="stream.onRetry">重试</button>
    </div>
  </div>
</template>

<script setup>
/* 360Player 宿主：props/emits 接口零改动（PlayerView / SharePanoViewer 两调用方不变）。
 * 引擎核心（usePanoEngine）/陀螺仪（usePanoGyro）/流媒体播放（usePanoStream）三域分离，
 * 宿主自持 DOM ref + 控制栏 UI 状态（barsHidden/fovValue/toast/pokeBars）并装配编排。 */
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { usePanoEngine } from './usePanoEngine'
import { usePanoGyro } from './usePanoGyro'
import { usePanoStream } from './usePanoStream'

const props = defineProps({
  mediaId: { type: String, required: true },
  src: { type: String, required: true }, // video: hls master 完整 URL；photo: 图片 URL
  title: { type: String, default: '' },
  mode: { type: String, default: 'video' }, // video | photo（360 照片球面渲染）
  auth: { type: String, default: 'bearer' }, // bearer（主站 HLS 带 token）| none（分享公开端点免鉴权）
  appendQuery: { type: String, default: '' }, // 追加到每个 HLS 请求 URL 的查询串（如分享密码 password=xxx）
  // Phase 4 P1：实测下行带宽（kbps）。0 = 未知/测速失败 → 完全不干预，用 hls.js 默认 ABR。
  bandwidthKbps: { type: Number, default: 0 }
})
const isPhoto = computed(() => props.mode === 'photo')

const containerRef = ref(null)
const videoRef = ref(null)

/* 控制栏 UI 状态 */
const barsHidden = ref(false)
const fovValue = ref(75)
let hideTimer = 0
let toastTimer = 0

function showToast(text) {
  stream.statsText.value = text
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => { stream.statsText.value = ''; toastTimer = 0 }, 2500)
  pokeBars()
}
function pokeBars() {
  barsHidden.value = false
  clearTimeout(hideTimer)
  hideTimer = setTimeout(() => { barsHidden.value = true }, 3000)
}

const engine = usePanoEngine({
  containerRef,
  videoRef,
  isPhoto,
  isGyroActive: () => gyro.gyroOn.value,
  pokeBars,
  onFovApplied: (v) => { fovValue.value = v }
})
const gyro = usePanoGyro({ engine, showToast })
const stream = usePanoStream({ engine, props, showToast })

/* 模板 v-model 透传（stream 持 ref，v-model 需可变引用） */
const qualityValue = stream.qualityValue
const progressValue = stream.progressValue
const rate = stream.rate

function onFovInput() { engine.setFov(Number(fovValue.value)) }

onMounted(() => {
  engine.init()
  stream.init()
  pokeBars()
})

onBeforeUnmount(() => {
  stream.destroy()
  gyro.destroy()
  engine.destroy()
  clearTimeout(hideTimer)
  clearTimeout(toastTimer)
})
</script>

<style scoped>
.pano-player {
  position: relative;
  width: 100%;
  height: 100%;
  overflow: hidden;
  background: var(--player-bg);
  color: var(--player-text);
  font-family: var(--font-family);
  --p-text-dim: var(--player-text-dim);
  --p-panel: rgba(20, 24, 29, 0.72);
}
.pano-container { position: absolute; inset: 0; touch-action: none; }
.pano-video { display: none; }

.bar {
  position: absolute; left: 0; right: 0; z-index: 10;
  display: flex; align-items: center; gap: 8px;
  padding: 8px 12px;
  background: var(--p-panel);
  /* Job000137：移除 backdrop-filter（移动端不重绘缺陷，同 PlayerView） */
  transition: opacity 0.3s;
}
.bar.hidden { opacity: 0; pointer-events: none; }
.topbar { top: 0; padding-right: 56px; /* 为 PlayerView 右上退出按钮预留空间 */ }
.controls { bottom: 0; flex-wrap: wrap; }
.bar button, .bar select {
  background: transparent; color: var(--player-text);
  border: 1px solid var(--p-text-dim);
  border-radius: var(--radius-md);
  padding: 6px 12px; font-size: var(--font-size-md);
  cursor: pointer; min-height: 36px;
}
.bar button:hover, .bar select:hover { border-color: var(--color-primary); color: var(--color-primary); }
.bar button.active { background: var(--color-primary); border-color: var(--color-primary); color: #fff; }
.bar button:disabled { opacity: 0.4; cursor: not-allowed; }
.title { font-size: var(--font-size-md); color: var(--p-text-dim); margin-right: auto; }
.stats { font-size: var(--font-size-sm); color: var(--p-text-dim); font-variant-numeric: tabular-nums; }

.progress-wrap { flex: 1 1 100%; display: flex; align-items: center; gap: 8px; order: -1; }
.progress {
  flex: 1; height: 6px; appearance: none; -webkit-appearance: none;
  background: var(--p-text-dim); border-radius: 3px; outline: none;
}
.progress::-webkit-slider-thumb {
  -webkit-appearance: none; width: 14px; height: 14px;
  border-radius: 50%; background: var(--color-primary); cursor: pointer;
}
.time {
  font-size: var(--font-size-sm); color: var(--p-text-dim);
  min-width: 90px; text-align: center; font-variant-numeric: tabular-nums;
}
.fov-label { font-size: var(--font-size-sm); color: var(--p-text-dim); }
.fov { width: 90px; }

.overlay {
  position: absolute; inset: 0; z-index: 20;
  display: none; align-items: center; justify-content: center;
  flex-direction: column; gap: 12px;
  background: var(--p-panel);
}
.overlay.show { display: flex; }
.overlay .msg { font-size: var(--font-size-lg); }
.overlay .sub { font-size: var(--font-size-sm); color: var(--p-text-dim); }
.overlay .retry {
  background: var(--color-primary); color: #fff; border: none;
  border-radius: var(--radius-md); padding: 10px 24px;
  font-size: var(--font-size-lg); cursor: pointer;
}
</style>
