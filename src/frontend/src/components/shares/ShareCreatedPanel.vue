<template>
  <h3 class="dlg-title">分享创建成功</h3>
  <p class="success-desc">复制以下链接发送给好友，对方无需登录即可访问：</p>
  <div class="link-row">
    <input class="input link-input" type="text" readonly :value="link" data-testid="share-link" @focus="$event.target.select()" />
    <button class="btn primary" data-testid="share-copy" @click="copyLink">{{ copied ? '已复制' : '复制' }}</button>
  </div>
  <p v-if="password" class="pwd-hint">访问密码：{{ password }}</p>
  <div class="dlg-actions">
    <button class="btn primary" @click="$emit('cancel')">完成</button>
  </div>
</template>

<script setup>
// 分享创建成功面板（Job000094 从 ShareCreateDialog 抽出）：链接展示+复制+密码提示。
// 剪贴板交互全自持（生命周期内聚——084 范式）：clipboard API 优先，
// 非安全上下文回退 execCommand；copied 两秒自动复位。
import { computed, ref } from 'vue'
import { shareLink } from './shareApi'

const props = defineProps({
  created: { type: Object, required: true },
  password: { type: String, default: '' }
})
defineEmits(['cancel'])

const copied = ref(false)

const link = computed(() => shareLink(props.created.token))

async function copyLink() {
  try {
    await navigator.clipboard.writeText(link.value)
  } catch (e) {
    // 剪贴板 API 不可用（非安全上下文等）时回退
    const ta = document.createElement('textarea')
    ta.value = link.value
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    document.body.removeChild(ta)
  }
  copied.value = true
  setTimeout(() => (copied.value = false), 2000)
}
</script>

<style scoped>
.dlg-title {
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
  margin-bottom: 16px;
}

.success-desc {
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
  margin-bottom: 12px;
}

.input {
  padding: 8px 10px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  font-family: var(--font-family);
  color: var(--color-text-primary);
  background-color: var(--color-surface);
}

.link-row {
  display: flex;
  gap: 8px;
}

.link-input {
  flex: 1;
  min-width: 0;
  color: var(--color-text-secondary);
}

.pwd-hint {
  margin-top: 10px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.dlg-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 18px;
}

.btn {
  padding: 8px 16px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  background-color: var(--color-surface);
  cursor: pointer;
}

.btn:hover {
  background-color: var(--color-surface-hover);
}

.btn.primary {
  border-color: var(--color-primary);
  color: #fff;
  background-color: var(--color-primary);
}

.btn.primary:hover {
  background-color: var(--color-primary-hover);
}
</style>
