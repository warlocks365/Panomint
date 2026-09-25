<template>
  <section class="card">
    <div class="card-head">
      <h2 class="card-title">转码设置</h2>
      <button class="btn btn--ghost" type="button" :disabled="loading" data-testid="tc-reload" @click="load">
        {{ loading ? '加载中…' : '刷新' }}
      </button>
    </div>

    <p class="card-desc">
      「自动 HLS 转码」是<strong>系统级</strong>开关（Job000120-r2，原用户设置页开关已按裁决上收至此）：
      关闭后全站用户的播放器都不再发起 HLS 切片转码，直接使用原始文件播放——
      可避免大视频转码长时间占满服务器 CPU 与内存；缩略图与语义索引不受影响照常生成。
      360° 视频是例外：全景播放依赖 HLS 切片，关闭后新上传的 360° 视频将无法播放。
      命令行批量补排（transcodectl）是管理员工具，不受此开关约束。
    </p>

    <p v-if="forbidden" class="msg msg--error">当前账号缺少 admin:system 权限，无法管理转码设置。</p>
    <p v-if="err" class="msg msg--error" data-testid="tc-err">{{ err }}</p>
    <p v-if="msg" class="msg" :class="msgKind === 'error' ? 'msg--error' : 'msg--ok'" data-testid="tc-msg">{{ msg }}</p>

    <label v-if="!forbidden" class="tc-row" data-testid="tc-toggle-row">
      <input v-model="enabled" type="checkbox" class="tc-switch" data-testid="tc-toggle" :disabled="busy" @change="onToggle" />
      <span class="tc-label">
        <span class="tc-name">自动 HLS 转码</span>
        <span class="tc-hint">{{ enabled ? '已开启：视频可转码为多档 HLS 流（弱网自动降清晰度）' : '已关闭：全站播放直接使用原始文件，不占用转码算力' }}</span>
        <span v-if="updatedAt" class="tc-updated">最近变更：{{ updatedAt }}</span>
      </span>
    </label>
  </section>
</template>

<script setup>
// Job000120-r2（用户裁决 2026-09-25）：「自动 HLS 转码」从用户设置页上收为
// 管理后台系统级开关（后端 system_transcode_config 单行表，迁移 00038）。
// 读写 = GET/PUT /admin/transcode-config（admin:system，路由层校验）；
// 播放器侧经只读视图 GET /transcode/config 预判姿态（不发起注定 409 的请求）。
// 失败回滚开关姿态，与服务端真值保持一致。
import { onMounted, ref } from 'vue'
import { getTranscodeConfig, putTranscodeConfig } from '../../api/admin'

const enabled = ref(true)
const updatedAt = ref('')
const loading = ref(false)
const busy = ref(false)
const forbidden = ref(false)
const err = ref('')
const msg = ref('')
const msgKind = ref('ok')

async function load() {
  loading.value = true
  err.value = ''
  try {
    const d = (await getTranscodeConfig()) || {}
    enabled.value = d.auto_transcode !== false // 缺省=开启（无配置行的存量部署）
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
    const d = (await putTranscodeConfig(enabled.value)) || {}
    enabled.value = d.auto_transcode !== false
    updatedAt.value = d.updated_at ? new Date(d.updated_at).toLocaleString() : ''
    msgKind.value = 'ok'
    msg.value = enabled.value ? '已开启自动 HLS 转码（全站生效）' : '已关闭自动 HLS 转码：全站播放将直接使用原始文件'
  } catch (e) {
    enabled.value = !enabled.value // 失败回滚开关姿态，与服务端真值保持一致
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

.tc-updated {
  font-size: var(--font-size-sm);
  color: var(--color-text-disabled);
}
</style>
