<template>
  <form class="create-form" data-testid="user-create-form" @submit.prevent="onSubmit">
    <h3 class="sub-title">新建用户</h3>
    <div class="form-row">
      <label class="field">
        <span class="field-label">邮箱</span>
        <input v-model.trim="email" data-testid="uc-email" type="email" required />
      </label>
      <label class="field">
        <span class="field-label">昵称</span>
        <input v-model.trim="displayName" data-testid="uc-name" type="text" />
      </label>
      <label class="field">
        <span class="field-label">密码（至少 8 位）</span>
        <input v-model="password" data-testid="uc-password" type="password" minlength="8" required />
      </label>
      <label class="field">
        <span class="field-label">角色</span>
        <select v-model="role" data-testid="uc-role">
          <option v-for="r in roleOptions" :key="r" :value="r">{{ r }}</option>
        </select>
      </label>
      <button class="btn btn--primary" type="submit" :disabled="busy" data-testid="uc-submit">
        {{ busy ? '创建中…' : '创建' }}
      </button>
    </div>
    <p v-if="error" class="msg msg--error" data-testid="uc-error">{{ error }}</p>
  </form>
</template>

<script setup>
import { ref, watch } from 'vue'

const props = defineProps({
  roleOptions: { type: Array, default: () => [] },
  busy: { type: Boolean, default: false },
  error: { type: String, default: '' }
})
const emit = defineEmits(['submit'])

const email = ref('')
const displayName = ref('')
const password = ref('')
const role = ref('member')

// 角色选项到达后校正默认值（member 可能不存在，取第一个可用角色）
watch(
  () => props.roleOptions,
  (opts) => {
    if (opts.length && !opts.includes(role.value)) role.value = opts[0]
  },
  { immediate: true }
)

function onSubmit() {
  emit(
    'submit',
    { email: email.value, display_name: displayName.value, password: password.value, role: role.value },
    () => {
      // 成功后清空（由父级在创建成功时调用）
      email.value = ''
      displayName.value = ''
      password.value = ''
    }
  )
}
</script>

<style scoped>
.create-form {
  border-bottom: 1px solid var(--color-border);
  padding-bottom: 16px;
  margin-bottom: 8px;
}
.sub-title {
  margin: 16px 0 10px;
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
}
.form-row {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: flex-end;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 160px;
}
.field-label {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}
input,
select {
  padding: 7px 10px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  background: var(--color-surface);
}
input:focus,
select:focus {
  outline: 2px solid var(--color-primary-active-bg);
  border-color: var(--color-primary);
}
.msg {
  margin: 8px 0 0;
  font-size: var(--font-size-md);
}
.msg--error {
  color: var(--color-danger);
}
.btn {
  padding: 6px 14px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background: var(--color-surface);
  color: var(--color-text-primary);
  font-size: var(--font-size-md);
  cursor: pointer;
}
.btn:hover {
  background: var(--color-surface-hover);
}
.btn:disabled {
  opacity: 0.6;
  cursor: default;
}
.btn--primary {
  background: var(--color-primary);
  border-color: var(--color-primary);
  color: var(--color-surface);
}
.btn--primary:hover {
  background: var(--color-primary-hover);
}
</style>
