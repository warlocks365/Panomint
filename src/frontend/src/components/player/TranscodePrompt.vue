<template>
  <div class="state transcode">
    <!-- Job000120：用户关闭了自动 HLS 转码。360° 视频必须走 HLS 切片才能全景播放，
         原始文件无法直接贴球面——如实告知，不假装能播。 -->
    <template v-if="disabled">
      <div class="msg" data-testid="tc-prompt-disabled">已关闭自动 HLS 转码</div>
      <div class="sub">设置页「播放与转码」开启后，可发起转码再全景播放</div>
    </template>
    <template v-else-if="!transcode.jobId && !transcode.failed">
      <div class="msg">该 360 视频尚未转码</div>
      <div class="sub">转码为 HLS 多码率流后才能全景播放</div>
      <button class="primary" :disabled="transcode.starting" @click="$emit('start')">
        {{ transcode.starting ? '发起中…' : '发起转码（1080p）' }}
      </button>
    </template>
    <template v-else-if="transcode.failed">
      <div class="msg">转码失败</div>
      <button class="primary" @click="$emit('start')">重新发起转码</button>
    </template>
    <template v-else>
      <div class="msg">转码中…（{{ transcode.status }}）</div>
      <div class="sub">完成后将自动加载播放</div>
    </template>
  </div>
</template>

<script setup>
// PlayerView 拆解（Job000091）：360 视频转码提示块——
// 三态展示（未转码可发起/转码中轮询状态/失败重试），点击原样上抛 start 由宿主编排（真实 POST 属写操作）。
defineProps({
  transcode: { type: Object, required: true }, // { jobId, status, starting, failed }
  disabled: { type: Boolean, default: false } // Job000120：自动转码已关闭（用户级开关）
})
defineEmits(['start'])
</script>

<style scoped>
.state {
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  color: var(--player-text-dim);
  font-size: var(--font-size-md);
}

.state .msg {
  font-size: var(--font-size-lg);
  color: var(--player-text);
}

.state .sub {
  font-size: var(--font-size-sm);
}

.primary {
  background: var(--color-primary);
  color: #fff;
  border: none;
  border-radius: var(--radius-md);
  padding: 10px 24px;
  font-size: var(--font-size-lg);
  cursor: pointer;
}

.primary:hover {
  background: var(--color-primary-hover);
}

.primary:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
