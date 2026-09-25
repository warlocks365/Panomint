<template>
  <section class="card">
    <div class="card-head">
      <h2 class="card-title">转码设置</h2>
      <button class="btn btn--ghost" type="button" :disabled="loading" data-testid="tc-reload" @click="load">
        {{ loading ? '加载中…' : '刷新' }}
      </button>
    </div>

    <p class="card-desc">
      两个开关均为<strong>系统级</strong>（全站生效，管理后台维护）：
      「自动 HLS 转码」是<strong>总闸门</strong>，关闭后任何来源（播放器/API）都不能发起转码，
      直接使用原始文件播放——可避免大视频转码长时间占满服务器 CPU 与内存；缩略图与语义索引不受影响照常生成。
      360° 视频是例外：全景播放依赖 HLS 切片，关闭后新上传的 360° 视频将无法播放。
      「播放时自动转码」控制<strong>触发方式</strong>：开启后播放无 HLS 的视频自动发起转码，
      产物为多档码流，按带宽自适应切换清晰度。命令行批量补排（transcodectl）是管理员工具，不受开关约束。
    </p>

    <p v-if="forbidden" class="msg msg--error">当前账号缺少 admin:system 权限，无法管理转码设置。</p>
    <p v-if="err" class="msg msg--error" data-testid="tc-err">{{ err }}</p>
    <p v-if="msg" class="msg" :class="msgKind === 'error' ? 'msg--error' : 'msg--ok'" data-testid="tc-msg">{{ msg }}</p>

    <template v-if="!forbidden">
      <label class="tc-row" data-testid="tc-toggle-row">
        <input v-model="enabled" type="checkbox" class="tc-switch" data-testid="tc-toggle" :disabled="busy" @change="onToggle" />
        <span class="tc-label">
          <span class="tc-name">自动 HLS 转码</span>
          <span class="tc-hint">{{ enabled ? '已开启：允许发起转码（手动或自动），产出多档 HLS 流（弱网自动降清晰度）' : '已关闭（总闸门）：全站播放直接使用原始文件，不占用转码算力' }}</span>
          <span v-if="updatedAt" class="tc-updated">最近变更：{{ updatedAt }}</span>
        </span>
      </label>

      <label class="tc-row" data-testid="tc-rt-toggle-row">
        <input
          v-model="realtime"
          type="checkbox"
          class="tc-switch"
          data-testid="tc-rt-toggle"
          :disabled="busy || !enabled"
          @change="onRealtimeToggle"
        />
        <span class="tc-label">
          <span class="tc-name">播放时自动转码（实时转码）</span>
          <span class="tc-hint">{{ realtimeHint }}</span>
        </span>
      </label>
    </template>
  </section>
</template>

<script setup>
// Job000120-r2（用户裁决 2026-09-25）：「自动 HLS 转码」上收为管理后台系统级总闸门
//（后端 system_transcode_config 单行表，迁移 00038）。
// Job000124（2026-09-25）：新增「播放时自动转码」开关（迁移 00040 增列 realtime_transcode）。
// 两开关 AND 关系：有效自动触发 = 总闸门 && 实时开关；总闸门关时实时开关在 UI 禁用（AND 语义的界面表达，
// 服务端兜底库中可能出现的 (false,true) 姿态）。读写 = GET/PUT /admin/transcode-config（admin:system），
// PUT 为部分更新（undefined = 不改），失败回滚开关姿态，与服务端真值保持一致。
import { computed, onMounted, ref } from 'vue'
import { getTranscodeConfig, putTranscodeConfig } from '../../api/admin'

const enabled = ref(true)
const realtime = ref(false)
const updatedAt = ref('')
const loading = ref(false)
const busy = ref(false)
const forbidden = ref(false)
const err = ref('')
const msg = ref('')
const msgKind = ref('ok')

const realtimeHint = computed(() => {
  if (!enabled.value) return '需先开启上方「自动 HLS 转码」总开关'
  return realtime.value
    ? '已开启：播放无 HLS 的视频时自动转码，完成后自动切换多档自适应流（转码期间普通视频先用原始文件播放）'
    : '已关闭：保持手动发起转码（现状，播放页显示转码按钮）'
})

async function load() {
  loading.value = true
  err.value = ''
  try {
    const d = (await getTranscodeConfig()) || {}
    enabled.value = d.auto_transcode !== false // 缺省=开启（无配置行的存量部署）
    realtime.value = d.realtime_transcode === true // 缺省=关闭（Job000124 之前无此能力）
    updatedAt.value = d.updated_at ? new Date(d.updated_at).toLocaleString() : ''
  } catch (e) {
    if (e?.response?.status === 403) {
      forbidden.value = true
    } else {
      err.value = (e?.response?.data?.error?.message) || '加载失败，请重试'
    }
  } finally {
    loading.value = false
  }
}

async function onToggle() {
  msg.value = ''
  busy.value = true
  try {
    const d = (await putTranscodeConfig(enabled.value, undefined)) || {}
    enabled.value = d.auto_transcode !== false
    realtime.value = d.realtime_transcode === true
    updatedAt.value = d.updated_at ? new Date(d.updated_at).toLocaleString() : ''
    msgKind.value = 'ok'
    msg.value = enabled.value ? '已开启自动 HLS 转码（全站生效）' : '已关闭自动 HLS 转码总闸门：全站播放将直接使用原始文件，实时转码同时停用'
  } catch (e) {
    enabled.value = !enabled.value // 失败回滚开关姿态，与服务端真值保持一致
    msgKind.value = 'error'
    msg.value = (e?.response?.data?.error?.message) || '保存失败，请重试'
  } finally {
    busy.value = false
  }
}

async function onRealtimeToggle() {
  msg.value = ''
  busy.value = true
  try {
    const d = (await putTranscodeConfig(undefined, realtime.value)) || {}
    enabled.value = d.auto_transcode !== false
    realtime.value = d.realtime_transcode === true
    updatedAt.value = d.updated_at ? new Date(d.updated_at).toLocaleString() : ''
    msgKind.value = 'ok'
    msg.value = realtime.value ? '已开启播放时自动转码（全站生效，转码期间普通视频先播原始文件）' : '已关闭播放时自动转码：恢复手动发起转码'
  } catch (e) {
    realtime.value = !realtime.value // 失败回滚开关姿态，与服务端真值保持一致
    msgKind.value = 'error'
    msg.value = (e?.response?.data?.error?.message) || '保存失败，请重试'
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
  padding: 8px 0;
}

.tc-row + .tc-row {
  border-top: 1px solid var(--color-border);
}

.tc-switch {
  margin-top: 3px;
  width: 16px;
  height: 16px;
  accent-color: var(--color-primary);
  cursor: pointer;
}

.tc-switch:disabled {
  cursor: default;
  opacity: 0.5;
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

.tc-updated {
  font-size: var(--font-size-sm);
  color: var(--color-text-disabled);
}
</style>
