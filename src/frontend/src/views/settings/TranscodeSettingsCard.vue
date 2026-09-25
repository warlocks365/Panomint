<template>
  <section class="card" data-testid="tc-card">
    <h2 class="card-title">播放与转码</h2>
    <p class="card-desc">
      关闭「自动 HLS 转码」后：上传的视频不再发起切片转码，播放时直接使用原始文件，
      缩略图与语义索引不受影响照常生成——可避免大视频转码长时间占满服务器 CPU 与内存。
      360° 视频是例外：全景播放依赖 HLS 切片，关闭后新上传的 360° 视频将无法播放。
    </p>

    <p v-if="msg" class="msg" :class="msgKind === 'error' ? 'msg--error' : 'msg--ok'" data-testid="tc-msg">{{ msg }}</p>

    <label class="tc-row" data-testid="tc-toggle-row">
      <input v-model="enabled" type="checkbox" class="tc-switch" data-testid="tc-toggle" :disabled="busy" @change="onToggle" />
      <span class="tc-label">
        <span class="tc-name">自动 HLS 转码</span>
        <span class="tc-hint">{{ enabled ? '已开启：视频可转码为多档 HLS 流（弱网自动降清晰度）' : '已关闭：播放直接使用原始文件，不占用转码算力' }}</span>
      </span>
    </label>
  </section>
</template>

<script setup>
// Job000120 用户级「自动 HLS 转码」开关（后端 user_ui_prefs.auto_transcode，默认开启）。
// PUT 是整行替换语义，但后端对 auto_transcode 采用 keep-on-absent：
// 这里永远显式带上当前值，行为不受该例外影响。
import { onMounted, ref } from 'vue'
import { getUiPrefs, putUiPrefs } from '../../api/map'

const enabled = ref(true)
const busy = ref(false)
const msg = ref('')
const msgKind = ref('ok')

function fullPrefs(d) {
  return {
    map_slider_pos: d.map_slider_pos || 'bottom',
    map_filter_side: d.map_filter_side || 'left',
    map_filter_collapsed: d.map_filter_collapsed === true,
    map_marker_mode: d.map_marker_mode === 'thumb' ? 'thumb' : 'icon',
    map_default_provider: d.map_default_provider || 'auto',
    map_default_zoom: typeof d.map_default_zoom === 'number' ? d.map_default_zoom : null,
    spaces_group_by_album: d.spaces_group_by_album === true,
    auto_transcode: enabled.value
  }
}

async function load() {
  try {
    const d = (await getUiPrefs()).data || {}
    enabled.value = d.auto_transcode !== false // 缺省=开启（存量行为不变）
  } catch {
    enabled.value = true
  }
}

async function onToggle() {
  msg.value = ''
  busy.value = true
  try {
    const d = (await getUiPrefs()).data || {}
    await putUiPrefs(fullPrefs(d))
    msgKind.value = 'ok'
    msg.value = enabled.value ? '已开启自动 HLS 转码' : '已关闭自动 HLS 转码：播放将直接使用原始文件'
  } catch (e) {
    enabled.value = !enabled.value // 失败回滚开关姿态，与服务端真值保持一致
    msgKind.value = 'error'
    msg.value = (e.response?.data?.error?.message) || '保存失败，请重试'
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.card-desc {
  margin: 0 0 12px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  line-height: 1.7;
}

.msg {
  margin: 8px 0;
  font-size: var(--font-size-sm);
}

.msg--ok {
  color: var(--color-success);
}

.msg--error {
  color: var(--color-danger);
}

.tc-row {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  cursor: pointer;
}

.tc-switch {
  margin-top: 3px;
  width: 16px;
  height: 16px;
  accent-color: var(--color-primary);
  cursor: pointer;
}

.tc-label {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.tc-name {
  font-size: var(--font-size-md);
  font-weight: 600;
  color: var(--color-text-primary);
}

.tc-hint {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}
</style>
